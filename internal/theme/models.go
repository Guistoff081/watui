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
	// Content holds the bare caption for media messages, or the full text for text messages.
	// Use PreviewText() to get a human-readable preview for the conversation list.
	Content   string
	Timestamp time.Time
	IsFromMe  bool
	Status    string // sending/sent/delivered/read/received/failed

	// Media fields (zero values mean plain-text message)
	MediaType     string // "image"|"video"|"audio"|"voice"|"document"|"sticker"|"gif"
	MediaPath     string // local cache path once full-res is downloaded (empty until then)
	MimeType      string
	FileName      string // for document messages
	Thumbnail     []byte // embedded JPEG thumbnail (images/videos); ready immediately, no network
	Width, Height int
	Duration      int  // seconds (audio/video)
	IsAnimated    bool // animated sticker or GIF

	// Download metadata — needed to call DownloadMediaWithPath after a restart
	DirectPath    string
	MediaKey      []byte
	FileSHA256    []byte
	FileEncSHA256 []byte
}

// PreviewText returns a human-readable one-liner for the conversation list.
// For media messages it prepends the type tag; for text messages it returns Content as-is.
func (m Message) PreviewText() string {
	if m.MediaType == "" {
		return m.Content
	}
	icons := map[string]string{
		"image":    "[image]",
		"video":    "[video]",
		"audio":    "[audio]",
		"voice":    "[voice message]",
		"document": "[file]",
		"sticker":  "[sticker]",
		"gif":      "[GIF]",
	}
	tag := icons[m.MediaType]
	if tag == "" {
		tag = "[media]"
	}
	if m.Content != "" {
		return tag + " " + m.Content
	}
	return tag
}

// MediaDownloadedMsg is emitted when a media file has been successfully downloaded
// and saved to the local cache.
type MediaDownloadedMsg struct {
	ChatJID   string
	MessageID string
	Path      string
}

// MediaDownloadFailedMsg is emitted when a media download fails.
type MediaDownloadFailedMsg struct {
	ChatJID   string
	MessageID string
	Err       error
}
