package chatlist

import (
	"strings"
	"testing"

	"github.com/watui/watui/internal/theme"
)

func TestItemTitleFallsBackToJID(t *testing.T) {
	noName := NewItem(theme.Conversation{JID: "123@s.whatsapp.net"})
	if got := noName.Title(); got != "123@s.whatsapp.net" {
		t.Errorf("Title() = %q, want JID fallback", got)
	}
	named := NewItem(theme.Conversation{JID: "123@s.whatsapp.net", Name: "Alice"})
	if got := named.Title(); got != "Alice" {
		t.Errorf("Title() = %q, want Alice", got)
	}
}

func TestItemDescriptionTruncates(t *testing.T) {
	short := NewItem(theme.Conversation{LastMessage: "hello"})
	if got := short.Description(); got != "hello" {
		t.Errorf("Description() = %q, want hello", got)
	}

	long := NewItem(theme.Conversation{LastMessage: strings.Repeat("a", 60)})
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
		if got := NewItem(theme.Conversation{UnreadCount: count}).FormatUnread(); got != want {
			t.Errorf("FormatUnread(%d) = %q, want %q", count, got, want)
		}
	}
}

func TestItemFormatTimeZero(t *testing.T) {
	if got := NewItem(theme.Conversation{}).FormatTime(); got != "" {
		t.Errorf("FormatTime() = %q, want empty for zero time", got)
	}
}
