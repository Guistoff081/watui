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
		c.send(theme.TypingMsg{
			ChatJID:  evt.MessageSource.Chat,
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
	if content == "" {
		// For now, skip non-text messages
		content = "[media]"
	}

	senderName := ""
	if evt.Info.PushName != "" {
		senderName = evt.Info.PushName
	}

	msg := theme.Message{
		ID:         evt.Info.ID,
		ChatJID:    evt.Info.Chat.String(),
		SenderJID:  evt.Info.Sender.String(),
		SenderName: senderName,
		Content:    content,
		Timestamp:  evt.Info.Timestamp,
		IsFromMe:   evt.Info.IsFromMe,
		Status:     "received",
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

	for _, msgID := range evt.MessageIDs {
		c.send(theme.MessageStatusMsg{
			ChatJID:   evt.Chat,
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

		isGroup := parsedJID.Server == types.GroupServer
		name := conv.GetDisplayName()
		if name == "" {
			if isGroup {
				name = groupNames[jid]
			} else {
				name = c.GetContactName(parsedJID)
			}
		}

		convModel := theme.Conversation{
			JID:     jid,
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
			content := extractTextContent(wmi.Message)
			if content == "" {
				content = "[media]"
			}

			ts := time.Unix(int64(wmi.GetMessageTimestamp()), 0)

			msg := theme.Message{
				ID:        msgInfo.GetID(),
				ChatJID:   jid,
				SenderJID: msgInfo.GetParticipant(),
				Content:   content,
				Timestamp: ts,
				IsFromMe:  msgInfo.GetFromMe(),
				Status:    "received",
			}

			if msg.IsFromMe {
				msg.Status = "read"
				if msg.SenderJID == "" {
					msg.SenderJID = jid
				}
			}

			messages = append(messages, msg)

			if ts.After(lastTime) {
				lastTime = ts
				lastMsg = content
			}
		}

		convModel.LastMessage = lastMsg
		convModel.LastMsgTime = lastTime

		if len(messages) > 0 {
			c.send(theme.MessagesLoadedMsg{
				ChatJID:  parsedJID,
				Messages: messages,
			})
		}

		c.send(theme.ConversationUpdatedMsg{Conversation: convModel})
	}

	c.send(theme.HistorySyncCompleteMsg{})
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
