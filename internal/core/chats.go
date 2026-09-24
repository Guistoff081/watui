package core

import (
	"sort"
	"strings"
)

// AliasResolver maps a chat JID to its LID↔PN alternate, returning "" when
// no alternate is known. The WhatsApp session implements it.
type AliasResolver interface {
	AltChatJID(jid string) string
}

// Receipt is a read receipt the caller must send: IDs of messages from Sender
// in Chat. In direct chats Sender equals Chat; in groups it is the author.
type Receipt struct {
	Chat   string
	Sender string
	IDs    []string
}

// Effects describes what the caller must do after a Chats operation. Chats
// never performs I/O itself; the caller persists, renders and sends.
type Effects struct {
	// Chat is the canonical (alias-resolved) JID the operation applied to.
	Chat string
	// Conversations changed: refresh them in the chat list and persist them.
	Conversations []Conversation
	// Messages to persist (insert, ignoring duplicates).
	Messages []Message
	// ClearUnread lists chats whose unread count was reset and must be
	// cleared in the chat list and the store.
	ClearUnread []string
	// Receipts to send to WhatsApp.
	Receipts []Receipt
	// Downloads lists messages whose media should be fetched in background.
	Downloads []Message
	// InView reports that Chat is the chat currently shown to the user.
	InView bool
	// Append holds messages to append to the open chat view.
	Append []Message
	// Prepend holds older messages to insert above the open chat history.
	Prepend []Message
	// NoOlder reports that there is no older history left to load for Chat.
	NoOlder bool
}

// StickerAutoDownloadCap bounds how many sticker downloads Open requests.
const StickerAutoDownloadCap = 10

// Chats owns conversation and message state and the rules that keep it
// consistent: LID↔PN alias folding, dedupe and ordering, unread counting,
// previews and read-receipt selection. It is not safe for concurrent use.
type Chats struct {
	aliases AliasResolver
	convs   map[string]Conversation
	msgs    map[string][]Message
}

// NewChats returns an empty engine. aliases may be nil when no LID↔PN
// mapping is available.
func NewChats(aliases AliasResolver) *Chats {
	return &Chats{
		aliases: aliases,
		convs:   make(map[string]Conversation),
		msgs:    make(map[string][]Message),
	}
}

func (c *Chats) alt(jid string) string {
	if c.aliases == nil {
		return ""
	}
	return c.aliases.AltChatJID(jid)
}

// Resolve maps a chat JID to the key of a known conversation, trying the
// LID↔PN alternate when jid itself is unknown. Unknown JIDs are returned as-is.
func (c *Chats) Resolve(jid string) string {
	if jid == "" {
		return jid
	}
	if _, ok := c.convs[jid]; ok {
		return jid
	}
	if alt := c.alt(jid); alt != "" {
		if _, ok := c.convs[alt]; ok {
			return alt
		}
	}
	return jid
}

// Aliases returns jid followed by its LID↔PN alternate, if known, for
// cache and store lookups.
func (c *Chats) Aliases(jid string) []string {
	if jid == "" {
		return nil
	}
	aliases := []string{jid}
	if alt := c.alt(jid); alt != "" && alt != jid {
		aliases = append(aliases, alt)
	}
	return aliases
}

// Conversation returns the conversation stored under jid.
func (c *Chats) Conversation(jid string) (Conversation, bool) {
	conv, ok := c.convs[jid]
	return conv, ok
}

// Messages returns the cached messages for a canonical chat JID, oldest
// first. The slice is shared with the engine and must not be modified.
func (c *Chats) Messages(jid string) []Message {
	return c.msgs[jid]
}

// Find returns the cached message msgID of chatJID (alias-resolved).
func (c *Chats) Find(chatJID, msgID string) (Message, bool) {
	for _, msg := range c.msgs[c.Resolve(chatJID)] {
		if msg.ID == msgID {
			return msg, true
		}
	}
	return Message{}, false
}

