package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/watui/watui/internal/core"
)

func (c *Client) handleEvent(rawEvt interface{}) {
	if c.dbg != nil {
		c.dbg.Debug("whatsmeow event", "type", fmt.Sprintf("%T", rawEvt))
	}

	switch evt := rawEvt.(type) {
	case *events.Connected:
		jid := c.wm.Store.ID
		if jid != nil {
			c.send(core.Connected{JID: *jid})
		}
		_ = c.SendPresence(context.Background(), true)

	case *events.Disconnected:
		c.send(core.Disconnected{})

	case *events.LoggedOut:
		err := fmt.Errorf("logged out: %s", evt.Reason)
		if c.dbg != nil {
			c.dbg.Error(err, "whatsmeow logged out", "reason", evt.Reason)
		}
		c.send(core.Disconnected{Err: err})

	case *events.ClientOutdated:
		if c.dbg != nil {
			c.dbg.Error(fmt.Errorf("client outdated (405)"), "whatsmeow client version rejected by WhatsApp")
		}
		c.send(core.ClientOutdated{})

	case *events.QR:
		if len(evt.Codes) > 0 {
			c.send(core.QRCode{Code: evt.Codes[0]})
		}

	case *events.PairSuccess:
		c.send(core.LoginSuccess{JID: evt.ID})

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
		c.send(core.Typing{
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
	if !isDisplayable(evt.Message) {
		c.logSkipped(evt.Info.ID, evt.Message)
		return
	}

	chatJID := c.canonicalChatFromInfo(evt.Info)
	senderJID := c.canonicalSenderFromInfo(evt.Info)

	msg := newCoreMessage(evt.Message)
	msg.ID = evt.Info.ID
	msg.ChatJID = chatJID.String()
	msg.SenderJID = senderJID.String()
	msg.SenderName = evt.Info.PushName
	msg.Timestamp = evt.Info.Timestamp
	msg.IsFromMe = evt.Info.IsFromMe
	msg.Status = "received"

	if evt.Info.IsFromMe {
		msg.Status = "sent"
	}

	c.send(core.NewMessage{Message: msg})
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
		c.send(core.MessageStatus{
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

	ctx := context.Background()
	// Resolve group subjects once per sync instead of one network call per chat.
	groupNames, _ := c.GetGroupNames(ctx)
	r := &clientHistoryResolver{c: c, ctx: ctx, groupNames: groupNames}

	for _, conv := range data.GetConversations() {
		convModel, messages, ok := convertHistoryConversation(conv, r)
		if !ok {
			continue
		}

		// ConversationUpdated must arrive before MessagesLoaded so that
		// _foreign_keys=on does not silently drop history rows for new conversations.
		c.send(core.ConversationUpdated{Conversation: convModel})

		if len(messages) > 0 {
			chatJID, _ := types.ParseJID(convModel.JID)
			c.send(core.MessagesLoaded{
				ChatJID:  chatJID,
				Messages: messages,
			})
		}
	}

	c.send(core.HistorySyncComplete{})
}

// historyResolver supplies the lookups history conversion needs from the
// WhatsApp session, so convertHistoryConversation can be tested with a fake.
type historyResolver interface {
	// canonicalChatJID maps a chat JID to its stable key (PN preferred over LID).
	canonicalChatJID(chat types.JID) types.JID
	// contactName returns the best known name for a user JID, or "".
	contactName(jid types.JID) string
	// groupName returns the subject of a joined group, or "".
	groupName(jid types.JID) string
	// skipped is told about each non-displayable message that was dropped.
	skipped(id string, msg *waProto.Message)
}

type clientHistoryResolver struct {
	c          *Client
	ctx        context.Context
	groupNames map[string]string
}

func (r *clientHistoryResolver) canonicalChatJID(chat types.JID) types.JID {
	return r.c.canonicalChatJID(chat, types.EmptyJID)
}

func (r *clientHistoryResolver) contactName(jid types.JID) string {
	return r.c.GetContactName(r.ctx, jid)
}

func (r *clientHistoryResolver) groupName(jid types.JID) string {
	return r.groupNames[jid.String()]
}

func (r *clientHistoryResolver) skipped(id string, msg *waProto.Message) {
	r.c.logSkipped(id, msg)
}

// convertHistoryConversation converts one history-sync conversation into the
// conversation row and its displayable messages. ok is false when the
// conversation has no usable JID and should be ignored.
func convertHistoryConversation(conv *waHistorySync.Conversation, r historyResolver) (_ core.Conversation, _ []core.Message, ok bool) {
	parsedJID, err := types.ParseJID(conv.GetID())
	if conv.GetID() == "" || err != nil {
		return core.Conversation{}, nil, false
	}

	canonical := r.canonicalChatJID(parsedJID)
	canonicalStr := canonical.String()

	isGroup := canonical.Server == types.GroupServer
	name := conv.GetDisplayName()
	if name == "" {
		if isGroup {
			name = r.groupName(canonical)
		} else {
			name = r.contactName(canonical)
		}
	}

	convModel := core.Conversation{
		JID:         canonicalStr,
		Name:        name,
		IsGroup:     isGroup,
		UnreadCount: int(conv.GetUnreadCount()),
		IsPinned:    conv.GetPinned() > 0,
	}

	var messages []core.Message
	for _, hm := range conv.GetMessages() {
		wmi := hm.GetMessage()
		if wmi == nil || wmi.Message == nil {
			continue
		}

		key := wmi.GetKey()
		if !isDisplayable(wmi.Message) {
			r.skipped(key.GetID(), wmi.Message)
			continue
		}

		msg := newCoreMessage(wmi.Message)
		msg.ID = key.GetID()
		msg.ChatJID = canonicalStr
		msg.SenderJID = key.GetParticipant()
		msg.Timestamp = time.Unix(int64(wmi.GetMessageTimestamp()), 0)
		msg.IsFromMe = key.GetFromMe()
		msg.Status = "received"

		if msg.IsFromMe {
			msg.Status = "read"
			if msg.SenderJID == "" {
				msg.SenderJID = canonicalStr
			}
		}

		messages = append(messages, msg)

		if msg.Timestamp.After(convModel.LastMsgTime) {
			convModel.LastMsgTime = msg.Timestamp
			convModel.LastMessage = msg.PreviewText()
		}
	}

	return convModel, messages, true
}

// newCoreMessage fills the content and media fields of a core.Message from a
// displayable message. Media messages carry their bare caption (possibly "")
// as Content; other kinds get a text rendering, or "[media]" when none exists.
func newCoreMessage(m *waProto.Message) core.Message {
	meta := extractMedia(m)
	content := meta.caption
	if meta.mediaType == "" {
		content = extractTextContent(m)
		if content == "" {
			content = "[media]"
		}
	}
	return core.Message{
		Content:       content,
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
}

// nonDisplayableFields lists waE2E.Message fields (proto names) that carry no
// chat bubble of their own: transport metadata, reactions, protocol messages
// (revoke, edit, ephemeral setting, history-sync and app-state key shares),
// poll votes, pins and similar side effects. Edits and revokes are dropped
// until proper handling lands.
var nonDisplayableFields = map[protoreflect.Name]struct{}{
	"messageContextInfo":                         {},
	"senderKeyDistributionMessage":               {},
	"fastRatchetKeySenderKeyDistributionMessage": {},
	"reactionMessage":                            {},
	"encReactionMessage":                         {},
	"protocolMessage":                            {},
	"editedMessage":                              {},
	"pollUpdateMessage":                          {},
	"pollAddOptionMessage":                       {},
	"encEventResponseMessage":                    {},
	"keepInChatMessage":                          {},
	"pinInChatMessage":                           {},
	"stickerSyncRmrMessage":                      {},
	"placeholderMessage":                         {},
	"secretEncryptedMessage":                     {},
	"messageHistoryNotice":                       {},
	"groupRootKeyShare":                          {},
	"rootSecretDistributeMessage":                {},
}

// isDisplayable reports whether msg should appear in the chat. A message is
// displayable when it populates at least one field outside
// nonDisplayableFields, so unknown content-bearing kinds stay visible.
func isDisplayable(msg *waProto.Message) bool {
	if msg == nil {
		return false
	}
	displayable := false
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
		if _, skip := nonDisplayableFields[fd.Name()]; !skip {
			displayable = true
			return false
		}
		return true
	})
	return displayable
}

// logSkipped records which populated fields caused a message to be dropped.
func (c *Client) logSkipped(id string, msg *waProto.Message) {
	if c.dbg == nil {
		return
	}
	var fields []string
	if msg != nil {
		msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			fields = append(fields, string(fd.Name()))
			return true
		})
	}
	c.dbg.Debug("skipping non-displayable message", "id", id, "fields", strings.Join(fields, ","))
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
