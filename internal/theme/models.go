package theme

import (
	"time"

	"go.mau.fi/whatsmeow/types"
)

// Auth messages
type QRCodeMsg struct {
	Code string
}

type QRTimeoutMsg struct{}

type LoginSuccessMsg struct {
	JID types.JID
}

type LoginFailedMsg struct {
	Err error
}

// Connection messages
type ConnectedMsg struct {
	JID types.JID
}

type DisconnectedMsg struct {
	Err error
}

type ReconnectingMsg struct{}

// ClientOutdatedMsg is emitted when WhatsApp rejects the connection because the
// whatsmeow client version is too old (failure reason 405).
type ClientOutdatedMsg struct{}

// Chat messages
type ConversationListMsg struct {
	Conversations []Conversation
}

type ConversationUpdatedMsg struct {
	Conversation Conversation
}

type ChatSelectedMsg struct {
	JID types.JID
}

type MessagesLoadedMsg struct {
	ChatJID  types.JID
	Messages []Message
}

type NewMessageMsg struct {
	Message Message
}

type MessageSentMsg struct {
	ChatJID   types.JID
	MessageID string
	Timestamp time.Time
}

type MessageSendFailedMsg struct {
	ChatJID   types.JID
	MessageID string
	Err       error
}

type MessageStatusMsg struct {
	ChatJID   types.JID
	MessageID string
	Status    string
}

// Typing
type TypingMsg struct {
	ChatJID  types.JID
	Sender   types.JID
	IsTyping bool
}

// History sync
type HistorySyncProgressMsg struct {
	Progress int
}

type HistorySyncCompleteMsg struct{}

// Errors
type ErrorMsg struct {
	Err     error
	Context string
}

// Data models shared across packages
type Conversation struct {
	JID         string
	Name        string
	IsGroup     bool
	LastMessage string
	LastMsgTime time.Time
	UnreadCount int
	IsPinned    bool
}

type Message struct {
	ID         string
	ChatJID    string
	SenderJID  string
	SenderName string
	Content    string
	Timestamp  time.Time
	IsFromMe   bool
	Status     string // sending/sent/delivered/read/received/failed
}