// Load registers conversations read from persistent storage.
func (c *Chats) Load(convs []Conversation) {
	for _, conv := range convs {
		c.convs[conv.JID] = conv
	}
}

// ApplyNames sets contact/group names on known conversations that have no
// name yet (or only their raw JID). Changed conversations are returned in
// JID order.
func (c *Chats) ApplyNames(names map[string]string) Effects {
	var eff Effects
	for jid, name := range names {
		conv, ok := c.convs[jid]
		if !ok || name == "" || (conv.Name != "" && conv.Name != conv.JID) {
			continue
		}
		conv.Name = name
		c.convs[jid] = conv
		eff.Conversations = append(eff.Conversations, conv)
	}
	sort.Slice(eff.Conversations, func(i, j int) bool {
		return eff.Conversations[i].JID < eff.Conversations[j].JID
	})
	return eff
}

// UpdateConversation stores a conversation update from the session. History
// sync may compute Last* from a partial batch, so an update never regresses
// the preview to an older message.
func (c *Chats) UpdateConversation(conv Conversation) Effects {
	if existing, ok := c.convs[conv.JID]; ok && !conv.LastMsgTime.After(existing.LastMsgTime) {
		conv.LastMessage = existing.LastMessage
		conv.LastMsgTime = existing.LastMsgTime
	}
	c.convs[conv.JID] = conv
	return Effects{Chat: conv.JID, Conversations: []Conversation{conv}}
}

// AddMessage records a live incoming (or own, from another device) message.
// viewing is the chat currently open in the UI ("" for none).
//
// Duplicates (same ID) are dropped: WhatsApp can dispatch a message twice,
// e.g. a group stanza carrying both pkmsg and skmsg. Offline-replayed older
// messages are inserted in order and never overwrite a newer preview. A
// message in a chat that is not open counts as unread unless it is our own;
// one in the open chat is appended to the view and receipted immediately.
func (c *Chats) AddMessage(msg Message, viewing string) Effects {
	jid := c.Resolve(msg.ChatJID)
	msg.ChatJID = jid
	c.mergeAliasCache(jid)

	eff := Effects{Chat: jid}
	for _, existing := range c.msgs[jid] {
		if existing.ID == msg.ID {
			return eff
		}
	}
	c.msgs[jid] = insertMessageSorted(c.msgs[jid], msg)
	eff.Messages = []Message{msg}

	conv, ok := c.convs[jid]
	if !ok {
		conv = Conversation{JID: jid, Name: jid, IsGroup: strings.HasSuffix(jid, "@g.us")}
	}
	// A 1:1 chat with no known name takes the sender's push name (numbers
	// outside the address book). Groups never do: that is a member's name.
	if !conv.IsGroup && !msg.IsFromMe && msg.SenderName != "" && (conv.Name == "" || conv.Name == conv.JID) {
		conv.Name = msg.SenderName
	}
	if msg.Timestamp.After(conv.LastMsgTime) {
		conv.LastMessage = msg.PreviewText()
		conv.LastMsgTime = msg.Timestamp
	}
	eff.InView = c.Resolve(viewing) == jid
	if !msg.IsFromMe && !eff.InView {
		conv.UnreadCount++
	}
	c.convs[jid] = conv
	eff.Conversations = []Conversation{conv}

	if eff.InView {
		eff.Append = []Message{msg}
		if !msg.IsFromMe {
			eff.Receipts = receiptsFor(jid, conv.IsGroup, eff.Append, 1)
		}
	}
	return eff
}

// AddOutgoing records an optimistic message the user just sent from the
// open chat and makes it the conversation preview.
func (c *Chats) AddOutgoing(msg Message) Effects {
	jid := msg.ChatJID
	c.msgs[jid] = append(c.msgs[jid], msg)
	eff := Effects{Chat: jid, Messages: []Message{msg}, InView: true, Append: []Message{msg}}
	if conv, ok := c.convs[jid]; ok {
		conv.LastMessage = msg.Content
		conv.LastMsgTime = msg.Timestamp
		c.convs[jid] = conv
		eff.Conversations = []Conversation{conv}
	}
	return eff
}

