package whatsapp

import (
	"fmt"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/watui/watui/internal/theme"
)

func (c *Client) handleEvent(rawEvt interface{}) {
	if c.dbg != nil {
		c.dbg.Debug("whatsmeow event", "type", fmt.Sprintf("%T", rawEvt))
	}

	switch evt := rawEvt.(type) {
	case *events.Connected:
		jid := c.wm.Store.ID
		if jid != nil {
			c.send(theme.ConnectedMsg{JID: *jid})
		}
		c.SendPresence(true)

	case *events.Disconnected:
		c.send(theme.DisconnectedMsg{})

	case *events.LoggedOut:
		err := fmt.Errorf("logged out: %s", evt.Reason)
		if c.dbg != nil {
			c.dbg.Error(err, "whatsmeow logged out", "reason", evt.Reason)
		}
		c.send(theme.DisconnectedMsg{Err: err})

	case *events.ClientOutdated:
		if c.dbg != nil {
			c.dbg.Error(fmt.Errorf("client outdated (405)"), "whatsmeow client version rejected by WhatsApp")
		}
		c.send(theme.ClientOutdatedMsg{})

	case *events.QR:
		if len(evt.Codes) > 0 {
			c.send(theme.QRCodeMsg{Code: evt.Codes[0]})
		}

	case *events.PairSuccess:
		c.send(theme.LoginSuccessMsg{JID: evt.ID})

	case *events.Message:
		c.handleMessage(evt)

	case *events.Receipt:
		c.handleReceipt(evt)

	case *events.ChatPresence:
		alt := evt.MessageSource.SenderAlt
		if evt.MessageSource.IsFromMe {
			alt = evt.MessageSource.RecipientAlt
		}
		chatJID := c.canonicalChatJID(evt.MessageSource.Chat, alt)
		c.send(theme.TypingMsg{
			ChatJID:  chatJID,
			Sender:   evt.MessageSource.Sender,
			IsTyping: evt.State == types.ChatPresenceComposing,
		})

	case *events.HistorySync:
		c.handleHistorySync(evt)

	case *events.PushName:
		// Could update contact names
	}
}

func (c *Client) handleMessage(evt *events.Message) {
	content := extractTextContent(evt.Message)
	meta := extractMedia(evt.Message)

	if meta.mediaType != "" {
		// For media messages, Content holds the bare caption (may be "").
		content = meta.caption
	} else if content == "" {
		content = "[media]"
	}

	senderName := ""
	if evt.Info.PushName != "" {
		senderName = evt.Info.PushName
	}

	chatJID := c.canonicalChatFromInfo(evt.Info)
	senderJID := c.canonicalSenderFromInfo(evt.Info)

	msg := theme.Message{
		ID:         evt.Info.ID,
		ChatJID:    chatJID.String(),
		SenderJID:  senderJID.String(),
		SenderName: senderName,
		Content:    content,
		Timestamp:  evt.Info.Timestamp,
		IsFromMe:   evt.Info.IsFromMe,
		Status:     "received",

		MediaType:     meta.mediaType,
		MimeType:      meta.mimeType,
		FileName:      meta.fileName,
		Thumbnail:     meta.thumbnail,
		Width:         meta.width,
		Height:        meta.height,
		Duration:      meta.duration,
		IsAnimated:    meta.isAnimated,
		DirectPath:    meta.directPath,
		MediaKey:      meta.mediaKey,
		FileSHA256:    meta.fileSHA256,
		FileEncSHA256: meta.fileEncSHA256,
	}

	if evt.Info.IsFromMe {
		msg.Status = "sent"
	}

	c.send(theme.NewMessageMsg{Message: msg})
}

func (c *Client) handleReceipt(evt *events.Receipt) {
	var status string
	switch evt.Type {
	case types.ReceiptTypeDelivered:
		status = "delivered"
	case types.ReceiptTypeRead:
		status = "read"
	default:
		return
	}

	chatJID := c.canonicalChatJID(evt.Chat, types.EmptyJID)

	for _, msgID := range evt.MessageIDs {
		if msgID == "" {
			continue
		}
		c.send(theme.MessageStatusMsg{
			ChatJID:   chatJID,
			MessageID: string(msgID),
			Status:    status,
		})
	}
}

