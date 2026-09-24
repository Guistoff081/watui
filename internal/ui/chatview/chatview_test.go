package chatview

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/core"
)

func msgIDs(msgs []core.Message) []string {
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
	m.SetChat(jid, false, []core.Message{
		{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "c", ChatJID: jid, Timestamp: time.Unix(300, 0)},
	})

	// Newest (live) goes to the end.
	m.AppendMessage(core.Message{ID: "d", ChatJID: jid, Timestamp: time.Unix(400, 0)})
	// Older (offline replay) is inserted in order, not at the bottom.
	m.AppendMessage(core.Message{ID: "b", ChatJID: jid, Timestamp: time.Unix(200, 0)})

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
	m.AppendMessage(core.Message{ID: "a", ChatJID: "y", Timestamp: time.Unix(1, 0)})
	if len(m.messages) != 0 {
		t.Errorf("messages = %v, want empty (other chat ignored)", msgIDs(m.messages))
	}
}

func TestSetChatSortsAndSetsOldest(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	jid := "x"
	m.SetChat(jid, false, []core.Message{
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
	m.SetChat(jid, false, []core.Message{
		{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0), Status: "sending"},
	})
	m.UpdateMessageStatus("a", "read")
	if m.messages[0].Status != "read" {
		t.Errorf("status = %q, want read", m.messages[0].Status)
	}
}

func TestRenderMessageTextOnlyUnchanged(t *testing.T) {
	cache := make(map[string]string)
	msg := core.Message{
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
	msg := core.Message{
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
	msg := core.Message{
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
	// Entries are keyed msgID:cols for any column count; all of them go, and
	// other messages' entries (even with a shared ID prefix) stay.
	m.thumbCache["msg1:40"] = "CACHED"
	m.thumbCache["msg1:12"] = "CACHED"
	m.thumbCache["msg10:40"] = "OTHER"
	m.InvalidateThumbnail("msg1")
	if _, ok := m.thumbCache["msg1:40"]; ok {
		t.Error("InvalidateThumbnail did not remove msg1:40")
	}
	if _, ok := m.thumbCache["msg1:12"]; ok {
		t.Error("InvalidateThumbnail did not remove msg1:12")
	}
	if _, ok := m.thumbCache["msg10:40"]; !ok {
		t.Error("InvalidateThumbnail removed another message's entry")
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

// SetChat must not alias the caller's slice: the app passes the core.Chats
// cache, and both sides later append/insert, which would otherwise corrupt
// each other through a shared backing array.
func TestSetChatDoesNotAliasCallerSlice(t *testing.T) {
	jid := "123@s.whatsapp.net"
	src := make([]core.Message, 2, 8) // spare capacity invites aliasing
	src[0] = core.Message{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0)}
	src[1] = core.Message{ID: "c", ChatJID: jid, Timestamp: time.Unix(300, 0)}

	m := New()
	m.SetSize(80, 24)
	m.SetChat(jid, false, src)

	m.AppendMessage(core.Message{ID: "b", ChatJID: jid, Timestamp: time.Unix(200, 0)})
	m.UpdateMessageStatus("a", "read")

	if got := msgIDs(src[:cap(src)][:3]); got[2] != "" {
		t.Errorf("caller backing array written by view: %v", got)
	}
	if src[0].Status == "read" {
		t.Errorf("caller message mutated by UpdateMessageStatus")
	}
	if got := msgIDs(m.messages); len(got) != 3 || got[1] != "b" {
		t.Errorf("view messages = %v, want [a b c]", got)
	}
}

// Long lines must wrap inside the bubble, not be cut at the bubble edge.
func TestRenderMessageWrapsLongLines(t *testing.T) {
	text := "Se você estava avaliando iniciar uma pós-graduação e dar o próximo passo na carreira, as inscrições seguem abertas até sexta"
	msg := core.Message{ID: "w", Content: text, Timestamp: time.Unix(0, 0)}
	out := stripANSI(renderMessage(msg, 60, false, false, map[string]string{}))
	for _, word := range []string{"avaliando", "carreira", "sexta"} {
		if !strings.Contains(out, word) {
			t.Errorf("rendered bubble lost %q (truncated instead of wrapped):\n%s", word, out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 60 {
			t.Errorf("line width %d exceeds view width 60: %q", w, l)
		}
	}
}

// Animated stickers and thumbnail-less GIFs render their extracted still
// frame (core.PosterPath) instead of an empty preview.
func TestRenderMessageUsesPosterForAnimatedMedia(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "a.webp")
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	f, err := os.Create(core.PosterPath(media))
	if err != nil {
		t.Fatal(err)
	}
	_ = png.Encode(f, img)
	f.Close()

	for _, msg := range []core.Message{
		{ID: "st", MediaType: "sticker", IsAnimated: true, MediaPath: media, Timestamp: time.Unix(0, 0)},
		{ID: "gf", MediaType: "gif", MediaPath: media, Timestamp: time.Unix(0, 0)},
	} {
		out := renderMessage(msg, 80, false, false, map[string]string{})
		if !strings.Contains(out, "▀") {
			t.Errorf("%s: no half-block preview rendered from the poster:\n%s", msg.MediaType, stripANSI(out))
		}
	}
}

// A preview rendered before the file/poster existed is cached as empty; the
// download handler's InvalidateThumbnail must clear it so the next render
// picks up the poster. The cache key uses the thumbnail column count, not
// the view width, so invalidation has to drop every entry for the message.
func TestInvalidateThumbnailPicksUpPosterCreatedLater(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "a.webp")
	msg := core.Message{ID: "late", ChatJID: "c@s.whatsapp.net", MediaType: "sticker", IsAnimated: true,
		MediaPath: media, Timestamp: time.Unix(0, 0)}

	m := New()
	m.SetSize(100, 30)
	m.SetChat("c@s.whatsapp.net", false, []core.Message{msg})
	if strings.Contains(m.View(), "▀") {
		t.Fatal("preview rendered before the poster exists")
	}

	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	f, _ := os.Create(core.PosterPath(media))
	_ = png.Encode(f, img)
	f.Close()

	m.InvalidateThumbnail("late")
	m.SetChat("c@s.whatsapp.net", false, []core.Message{msg})
	if !strings.Contains(m.View(), "▀") {
		t.Errorf("poster not rendered after InvalidateThumbnail:\n%s", stripANSI(m.View()))
	}
}

func tallMsg(id string, ts int64, lines int) core.Message {
	return core.Message{ID: id, ChatJID: "c@s.whatsapp.net", Content: strings.TrimSpace(strings.Repeat("linha\n", lines)),
		Timestamp: time.Unix(ts, 0)}
}

// Selecting the newest message must scroll it fully into view; the last
// message's bottom used to be computed as its top line + 1, leaving a tall
// message (e.g. a video thumbnail) cut at the bottom edge.
func TestSelectNewestTallMessageScrollsToItsBottom(t *testing.T) {
	m := New()
	m.SetSize(60, 10)
	m.SetFocused(true)
	m.SetChat("c@s.whatsapp.net", false, []core.Message{tallMsg("a", 100, 3), tallMsg("b", 200, 3), tallMsg("tall", 300, 12)})

	m.viewport.GotoTop()
	for i := 0; i < 5; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !m.viewport.AtBottom() {
		t.Errorf("newest message selected but viewport not at bottom (YOffset %d of %d lines)",
			m.viewport.YOffset, m.viewport.TotalLineCount())
	}
}

// Loading older messages keeps the message that was on top at the same
// screen row, instead of an estimate that ignored the removed loading line
// and a date separator shared with the new page.
func TestPrependKeepsPreviousTopMessageAnchored(t *testing.T) {
	m := New()
	m.SetSize(60, 10)
	m.SetFocused(true)
	day := int64(86400 * 20000)
	m.SetChat("c@s.whatsapp.net", false, []core.Message{tallMsg("x", day+300, 2), tallMsg("y", day+400, 20)})
	m.viewport.GotoTop()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // first key selects the newest message
	for i := 0; i < 3; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	beforeRow := m.lineOffsets[0] - m.viewport.YOffset

	m.PrependMessages([]core.Message{tallMsg("o1", day+100, 2), tallMsg("o2", day+200, 2)}) // same day as x
	idx := 2                                                                                // x after the prepend
	if got := m.lineOffsets[idx] - m.viewport.YOffset; got != beforeRow {
		t.Errorf("previous top message moved from row %d to row %d after prepend", beforeRow, got)
	}
}