// AddHistory merges a history-sync batch into the chat cache without
// discarding live messages, and advances the preview if the batch holds a
// message newer than it. When InView is set the open chat must be reloaded
// from Messages(Chat).
func (c *Chats) AddHistory(chatJID string, msgs []Message, viewing string) Effects {
	jid := c.Resolve(chatJID)
	c.mergeAliasCache(jid)
	c.msgs[jid] = mergeMessages(c.msgs[jid], msgs)

	eff := Effects{Chat: jid, Messages: msgs, InView: c.Resolve(viewing) == jid}
	if len(msgs) > 0 {
		latest := msgs[0]
		for _, msg := range msgs {
			if msg.Timestamp.After(latest.Timestamp) {
				latest = msg
			}
		}
		if conv, ok := c.convs[jid]; ok && latest.Timestamp.After(conv.LastMsgTime) {
			conv.LastMessage = latest.PreviewText()
			conv.LastMsgTime = latest.Timestamp
			c.convs[jid] = conv
			eff.Conversations = []Conversation{conv}
		}
	}
	return eff
}

// Open prepares a known conversation for display. stored is the persisted
// recent history for Aliases(jid); it is merged with the cache (live and
// offline-sync messages) so the view shows both. The preview is advanced to
// the newest message, the unread count is cleared, receipts are produced
// for the messages that were unread, and missing sticker files are queued
// for download. The caller must show Messages(jid). ok is false when the
// conversation is unknown.
func (c *Chats) Open(jid string, stored []Message) (eff Effects, ok bool) {
	conv, ok := c.convs[jid]
	if !ok {
		return Effects{}, false
	}
	c.mergeAliasCache(jid)
	messages := mergeMessages(c.msgs[jid], stored)
	c.msgs[jid] = messages

	eff.Chat = jid
	previewChanged := false
	if len(messages) > 0 {
		if last := messages[len(messages)-1]; last.Timestamp.After(conv.LastMsgTime) {
			conv.LastMessage = last.PreviewText()
			conv.LastMsgTime = last.Timestamp
			previewChanged = true
		}
	}

	// The unread count bounds how many of the newest incoming messages still
	// need a receipt; it is captured before being cleared.
	unread := conv.UnreadCount
	conv.UnreadCount = 0
	c.convs[jid] = conv
	if previewChanged {
		eff.Conversations = []Conversation{conv}
	}
	eff.ClearUnread = []string{jid}
	eff.Receipts = receiptsFor(jid, conv.IsGroup, messages, unread)
	// Stickers carry no embedded thumbnail, so the file is needed to render.
	eff.Downloads = stickersToAutoDownload(messages, StickerAutoDownloadCap)
	return eff, true
}

// PrependOlder adds older messages loaded on demand for chatJID. Messages
// already cached are skipped; if nothing new remains, NoOlder is set.
func (c *Chats) PrependOlder(chatJID string, older []Message, viewing string) Effects {
	jid := c.Resolve(chatJID)
	eff := Effects{Chat: jid}

	cached := c.msgs[jid]
	seen := make(map[string]struct{}, len(cached))
	for _, msg := range cached {
		if msg.ID != "" {
			seen[msg.ID] = struct{}{}
		}
	}
	var fresh []Message
	for _, msg := range older {
		if _, dup := seen[msg.ID]; dup {
			continue
		}
		fresh = append(fresh, msg)
	}
	if len(fresh) == 0 {
		eff.NoOlder = true
		return eff
	}

	c.msgs[jid] = mergeMessages(cached, fresh)
	if c.Resolve(viewing) == jid {
		eff.InView = true
		eff.Prepend = fresh
	}
	return eff
}

