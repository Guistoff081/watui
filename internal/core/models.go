package core

import (
	"strings"
	"time"
)

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

// DisplayName returns the name to show for conv. Chats with no known name
// (numbers outside the address book with no push name) fall back to the
// phone number instead of the raw JID, like WhatsApp does.
func DisplayName(conv Conversation) string {
	if conv.Name != "" && conv.Name != conv.JID {
		return conv.Name
	}
	user, server, ok := strings.Cut(conv.JID, "@")
	if !ok || server != "s.whatsapp.net" {
		return conv.JID
	}
	if user == "0" {
		return "WhatsApp" // official service account
	}
	return formatPhone(user)
}

// formatPhone renders an international number the way WhatsApp does for
// Brazil (+55 AA NNNNN-NNNN / +55 AA NNNN-NNNN); other countries get the
// plain digits, since their grouping rules vary.
func formatPhone(digits string) string {
	if strings.HasPrefix(digits, "55") && (len(digits) == 12 || len(digits) == 13) {
		area, local := digits[2:4], digits[4:]
		split := len(local) - 4
		return "+55 " + area + " " + local[:split] + "-" + local[split:]
	}
	return "+" + digits
}

// NeedsPoster reports whether m's preview must come from a still frame
// extracted from the downloaded file: animated stickers (Go can't decode
// animated WebP) and GIFs/videos that arrived without an embedded thumbnail.
func (m Message) NeedsPoster() bool {
	switch m.MediaType {
	case "sticker":
		return m.IsAnimated
	case "gif", "video":
		return len(m.Thumbnail) == 0
	}
	return false
}

// PosterPath is where the still frame for the media cached at mediaPath lives.
func PosterPath(mediaPath string) string {
	return mediaPath + ".poster.png"
}
