package whatsapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
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

	case *events.BusinessName:
		if evt.NewBusinessName != "" {
			c.send(core.ContactNameChanged{
				JID:  c.canonicalChatJID(evt.JID, types.EmptyJID).String(),
				Name: evt.NewBusinessName,
			})
		}

	case *events.PushName:
		// whatsmeow already stored it; tell the app so an unnamed chat (a
		// number outside the address book) picks it up without a restart.
		if evt.NewPushName != "" {
			c.send(core.ContactNameChanged{
				JID:  c.canonicalChatJID(evt.JID, evt.JIDAlt).String(),
				Name: evt.NewPushName,
			})
		}
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
	if msg.Content == unsupportedPlaceholder {
		c.logUnsupported(evt.Info.ID, evt.Message)
	}
	msg.ID = evt.Info.ID
	msg.ChatJID = chatJID.String()
	msg.SenderJID = senderJID.String()
	msg.SenderName = evt.Info.PushName
	if vn := evt.Info.VerifiedName; vn != nil && vn.Details.GetVerifiedName() != "" {
		msg.SenderName = vn.Details.GetVerifiedName() // businesses show their verified name
	}
	// In groups the member's address-book name wins, as in WhatsApp.
	if evt.Info.IsGroup && !evt.Info.IsFromMe {
		msg.SenderName = firstNonEmpty(c.GetContactName(context.Background(), senderJID), msg.SenderName)
	}
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
	// unsupported is told about each kept message whose kind has no rendering.
	unsupported(id string, msg *waProto.Message)
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

func (r *clientHistoryResolver) unsupported(id string, msg *waProto.Message) {
	r.c.logUnsupported(id, msg)
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
	var pushNameTime time.Time
	for _, hm := range conv.GetMessages() {
		wmi := hm.GetMessage()
		if wmi == nil || wmi.Message == nil {
			continue
		}

		key := wmi.GetKey()
		content := unwrapMessage(wmi.Message)
		if !isDisplayable(content) {
			r.skipped(key.GetID(), content)
			continue
		}

		msg := newCoreMessage(content)
		msg.ID = key.GetID()
		msg.ChatJID = canonicalStr
		msg.SenderJID = historySender(wmi, canonical)
		msg.Timestamp = time.Unix(int64(wmi.GetMessageTimestamp()), 0)
		msg.IsFromMe = key.GetFromMe()
		msg.Status = "received"
		if msg.IsFromMe {
			msg.Status = "read"
		} else {
			// Businesses show their verified name, like WhatsApp does.
			msg.SenderName = firstNonEmpty(wmi.GetVerifiedBizName(), wmi.GetPushName())
			// In groups the address-book name of the member wins, as in
			// WhatsApp; history messages often carry no push name at all.
			if isGroup {
				if sender, err := types.ParseJID(msg.SenderJID); err == nil {
					msg.SenderName = firstNonEmpty(r.contactName(sender), msg.SenderName)
				}
			}
		}
		if msg.Content == unsupportedPlaceholder {
			r.unsupported(msg.ID, content)
		}

		messages = append(messages, msg)

		// Numbers outside the address book have no contact name; the newest
		// push name they sent is the best label (WhatsApp shows "~Name").
		if name == "" && !isGroup && msg.SenderName != "" && !msg.Timestamp.Before(pushNameTime) {
			convModel.Name = msg.SenderName
			pushNameTime = msg.Timestamp
		}

		if msg.Timestamp.After(convModel.LastMsgTime) {
			convModel.LastMsgTime = msg.Timestamp
			convModel.LastMessage = msg.PreviewText()
		}
	}

	return convModel, messages, true
}

// historySender returns the SenderJID for a history message in chat, keyed the
// same way live messages are: 1:1 and newsletter messages are attributed to the
// canonical chat (whatsmeow's ParseWebMessage uses the chat as sender there),
// group and broadcast messages to the participant without its device suffix.
// Own messages keep their participant when present and otherwise fall back to
// the chat, as before.
func historySender(wmi *waWeb.WebMessageInfo, chat types.JID) string {
	participant := wmi.GetParticipant()
	if participant == "" {
		participant = wmi.GetKey().GetParticipant()
	}
	fromMe := wmi.GetKey().GetFromMe()

	switch {
	case fromMe && participant == "":
		return chat.String()
	case !fromMe && !(chat.Server == types.GroupServer || chat.Server == types.BroadcastServer):
		return chat.String()
	}
	if jid, err := types.ParseJID(participant); err == nil {
		return jid.ToNonAD().String()
	}
	return participant
}

// unwrapMessage strips the container messages WhatsApp wraps real content in
// (disappearing-chat EphemeralMessage, view-once, DocumentWithCaption, bot
// invoke, lottie sticker, device-sent echoes). Live messages arrive already
// unwrapped by whatsmeow's events.Message.UnwrapRaw; history-sync messages do
// not, so without this they would render as "[media]".
//
// It mirrors UnwrapRaw's order, with one deliberate difference: EditedMessage
// stays wrapped, so edits keep being dropped as non-displayable instead of
// appearing as a duplicate bubble.
func unwrapMessage(msg *waProto.Message) *waProto.Message {
	if m := msg.GetDeviceSentMessage().GetMessage(); m != nil {
		msg = m
	}
	for _, wrapper := range []func(*waProto.Message) *waProto.FutureProofMessage{
		(*waProto.Message).GetBotInvokeMessage,
		(*waProto.Message).GetEphemeralMessage,
		(*waProto.Message).GetViewOnceMessage,
		(*waProto.Message).GetViewOnceMessageV2,
		(*waProto.Message).GetViewOnceMessageV2Extension,
		(*waProto.Message).GetLottieStickerMessage,
		(*waProto.Message).GetDocumentWithCaptionMessage,
	} {
		if m := wrapper(msg).GetMessage(); m != nil {
			msg = m
		}
	}
	return msg
}

