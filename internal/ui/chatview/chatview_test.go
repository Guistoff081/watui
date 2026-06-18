package chatview

import (
	"testing"
	"time"

	"github.com/watui/watui/internal/theme"
)

func msgIDs(msgs []theme.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestAppendMessageInsertsInTimestampOrder(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	jid := "x@s.whatsapp.net"
	m.SetChat(jid, false, []theme.Message{
		{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "c", ChatJID: jid, Timestamp: time.Unix(300, 0)},
	})

	// Newest (live) goes to the end.
	m.AppendMessage(theme.Message{ID: "d", ChatJID: jid, Timestamp: time.Unix(400, 0)})
	// Older (offline replay) is inserted in order, not at the bottom.
	m.AppendMessage(theme.Message{ID: "b", ChatJID: jid, Timestamp: time.Unix(200, 0)})

	want := []string{"a", "b", "c", "d"}
	if got := msgIDs(m.messages); len(got) != len(want) {
		t.Fatalf("len = %d (%v), want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if m.messages[i].ID != w {
			t.Errorf("messages[%d].ID = %s, want %s", i, m.messages[i].ID, w)
		}
	}
}

func TestAppendMessageIgnoresOtherChat(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m.SetChat("x", false, nil)
	m.AppendMessage(theme.Message{ID: "a", ChatJID: "y", Timestamp: time.Unix(1, 0)})
	if len(m.messages) != 0 {
		t.Errorf("messages = %v, want empty (other chat ignored)", msgIDs(m.messages))
	}
}

func TestSetChatSortsAndSetsOldest(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	jid := "x"
	m.SetChat(jid, false, []theme.Message{
		{ID: "c", ChatJID: jid, Timestamp: time.Unix(300, 0)},
		{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "b", ChatJID: jid, Timestamp: time.Unix(200, 0)},
	})

	want := []string{"a", "b", "c"}
	for i, w := range want {
		if m.messages[i].ID != w {
			t.Errorf("messages[%d].ID = %s, want %s", i, m.messages[i].ID, w)
		}
	}
	if !m.oldestTS.Equal(time.Unix(100, 0)) {
		t.Errorf("oldestTS = %v, want %v", m.oldestTS, time.Unix(100, 0))
	}
}

func TestUpdateMessageStatus(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	jid := "x"
	m.SetChat(jid, false, []theme.Message{
		{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0), Status: "sending"},
	})
	m.UpdateMessageStatus("a", "read")
	if m.messages[0].Status != "read" {
		t.Errorf("status = %q, want read", m.messages[0].Status)
	}
}

func TestRenderMessageTextOnlyUnchanged(t *testing.T) {
	cache := make(map[string]string)
	msg := theme.Message{
		ID:        "t1",
		Content:   "hello world",
		Timestamp: time.Unix(100, 0),
		Status:    "sent",
		IsFromMe:  true,
	}
	rendered := renderMessage(msg, 80, false, false, cache)
	if rendered == "" {
		t.Fatal("renderMessage returned empty string for text message")
	}
	if !containsString(rendered, "hello world") {
		t.Errorf("rendered text message does not contain content:\n%s", rendered)
	}
}

func TestRenderMessageMediaHasBody(t *testing.T) {
	cache := make(map[string]string)
	msg := theme.Message{
		ID:        "img1",
		MediaType: "image",
		MimeType:  "image/jpeg",
		Width:     800,
		Height:    600,
		Timestamp: time.Unix(100, 0),
	}
	rendered := renderMessage(msg, 80, false, false, cache)
	if rendered == "" {
		t.Fatal("renderMessage returned empty string for image message")
	}
	if !containsString(rendered, "[image]") {
		t.Errorf("rendered image message missing type tag:\n%s", rendered)
	}
	if !containsString(rendered, "↵ open") {
		t.Errorf("rendered image message missing action hint:\n%s", rendered)
	}
}

func TestRenderMessageAudioHasHint(t *testing.T) {
	cache := make(map[string]string)
	msg := theme.Message{
		ID:        "aud1",
		MediaType: "voice",
		Duration:  95,
		Timestamp: time.Unix(100, 0),
	}
	rendered := renderMessage(msg, 80, false, false, cache)
	if !containsString(rendered, "1:35") {
		t.Errorf("rendered voice message missing formatted duration:\n%s", rendered)
	}
	if !containsString(rendered, "↵ play") {
		t.Errorf("rendered voice message missing play hint:\n%s", rendered)
	}
}

func TestInvalidateThumbnailClearsEntry(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	// Seed with the key format that InvalidateThumbnail generates (msgID + ":" + width).
	key := "msg1:80"
	m.thumbCache[key] = "CACHED"
	m.InvalidateThumbnail("msg1")
	if _, ok := m.thumbCache[key]; ok {
		t.Error("InvalidateThumbnail did not remove cached entry")
	}
}

// containsString reports whether s contains substr after stripping ANSI codes.
func containsString(s, substr string) bool {
	// Strip ANSI escape sequences for comparison.
	plain := stripANSI(s)
	return len(plain) > 0 && len(substr) > 0 && (plain == substr ||
		len(plain) >= len(substr) && containsRaw(plain, substr))
}

func containsRaw(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func stripANSI(s string) string {
	var out []byte
	inEsc := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= 'a' && s[i] <= 'z') {
				inEsc = false
			}
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