func (c *Client) handleHistorySync(evt *events.HistorySync) {
	data := evt.Data
	if data == nil {
		return
	}

	// Resolve group subjects once per sync instead of one network call per chat.
	groupNames := c.GetGroupNames()

	conversations := data.GetConversations()
	for _, conv := range conversations {
		jid := conv.GetID()
		if jid == "" {
			continue
		}

		parsedJID, err := types.ParseJID(jid)
		if err != nil {
			continue
		}

		canonical := c.canonicalChatJID(parsedJID, types.EmptyJID)
		canonicalStr := canonical.String()

		isGroup := canonical.Server == types.GroupServer
		name := conv.GetDisplayName()
		if name == "" {
			if isGroup {
				name = groupNames[canonicalStr]
			} else {
				name = c.GetContactName(canonical)
			}
		}

		convModel := theme.Conversation{
			JID:     canonicalStr,
			Name:    name,
			IsGroup: isGroup,
		}

		if conv.GetUnreadCount() > 0 {
			convModel.UnreadCount = int(conv.GetUnreadCount())
		}
		if conv.GetPinned() > 0 {
			convModel.IsPinned = true
		}

		// Process messages in this conversation
		var messages []theme.Message
		var lastMsg string
		var lastTime time.Time

		for _, hm := range conv.GetMessages() {
			wmi := hm.GetMessage()
			if wmi == nil || wmi.Message == nil {
				continue
			}

			msgInfo := wmi.GetKey()
			meta := extractMedia(wmi.Message)

			var content string
			if meta.mediaType != "" {
				content = meta.caption
			} else {
				content = extractTextContent(wmi.Message)
				if content == "" {
					content = "[media]"
				}
			}

			ts := time.Unix(int64(wmi.GetMessageTimestamp()), 0)

			msg := theme.Message{
				ID:        msgInfo.GetID(),
				ChatJID:   canonicalStr,
				SenderJID: msgInfo.GetParticipant(),
				Content:   content,
				Timestamp: ts,
				IsFromMe:  msgInfo.GetFromMe(),
				Status:    "received",

				MediaType:     meta.mediaType,
				MimeType:      meta.mimeType,
				FileName:      meta.fileName,
				Thumbnail:     meta.thumbnail,
				Width:         meta.width,
				Height:        meta.height,
				Duration:      meta.duration,
				IsAnimated:    meta.isAnimated,
				DirectPath:    meta.directPath,
				MediaKey:      meta.mediaKey,
				FileSHA256:    meta.fileSHA256,
				FileEncSHA256: meta.fileEncSHA256,
			}

			if msg.IsFromMe {
				msg.Status = "read"
				if msg.SenderJID == "" {
					msg.SenderJID = canonicalStr
				}
			}

			messages = append(messages, msg)

			preview := msg.PreviewText()
			if ts.After(lastTime) {
				lastTime = ts
				lastMsg = preview
			}
		}

		convModel.LastMessage = lastMsg
		convModel.LastMsgTime = lastTime

		// ConversationUpdatedMsg must arrive before MessagesLoadedMsg so that
		// _foreign_keys=on does not silently drop history rows for new conversations.
		c.send(theme.ConversationUpdatedMsg{Conversation: convModel})

		if len(messages) > 0 {
			c.send(theme.MessagesLoadedMsg{
				ChatJID:  canonical,
				Messages: messages,
			})
		}
	}

	c.send(theme.HistorySyncCompleteMsg{})
}

type mediaMeta struct {
	mediaType     string
	directPath    string
	mediaKey      []byte
	fileSHA256    []byte
	fileEncSHA256 []byte
	mimeType      string
	thumbnail     []byte
	width         int
	height        int
	duration      int
	isAnimated    bool
	fileName      string
	caption       string
}

