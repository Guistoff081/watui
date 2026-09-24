package chatlist

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/core"
)

func TestItemTitleFallsBackToPhoneNumber(t *testing.T) {
	noName := NewItem(core.Conversation{JID: "123@s.whatsapp.net"})
	if got := noName.Title(); got != "+123" {
		t.Errorf("Title() = %q, want phone number fallback", got)
	}
	named := NewItem(core.Conversation{JID: "123@s.whatsapp.net", Name: "Alice"})
	if got := named.Title(); got != "Alice" {
		t.Errorf("Title() = %q, want Alice", got)
	}
}

func TestItemDescriptionTruncates(t *testing.T) {
	short := NewItem(core.Conversation{LastMessage: "hello"})
	if got := short.Description(); got != "hello" {
		t.Errorf("Description() = %q, want hello", got)
	}

	long := NewItem(core.Conversation{LastMessage: strings.Repeat("a", 60)})
	got := long.Description()
	if !strings.HasSuffix(got, "...") {
		t.Errorf("Description() = %q, want truncated with ellipsis", got)
	}
	if len(got) != 43 { // 40 chars + "..."
		t.Errorf("Description() len = %d, want 43", len(got))
	}
}

func TestItemFormatUnread(t *testing.T) {
	cases := map[int]string{0: "", 5: "5", 99: "99", 100: "99+", 250: "99+"}
	for count, want := range cases {
		if got := NewItem(core.Conversation{UnreadCount: count}).FormatUnread(); got != want {
			t.Errorf("FormatUnread(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestItemFormatTimeZero(t *testing.T) {
	if got := NewItem(core.Conversation{}).FormatTime(); got != "" {
		t.Errorf("FormatTime() = %q, want empty for zero time", got)
	}
}

func TestItemDescriptionIsSingleLineAndRuneSafe(t *testing.T) {
	item := NewItem(core.Conversation{LastMessage: "Oi, tudo bem?\n\n*Ainda dá tempo* de um limite de crédito"})
	got := item.Description()
	if strings.Contains(got, "\n") {
		t.Errorf("Description() = %q, must be a single line", got)
	}
	if !utf8.ValidString(got) {
		t.Errorf("Description() = %q, cut a multi-byte character", got)
	}
}

func TestRenderItemHasFixedHeightAndWidth(t *testing.T) {
	m := New()
	m.SetSize(30, 20)
	long := core.Conversation{
		JID:         "5581934196700@s.whatsapp.net",
		Name:        "Nome de empresa muito comprido demais",
		LastMessage: "Atlantis Software, um limite de crédito foi disponibilizado\n\n📌 O vencimento",
		UnreadCount: 3,
	}
	for _, selected := range []bool{false, true} {
		block := m.renderItem(NewItem(long), selected, false)
		// The trailing spacer row (blank, or highlighted blanks when selected)
		// separates items; the item itself must be exactly two rows.
		lines := strings.Split(strings.TrimRight(block, " \n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("selected=%v: item renders %d lines, want 2: %q", selected, len(lines), block)
		}
		for _, l := range lines {
			if w := lipgloss.Width(l); w > 30 {
				t.Errorf("selected=%v: line width %d exceeds panel width 30: %q", selected, w, l)
			}
			if !utf8.ValidString(l) {
				t.Errorf("selected=%v: invalid UTF-8 in %q", selected, l)
			}
		}
	}
}

// The unread badge is rendered with padding (" 3 "), so row math must use its
// rendered width; with the raw width the preview row overflowed by one column
// and the badge wrapped into the spacer row of the selected item.
func TestRenderItemBadgeStaysOnPreviewRow(t *testing.T) {
	m := New()
	m.SetSize(30, 20)
	it := NewItem(core.Conversation{JID: "a@s.whatsapp.net", Name: "Elisson Guímel",
		LastMessage: "Olha os logs do watui", UnreadCount: 3, LastMsgTime: time.Now()})
	for _, selected := range []bool{false, true} {
		rows := strings.Split(m.renderItem(it, selected, false), "\n")
		if len(rows) != 3 {
			t.Fatalf("selected=%v: %d rows, want name, preview and spacer", selected, len(rows))
		}
		if !strings.Contains(rows[1], "3") {
			t.Errorf("selected=%v: badge not on the preview row: %q", selected, rows[1])
		}
		if strings.TrimSpace(ansi.Strip(rows[2])) != "" {
			t.Errorf("selected=%v: spacer row not empty (badge wrapped?): %q", selected, rows[2])
		}
		for i, r := range rows {
			if w := lipgloss.Width(r); w > 29 {
				t.Errorf("selected=%v: row %d is %d cols, want <= 29 (panel content)", selected, i, w)
			}
		}
	}
}