// SetStatus updates the delivery status of a cached message. The caller
// persists the status and, when InView, updates the open view. ok is false
// (nothing to do) for an empty message ID.
func (c *Chats) SetStatus(chatJID, msgID, status, viewing string) (eff Effects, ok bool) {
	if msgID == "" {
		return Effects{}, false
	}
	jid := c.Resolve(chatJID)
	c.mergeAliasCache(jid)
	msgs := c.msgs[jid]
	for i := range msgs {
		if msgs[i].ID == msgID {
			msgs[i].Status = status
			break
		}
	}
	return Effects{Chat: jid, InView: c.Resolve(viewing) == jid}, true
}

// SetMediaPath records where a message's media was downloaded. The caller
// persists the path and, when InView, reloads the open view.
func (c *Chats) SetMediaPath(chatJID, msgID, path, viewing string) Effects {
	jid := c.Resolve(chatJID)
	msgs := c.msgs[jid]
	for i := range msgs {
		if msgs[i].ID == msgID {
			msgs[i].MediaPath = path
			break
		}
	}
	return Effects{Chat: jid, InView: c.Resolve(viewing) == jid}
}

// mergeAliasCache folds messages cached under an alternate JID into the
// canonical key.
func (c *Chats) mergeAliasCache(canonical string) {
	for _, alias := range c.Aliases(canonical) {
		if alias == canonical {
			continue
		}
		if msgs, ok := c.msgs[alias]; ok {
			c.msgs[canonical] = mergeMessages(c.msgs[canonical], msgs)
			delete(c.msgs, alias)
		}
	}
}

// receiptsFor picks receipts for the newest unread incoming messages of a
// chat (msgs is ascending by time). Own messages neither get a receipt nor
// consume the unread budget. Groups need one receipt per sender, in order of
// first appearance; group messages without a sender are skipped.
func receiptsFor(chat string, isGroup bool, msgs []Message, unread int) []Receipt {
	if len(msgs) == 0 || unread <= 0 {
		return nil
	}
	start := len(msgs)
	for n := 0; start > 0 && n < unread; {
		start--
		if !msgs[start].IsFromMe {
			n++
		}
	}

	var out []Receipt
	index := make(map[string]int)
	for _, msg := range msgs[start:] {
		if msg.IsFromMe {
			continue
		}
		sender := chat
		if isGroup {
			if msg.SenderJID == "" {
				continue
			}
			sender = msg.SenderJID
		}
		i, ok := index[sender]
		if !ok {
			i = len(out)
			index[sender] = i
			out = append(out, Receipt{Chat: chat, Sender: sender})
		}
		out[i].IDs = append(out[i].IDs, msg.ID)
	}
	return out
}

// stickersToAutoDownload returns up to limit stickers that still need their
// file downloaded, newest first. msgs is time-ascending, so walking backwards
// favours the stickers visible at the bottom of the chat.
func stickersToAutoDownload(msgs []Message, limit int) []Message {
	var out []Message
	for i := len(msgs) - 1; i >= 0 && len(out) < limit; i-- {
		msg := msgs[i]
		if msg.MediaType == "sticker" && msg.MediaPath == "" && msg.DirectPath != "" {
			out = append(out, msg)
		}
	}
	return out
}

// mergeMessages combines message lists into one slice, de-duplicating by ID
// (earlier lists win, so cached/live state isn't clobbered by history) and
// sorting ascending by timestamp.
func mergeMessages(lists ...[]Message) []Message {
	seen := make(map[string]struct{})
	var out []Message
	for _, list := range lists {
		for _, msg := range list {
			if msg.ID != "" {
				if _, ok := seen[msg.ID]; ok {
					continue
				}
				seen[msg.ID] = struct{}{}
			}
			out = append(out, msg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

// insertMessageSorted inserts msg into a time-ascending slice at the correct
// position, so offline-replayed messages with older timestamps land in order.
func insertMessageSorted(msgs []Message, msg Message) []Message {
	idx := sort.Search(len(msgs), func(i int) bool {
		return msgs[i].Timestamp.After(msg.Timestamp)
	})
	msgs = append(msgs, Message{})
	copy(msgs[idx+1:], msgs[idx:])
	msgs[idx] = msg
	return msgs
}