// extractMedia returns download metadata and display hints for media messages.
// Returns a zero mediaMeta (mediaType == "") for plain-text messages.
func extractMedia(msg *waProto.Message) mediaMeta {
	if msg == nil {
		return mediaMeta{}
	}
	if img := msg.ImageMessage; img != nil {
		return mediaMeta{
			mediaType:     "image",
			directPath:    img.GetDirectPath(),
			mediaKey:      img.GetMediaKey(),
			fileSHA256:    img.GetFileSHA256(),
			fileEncSHA256: img.GetFileEncSHA256(),
			mimeType:      img.GetMimetype(),
			thumbnail:     img.GetJPEGThumbnail(),
			width:         int(img.GetWidth()),
			height:        int(img.GetHeight()),
			caption:       img.GetCaption(),
		}
	}
	if vid := msg.VideoMessage; vid != nil {
		mt := "video"
		if vid.GetGifPlayback() {
			mt = "gif"
		}
		return mediaMeta{
			mediaType:     mt,
			directPath:    vid.GetDirectPath(),
			mediaKey:      vid.GetMediaKey(),
			fileSHA256:    vid.GetFileSHA256(),
			fileEncSHA256: vid.GetFileEncSHA256(),
			mimeType:      vid.GetMimetype(),
			thumbnail:     vid.GetJPEGThumbnail(),
			width:         int(vid.GetWidth()),
			height:        int(vid.GetHeight()),
			duration:      int(vid.GetSeconds()),
			isAnimated:    vid.GetGifPlayback(),
			caption:       vid.GetCaption(),
		}
	}
	if aud := msg.AudioMessage; aud != nil {
		mt := "audio"
		if aud.GetPTT() {
			mt = "voice"
		}
		return mediaMeta{
			mediaType:     mt,
			directPath:    aud.GetDirectPath(),
			mediaKey:      aud.GetMediaKey(),
			fileSHA256:    aud.GetFileSHA256(),
			fileEncSHA256: aud.GetFileEncSHA256(),
			mimeType:      aud.GetMimetype(),
			duration:      int(aud.GetSeconds()),
		}
	}
	if doc := msg.DocumentMessage; doc != nil {
		return mediaMeta{
			mediaType:     "document",
			directPath:    doc.GetDirectPath(),
			mediaKey:      doc.GetMediaKey(),
			fileSHA256:    doc.GetFileSHA256(),
			fileEncSHA256: doc.GetFileEncSHA256(),
			mimeType:      doc.GetMimetype(),
			thumbnail:     doc.GetJPEGThumbnail(),
			fileName:      doc.GetFileName(),
			caption:       doc.GetCaption(),
		}
	}
	if stk := msg.StickerMessage; stk != nil {
		return mediaMeta{
			mediaType:     "sticker",
			directPath:    stk.GetDirectPath(),
			mediaKey:      stk.GetMediaKey(),
			fileSHA256:    stk.GetFileSHA256(),
			fileEncSHA256: stk.GetFileEncSHA256(),
			mimeType:      stk.GetMimetype(),
			width:         int(stk.GetWidth()),
			height:        int(stk.GetHeight()),
			isAnimated:    stk.GetIsAnimated(),
		}
	}
	return mediaMeta{}
}

func extractTextContent(msg *waProto.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Conversation != nil {
		return *msg.Conversation
	}
	if msg.ExtendedTextMessage != nil && msg.ExtendedTextMessage.Text != nil {
		return *msg.ExtendedTextMessage.Text
	}
	if msg.ImageMessage != nil {
		if c := msg.ImageMessage.GetCaption(); c != "" {
			return "[image] " + c
		}
		return "[image]"
	}
	if msg.VideoMessage != nil {
		if c := msg.VideoMessage.GetCaption(); c != "" {
			return "[video] " + c
		}
		return "[video]"
	}
	if msg.AudioMessage != nil {
		if msg.AudioMessage.GetPTT() {
			return "[voice message]"
		}
		return "[audio]"
	}
	if msg.DocumentMessage != nil {
		if n := msg.DocumentMessage.GetFileName(); n != "" {
			return "[file] " + n
		}
		return "[file]"
	}
	if msg.StickerMessage != nil {
		return "[sticker]"
	}
	if msg.LocationMessage != nil {
		return "[location]"
	}
	if msg.ContactMessage != nil {
		return "[contact] " + msg.ContactMessage.GetDisplayName()
	}
	return ""
}