// unsupportedPlaceholder is the content of displayable messages whose kind we
// cannot render yet; the populated proto fields are logged at debug level.
const unsupportedPlaceholder = "[unsupported message]"

// newCoreMessage fills the content and media fields of a core.Message from a
// displayable message. Media messages carry their bare caption (possibly "")
// as Content; other kinds get a text rendering, or unsupportedPlaceholder when none exists.
func newCoreMessage(m *waProto.Message) core.Message {
	meta := extractMedia(m)
	content := meta.caption
	if meta.mediaType == "" {
		content = extractTextContent(m)
		if content == "" {
			content = unsupportedPlaceholder
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
	c.logFields("skipping non-displayable message", id, msg)
}

// logUnsupported records the fields of a message shown as unsupportedPlaceholder,
// so new kinds can be identified from the debug log and rendered.
func (c *Client) logUnsupported(id string, msg *waProto.Message) {
	c.logFields("unsupported message kind", id, msg)
}

func (c *Client) logFields(what, id string, msg *waProto.Message) {
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
	c.dbg.Debug(what, "id", id, "fields", strings.Join(fields, ","))
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
	return extractStructuredText(msg)
}

// extractStructuredText renders the message kinds businesses and newer clients
// send (templates, buttons, lists, polls, call logs…). Without it they fell
// back to "[media]", which is most of what unsaved business numbers send.
func extractStructuredText(msg *waProto.Message) string {
	switch {
	case msg.TemplateMessage != nil:
		t := msg.TemplateMessage
		if i := t.GetInteractiveMessageTemplate(); i != nil {
			return interactiveText(i)
		}
		h := t.GetHydratedTemplate()
		if h == nil {
			h = t.GetHydratedFourRowTemplate()
		}
		tag := headerTag(h.GetImageMessage() != nil, h.GetVideoMessage() != nil,
			h.GetDocumentMessage() != nil, h.GetLocationMessage() != nil)
		return tagged(tag, firstNonEmpty(h.GetHydratedContentText(), h.GetHydratedTitleText()))
	case msg.InteractiveMessage != nil:
		return interactiveText(msg.InteractiveMessage)
	case msg.ButtonsMessage != nil:
		return firstNonEmpty(msg.ButtonsMessage.GetContentText(), msg.ButtonsMessage.GetText())
	case msg.ListMessage != nil:
		return firstNonEmpty(msg.ListMessage.GetDescription(), msg.ListMessage.GetTitle())
	case msg.ButtonsResponseMessage != nil:
		return msg.ButtonsResponseMessage.GetSelectedDisplayText()
	case msg.TemplateButtonReplyMessage != nil:
		return msg.TemplateButtonReplyMessage.GetSelectedDisplayText()
	case msg.ListResponseMessage != nil:
		return msg.ListResponseMessage.GetTitle()
	case msg.InteractiveResponseMessage != nil:
		return msg.InteractiveResponseMessage.GetBody().GetText()
	case msg.HighlyStructuredMessage != nil:
		return msg.HighlyStructuredMessage.GetHydratedHsm().GetHydratedTemplate().GetHydratedContentText()
	}
	for _, p := range []*waProto.PollCreationMessage{
		msg.PollCreationMessage, msg.PollCreationMessageV2, msg.PollCreationMessageV3,
		msg.PollCreationMessageV5, msg.PollCreationMessageV6,
	} {
		if p != nil {
			return tagged("[poll]", p.GetName())
		}
	}
	switch {
	case msg.CallLogMesssage != nil:
		if msg.CallLogMesssage.GetIsVideo() {
			return "[video call]"
		}
		return "[call]"
	case msg.PtvMessage != nil:
		return "[video note]"
	case msg.LiveLocationMessage != nil:
		return "[live location]"
	case msg.ContactsArrayMessage != nil:
		return "[contacts]"
	case msg.GroupInviteMessage != nil:
		return tagged("[group invite]", msg.GroupInviteMessage.GetGroupName())
	case msg.EventMessage != nil:
		return tagged("[event]", msg.EventMessage.GetName())
	case msg.ProductMessage != nil:
		return tagged("[product]", msg.ProductMessage.GetProduct().GetTitle())
	case msg.OrderMessage != nil:
		return "[order]"
	case msg.RequestPaymentMessage != nil, msg.SendPaymentMessage != nil, msg.PaymentInviteMessage != nil:
		return "[payment]"
	case msg.AlbumMessage != nil:
		return "[album]"
	}
	return ""
}

// interactiveText renders an interactive message: its body (or header title),
// prefixed with the header media kind when there is one.
func interactiveText(i *waProto.InteractiveMessage) string {
	h := i.GetHeader()
	tag := headerTag(h.GetImageMessage() != nil, h.GetVideoMessage() != nil,
		h.GetDocumentMessage() != nil, h.GetLocationMessage() != nil)
	return tagged(tag, firstNonEmpty(i.GetBody().GetText(), h.GetTitle()))
}

// headerTag names the media shown above a template/interactive message, as
// WhatsApp's list preview does with its camera/video icons.
func headerTag(image, video, document, location bool) string {
	switch {
	case image:
		return "[image]"
	case video:
		return "[video]"
	case document:
		return "[file]"
	case location:
		return "[location]"
	}
	return ""
}

// tagged joins a kind tag and an optional detail ("[poll] Lunch?" / "[poll]").
func tagged(tag, detail string) string {
	switch {
	case tag == "":
		return detail
	case detail == "":
		return tag
	}
	return tag + " " + detail
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
