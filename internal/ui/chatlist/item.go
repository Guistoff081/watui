package chatlist

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/core"
)

type Item struct {
	conversation core.Conversation
}

func NewItem(conv core.Conversation) Item {
	return Item{conversation: conv}
}

func (i Item) Title() string {
	return core.DisplayName(i.conversation)
}

// Description is the one-line preview: newlines and runs of whitespace are
// collapsed (business messages are multi-line) and it is cut by display
// width, never mid-character.
func (i Item) Description() string {
	preview := strings.Join(strings.Fields(i.conversation.LastMessage), " ")
	if ansi.StringWidth(preview) > 40 {
		preview = ansi.Truncate(preview, 40, "") + "..."
	}
	return preview
}

func (i Item) FilterValue() string {
	return i.Title()
}

func (i Item) JID() string {
	return i.conversation.JID
}

func (i Item) UnreadCount() int {
	return i.conversation.UnreadCount
}

func (i Item) IsPinned() bool {
	return i.conversation.IsPinned
}

func (i Item) LastMsgTime() time.Time {
	return i.conversation.LastMsgTime
}

func (i Item) FormatTime() string {
	t := i.conversation.LastMsgTime
	if t.IsZero() {
		return ""
	}

	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	if t.Year() == now.Year() && t.YearDay() == now.YearDay()-1 {
		return "Yesterday"
	}
	if now.Sub(t) < 7*24*time.Hour {
		return t.Format("Mon")
	}
	return t.Format("02/01/06")
}

func (i Item) FormatUnread() string {
	if i.conversation.UnreadCount == 0 {
		return ""
	}
	if i.conversation.UnreadCount > 99 {
		return "99+"
	}
	return fmt.Sprintf("%d", i.conversation.UnreadCount)
}

func (i Item) Conversation() core.Conversation {
	return i.conversation
}
