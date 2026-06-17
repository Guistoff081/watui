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
