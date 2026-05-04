package chatlist

import (
	"fmt"
	"time"

	"github.com/watui/watui/internal/theme"
)

type Item struct {
	conversation theme.Conversation
}

func NewItem(conv theme.Conversation) Item {
	return Item{conversation: conv}
}

func (i Item) Title() string {
	name := i.conversation.Name
	if name == "" {
		name = i.conversation.JID
	}
	return name
}

func (i Item) Description() string {
	preview := i.conversation.LastMessage
	if len(preview) > 40 {
		preview = preview[:40] + "..."
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

func (i Item) Conversation() theme.Conversation {
	return i.conversation
}
