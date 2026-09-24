package core

import (
	"time"

	"go.mau.fi/whatsmeow/types"
)

// Event is a domain event emitted by the WhatsApp session. Events are plain
// values with no UI dependency; the Bubble Tea app receives them directly as
// tea.Msg, and a future daemon can serialize them.
type Event interface{ isEvent() }

// Auth messages
type QRCode struct {
	Code string
}

type QRTimeout struct{}

type LoginSuccess struct {
	JID types.JID
}

type LoginFailed struct {
	Err error
}

// Connection messages
type Connected struct {
	JID types.JID
}

type Disconnected struct {
	Err error
}

// ClientOutdated is emitted when WhatsApp rejects the connection because the
// whatsmeow client version is too old (failure reason 405).
type ClientOutdated struct{}

// Chat messages

type ConversationUpdated struct {
	Conversation Conversation
}

type MessagesLoaded struct {
	ChatJID  types.JID
	Messages []Message
}

type NewMessage struct {
	Message Message
}

type MessageSent struct {
	ChatJID   types.JID
	MessageID string
	Timestamp time.Time
}

type MessageSendFailed struct {
	ChatJID   types.JID
	MessageID string
	Err       error
}

type MessageStatus struct {
	ChatJID   types.JID
	MessageID string
	Status    string
}

// Typing
type Typing struct {
	ChatJID  types.JID
	Sender   types.JID
	IsTyping bool
}

// History sync
type HistorySyncComplete struct{}

// MediaDownloaded is emitted when a media file has been successfully downloaded
// and saved to the local cache.
type MediaDownloaded struct {
	ChatJID   string
	MessageID string
	Path      string
}

// MediaDownloadFailed is emitted when a media download fails.
type MediaDownloadFailed struct {
	ChatJID   string
	MessageID string
	Err       error
}

// isEvent implementations seal the Event interface to this package.
func (QRCode) isEvent()              {}
func (QRTimeout) isEvent()           {}
func (LoginSuccess) isEvent()        {}
func (LoginFailed) isEvent()         {}
func (Connected) isEvent()           {}
func (Disconnected) isEvent()        {}
func (ClientOutdated) isEvent()      {}
func (ConversationUpdated) isEvent() {}
func (MessagesLoaded) isEvent()      {}
func (NewMessage) isEvent()          {}
func (MessageSent) isEvent()         {}
func (MessageSendFailed) isEvent()   {}
func (MessageStatus) isEvent()       {}
func (Typing) isEvent()              {}
func (HistorySyncComplete) isEvent() {}
func (MediaDownloaded) isEvent()     {}
func (MediaDownloadFailed) isEvent() {}
