package core

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

const (
	pnJID  = "1234567890@s.whatsapp.net"
	lidJID = "998877665544@lid"
)

// aliasMap is an AliasResolver backed by a static map.
type aliasMap map[string]string

func (a aliasMap) AltChatJID(jid string) string { return a[jid] }

func pnLIDAliases() aliasMap { return aliasMap{lidJID: pnJID, pnJID: lidJID} }

func msgIDs(msgs []Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

// incoming builds n incoming messages m1..mn for a chat, oldest first.
func incoming(jid string, n int) []Message {
	msgs := make([]Message, n)
	for i := range msgs {
		msgs[i] = Message{
			ID:        fmt.Sprintf("m%d", i+1),
			ChatJID:   jid,
			Content:   "x",
			Timestamp: time.Unix(int64(100+i), 0),
		}
	}
	return msgs
}

// stickerMsgs builds n sticker messages (s0..s{n-1}) in ascending time order,
// all downloadable (DirectPath set, MediaPath empty).
func stickerMsgs(jid string, n int) []Message {
	msgs := make([]Message, n)
	for i := range msgs {
		msgs[i] = Message{
			ID:         fmt.Sprintf("s%d", i),
			ChatJID:    jid,
			MediaType:  "sticker",
			DirectPath: "/direct/" + fmt.Sprint(i),
			Timestamp:  time.Unix(int64(100+i), 0),
		}
	}
	return msgs
}

// newChats returns an engine preloaded with convs.
func newChats(aliases AliasResolver, convs ...Conversation) *Chats {
	c := NewChats(aliases)
	c.Load(convs)
	return c
}

// --- merge helpers ---

func TestMergeMessagesDedupesAndSorts(t *testing.T) {
	cache := []Message{
		{ID: "c", Timestamp: time.Unix(300, 0)},
		{ID: "b", Timestamp: time.Unix(200, 0), Content: "cache-b"},
	}
	stored := []Message{
		{ID: "a", Timestamp: time.Unix(100, 0)},
		{ID: "b", Timestamp: time.Unix(200, 0), Content: "stored-b"},
	}

	out := mergeMessages(cache, stored)
	if got, want := msgIDs(out), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
	if out[1].Content != "cache-b" {
		t.Errorf("dedup kept %q, want cache-b (earlier list wins)", out[1].Content)
	}
}

func TestMergeMessagesEmpty(t *testing.T) {
	if out := mergeMessages(nil, nil); out != nil {
		t.Errorf("mergeMessages(nil, nil) = %v, want nil", out)
	}
}

func TestMergeMessagesKeepsEmptyIDs(t *testing.T) {
	out := mergeMessages([]Message{{Timestamp: time.Unix(2, 0)}}, []Message{{Timestamp: time.Unix(1, 0)}})
	if len(out) != 2 {
		t.Fatalf("merged len = %d, want 2 (messages without ID are never deduped)", len(out))
	}
}

func TestInsertMessageSorted(t *testing.T) {
	msgs := []Message{
		{ID: "a", Timestamp: time.Unix(100, 0)},
		{ID: "c", Timestamp: time.Unix(300, 0)},
	}

	msgs = insertMessageSorted(msgs, Message{ID: "b", Timestamp: time.Unix(200, 0)})
	if got, want := msgIDs(msgs), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after middle insert = %v, want %v", got, want)
	}

	msgs = insertMessageSorted(msgs, Message{ID: "z", Timestamp: time.Unix(50, 0)})
	if msgs[0].ID != "z" {
		t.Errorf("oldest insert: first = %s, want z", msgs[0].ID)
	}

	msgs = insertMessageSorted(msgs, Message{ID: "n", Timestamp: time.Unix(400, 0)})
	if msgs[len(msgs)-1].ID != "n" {
		t.Errorf("newest insert: last = %s, want n", msgs[len(msgs)-1].ID)
	}
}

func TestInsertMessageSortedIntoEmpty(t *testing.T) {
	msgs := insertMessageSorted(nil, Message{ID: "a", Timestamp: time.Unix(1, 0)})
	if got := msgIDs(msgs); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("insert into empty = %v, want [a]", got)
	}
}

// --- stickers ---

func TestStickersToAutoDownloadNewestFirst(t *testing.T) {
	got := msgIDs(stickersToAutoDownload(stickerMsgs("c@s.whatsapp.net", 5), 3))
	if want := []string{"s4", "s3", "s2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stickersToAutoDownload() = %v, want %v", got, want)
	}
}

func TestStickersToAutoDownloadSkipsIneligible(t *testing.T) {
	msgs := stickerMsgs("c@s.whatsapp.net", 5)
	msgs[4].MediaPath = "/cache/s4.webp" // already downloaded
	msgs[3].DirectPath = ""              // nothing to download from
	msgs[2].MediaType = "image"          // not a sticker
	got := msgIDs(stickersToAutoDownload(msgs, 10))
	if want := []string{"s1", "s0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("stickersToAutoDownload() = %v, want %v", got, want)
	}
}

func TestStickersToAutoDownloadEmpty(t *testing.T) {
	if got := stickersToAutoDownload(nil, 10); len(got) != 0 {
		t.Fatalf("stickersToAutoDownload(nil) = %v, want empty", msgIDs(got))
	}
	if got := stickersToAutoDownload(stickerMsgs("c@s.whatsapp.net", 3), 0); len(got) != 0 {
		t.Fatalf("stickersToAutoDownload(limit 0) = %v, want empty", msgIDs(got))
	}
}

// --- receipts ---

func TestReceiptsForDirectChat(t *testing.T) {
	jid := "a@s.whatsapp.net"
	msgs := incoming(jid, 5)
	// An own message in between must neither be marked nor consume the budget.
	msgs = append(msgs[:4:4], Message{ID: "own", ChatJID: jid, IsFromMe: true, Timestamp: time.Unix(103, 500)}, msgs[4])

	got := receiptsFor(jid, false, msgs, 2)
	want := []Receipt{{Chat: jid, Sender: jid, IDs: []string{"m4", "m5"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("receiptsFor() = %+v, want %+v", got, want)
	}
}

func TestReceiptsForGroupGroupsBySender(t *testing.T) {
	jid := "g@g.us"
	alice, bob := "alice@s.whatsapp.net", "bob@s.whatsapp.net"
	msgs := incoming(jid, 6)
	for i, s := range []string{alice, bob, alice, bob, alice, ""} {
		msgs[i].SenderJID = s
	}

	got := receiptsFor(jid, true, msgs, 4)
	// m6 has no sender and cannot be receipted in a group, but still counts as unread.
	want := []Receipt{
		{Chat: jid, Sender: alice, IDs: []string{"m3", "m5"}},
		{Chat: jid, Sender: bob, IDs: []string{"m4"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("receiptsFor() = %+v, want %+v", got, want)
	}
}

func TestReceiptsForNothingUnread(t *testing.T) {
	jid := "a@s.whatsapp.net"
	if got := receiptsFor(jid, false, incoming(jid, 3), 0); got != nil {
		t.Errorf("unread 0: receipts = %+v, want none", got)
	}
	if got := receiptsFor(jid, false, nil, 3); got != nil {
		t.Errorf("no messages: receipts = %+v, want none", got)
	}
	own := []Message{{ID: "me", IsFromMe: true}}
	if got := receiptsFor(jid, false, own, 1); got != nil {
		t.Errorf("own only: receipts = %+v, want none", got)
	}
}

func TestReceiptsForUnreadExceedsMessages(t *testing.T) {
	jid := "a@s.whatsapp.net"
	got := receiptsFor(jid, false, incoming(jid, 2), 10)
	want := []Receipt{{Chat: jid, Sender: jid, IDs: []string{"m1", "m2"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("receiptsFor() = %+v, want %+v", got, want)
	}
}

// --- aliases ---

func TestResolveUsesAlt(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID, Name: "Alice"})
	if got := c.Resolve(lidJID); got != pnJID {
		t.Fatalf("Resolve(%q) = %q, want %q", lidJID, got, pnJID)
	}
	if got := c.Resolve(pnJID); got != pnJID {
		t.Fatalf("Resolve(%q) = %q, want itself", pnJID, got)
	}
}

func TestResolveUnknownKeepsJID(t *testing.T) {
	c := newChats(pnLIDAliases())
	if got := c.Resolve(lidJID); got != lidJID {
		t.Errorf("Resolve(unknown) = %q, want %q", got, lidJID)
	}
	if got := c.Resolve(""); got != "" {
		t.Errorf("Resolve(\"\") = %q, want empty", got)
	}
	if got := NewChats(nil).Resolve(lidJID); got != lidJID {
		t.Errorf("nil resolver: Resolve = %q, want %q", got, lidJID)
	}
}

func TestAliases(t *testing.T) {
	c := newChats(pnLIDAliases())
	if got, want := c.Aliases(pnJID), []string{pnJID, lidJID}; !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases(pn) = %v, want %v", got, want)
	}
	if got, want := c.Aliases("x@s.whatsapp.net"), []string{"x@s.whatsapp.net"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases(no alt) = %v, want %v", got, want)
	}
	if got := c.Aliases(""); got != nil {
		t.Errorf("Aliases(\"\") = %v, want nil", got)
	}
	self := NewChats(aliasMap{pnJID: pnJID})
	if got, want := self.Aliases(pnJID), []string{pnJID}; !reflect.DeepEqual(got, want) {
		t.Errorf("Aliases(self alt) = %v, want %v", got, want)
	}
}

func TestMergeAliasCache(t *testing.T) {
	c := newChats(pnLIDAliases())
	c.msgs[lidJID] = []Message{{ID: "m1", ChatJID: lidJID, Timestamp: time.Unix(1, 0)}}
	c.msgs[pnJID] = []Message{{ID: "m2", ChatJID: pnJID, Timestamp: time.Unix(2, 0)}}

	c.mergeAliasCache(pnJID)

	if _, ok := c.msgs[lidJID]; ok {
		t.Fatalf("alias cache should be removed")
	}
	if got, want := msgIDs(c.Messages(pnJID)), []string{"m1", "m2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
}

// --- conversations ---

func TestLoadAndConversation(t *testing.T) {
	c := newChats(nil, Conversation{JID: "a@s.whatsapp.net", Name: "A"})
	conv, ok := c.Conversation("a@s.whatsapp.net")
	if !ok || conv.Name != "A" {
		t.Fatalf("Conversation() = %+v, %v; want A, true", conv, ok)
	}
	if _, ok := c.Conversation("b@s.whatsapp.net"); ok {
		t.Fatalf("Conversation(unknown) ok = true, want false")
	}
}

func TestApplyNames(t *testing.T) {
	c := newChats(nil,
		Conversation{JID: "a@s.whatsapp.net"},                           // no name → set
		Conversation{JID: "b@s.whatsapp.net", Name: "b@s.whatsapp.net"}, // raw JID → set
		Conversation{JID: "c@s.whatsapp.net", Name: "Custom"},           // named → keep
		Conversation{JID: "d@s.whatsapp.net"},                           // empty new name → keep
	)
	eff := c.ApplyNames(map[string]string{
		"a@s.whatsapp.net": "Alice",
		"b@s.whatsapp.net": "Bob",
		"c@s.whatsapp.net": "Carol",
		"d@s.whatsapp.net": "",
		"z@s.whatsapp.net": "Unknown chat",
	})

	var got []string
	for _, conv := range eff.Conversations {
		got = append(got, conv.JID+"="+conv.Name)
	}
	if want := []string{"a@s.whatsapp.net=Alice", "b@s.whatsapp.net=Bob"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	if conv, _ := c.Conversation("c@s.whatsapp.net"); conv.Name != "Custom" {
		t.Errorf("named conversation renamed to %q", conv.Name)
	}
	if _, ok := c.Conversation("z@s.whatsapp.net"); ok {
		t.Errorf("names must not create conversations")
	}
}

func TestUpdateConversationDoesNotRegressPreview(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, LastMessage: "live", LastMsgTime: time.Unix(500, 0)})

	eff := c.UpdateConversation(Conversation{JID: jid, Name: "A", LastMessage: "hist", LastMsgTime: time.Unix(100, 0)})

	conv, _ := c.Conversation(jid)
	if conv.Name != "A" || conv.LastMessage != "live" || !conv.LastMsgTime.Equal(time.Unix(500, 0)) {
		t.Fatalf("conv = %+v, want name A and live preview kept", conv)
	}
	if len(eff.Conversations) != 1 || eff.Conversations[0] != conv || eff.Chat != jid {
		t.Errorf("effects = %+v, want upsert of %+v", eff, conv)
	}
}

func TestUpdateConversationAdvancesPreview(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, LastMessage: "old", LastMsgTime: time.Unix(100, 0)})

	c.UpdateConversation(Conversation{JID: jid, LastMessage: "new", LastMsgTime: time.Unix(200, 0)})
	if conv, _ := c.Conversation(jid); conv.LastMessage != "new" {
		t.Errorf("LastMessage = %q, want new", conv.LastMessage)
	}

	c.UpdateConversation(Conversation{JID: "n@s.whatsapp.net", LastMessage: "hi"})
	if conv, ok := c.Conversation("n@s.whatsapp.net"); !ok || conv.LastMessage != "hi" {
		t.Errorf("new conversation = %+v, %v; want stored as-is", conv, ok)
	}
}

// --- new messages ---

func TestAddMessageResolvesLIDToExistingConversation(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID, Name: "Alice"})

	eff := c.AddMessage(Message{ID: "live1", ChatJID: lidJID, Content: "hello via lid", Timestamp: time.Unix(500, 0)}, pnJID)

	if got := c.Messages(pnJID); len(got) != 1 || got[0].ID != "live1" || got[0].ChatJID != pnJID {
		t.Fatalf("Messages(pn) = %+v, want live1 re-keyed under phone JID", got)
	}
	if got := c.Messages(lidJID); len(got) != 0 {
		t.Fatalf("message should not be cached under LID key")
	}
	if conv, _ := c.Conversation(pnJID); conv.LastMessage != "hello via lid" {
		t.Fatalf("preview = %q, want hello via lid", conv.LastMessage)
	}
	if eff.Chat != pnJID || !eff.InView || len(eff.Append) != 1 || eff.Append[0].ChatJID != pnJID {
		t.Fatalf("effects = %+v, want in-view append under pn", eff)
	}
	if len(eff.Messages) != 1 || eff.Messages[0].ChatJID != pnJID {
		t.Errorf("persisted = %+v, want message keyed under pn", eff.Messages)
	}
}

func TestAddMessageMergesAliasCache(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID})
	c.msgs[lidJID] = []Message{{ID: "old", ChatJID: lidJID, Timestamp: time.Unix(1, 0)}}

	c.AddMessage(Message{ID: "new", ChatJID: pnJID, Timestamp: time.Unix(2, 0)}, "")

	if got, want := msgIDs(c.Messages(pnJID)), []string{"old", "new"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Messages(pn) = %v, want %v", got, want)
	}
}

func TestAddMessageDeduplicates(t *testing.T) {
	jid := "123@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	msg := Message{ID: "m1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)}
	c.AddMessage(msg, "")
	eff := c.AddMessage(msg, "") // duplicate dispatch (e.g. group pkmsg+skmsg)

	if got := c.Messages(jid); len(got) != 1 {
		t.Fatalf("messages = %v, want exactly 1 (deduped)", msgIDs(got))
	}
	if conv, _ := c.Conversation(jid); conv.UnreadCount != 1 {
		t.Errorf("UnreadCount = %d, want 1 (duplicate must not double-count)", conv.UnreadCount)
	}
	if len(eff.Messages) != 0 || len(eff.Conversations) != 0 || len(eff.Append) != 0 || eff.InView {
		t.Errorf("duplicate effects = %+v, want none", eff)
	}
}

func TestAddMessageOrdersOfflineReplay(t *testing.T) {
	jid := "g@g.us"
	c := newChats(nil, Conversation{JID: jid})

	// Newest arrives first, then an older (offline-replayed) message.
	c.AddMessage(Message{ID: "new", ChatJID: jid, Content: "newest", Timestamp: time.Unix(300, 0)}, "")
	c.AddMessage(Message{ID: "old", ChatJID: jid, Content: "older", Timestamp: time.Unix(100, 0)}, "")

	if got, want := msgIDs(c.Messages(jid)), []string{"old", "new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	// The preview must reflect the newer message, not the offline-replayed one.
	conv, _ := c.Conversation(jid)
	if !conv.LastMsgTime.Equal(time.Unix(300, 0)) || conv.LastMessage != "newest" {
		t.Errorf("preview = (%q, %v), want (newest, 300)", conv.LastMessage, conv.LastMsgTime.Unix())
	}
}

func TestAddMessageOwnMessageNotUnread(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	c.AddMessage(Message{ID: "me1", ChatJID: jid, Content: "sent from phone", IsFromMe: true, Timestamp: time.Unix(100, 0)}, "")

	if conv, _ := c.Conversation(jid); conv.UnreadCount != 0 {
		t.Errorf("UnreadCount = %d, want 0 for own message", conv.UnreadCount)
	}
}

func TestAddMessageBackgroundChatCountsUnread(t *testing.T) {
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: a}, Conversation{JID: b})

	eff := c.AddMessage(Message{ID: "n1", ChatJID: a, Content: "hi", Timestamp: time.Unix(100, 0)}, b)

	conv, _ := c.Conversation(a)
	if conv.UnreadCount != 1 {
		t.Errorf("UnreadCount = %d, want 1", conv.UnreadCount)
	}
	if eff.InView || len(eff.Append) != 0 || len(eff.Receipts) != 0 {
		t.Errorf("effects = %+v, want no view append and no receipts for background chat", eff)
	}
	if len(eff.Conversations) != 1 || eff.Conversations[0] != conv {
		t.Errorf("upserts = %+v, want %+v", eff.Conversations, conv)
	}
}

func TestAddMessagePreviewUsesPreviewText(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	c.AddMessage(Message{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)}, "")

	if conv, _ := c.Conversation(jid); conv.LastMessage != "[image]" {
		t.Errorf("LastMessage = %q, want [image]", conv.LastMessage)
	}
}

func TestAddMessageCreatesConversation(t *testing.T) {
	c := NewChats(nil)

	eff := c.AddMessage(Message{ID: "g1", ChatJID: "g@g.us", SenderName: "Alice", Content: "hi", Timestamp: time.Unix(100, 0)}, "")
	conv, ok := c.Conversation("g@g.us")
	want := Conversation{JID: "g@g.us", Name: "Alice", IsGroup: true, LastMessage: "hi", LastMsgTime: time.Unix(100, 0), UnreadCount: 1}
	if !ok || conv != want {
		t.Fatalf("conv = %+v, want %+v", conv, want)
	}
	if len(eff.Conversations) != 1 || eff.Conversations[0] != want {
		t.Errorf("upserts = %+v, want %+v", eff.Conversations, want)
	}

	c.AddMessage(Message{ID: "d1", ChatJID: "d@s.whatsapp.net", Timestamp: time.Unix(100, 0)}, "")
	if conv, _ := c.Conversation("d@s.whatsapp.net"); conv.Name != "d@s.whatsapp.net" || conv.IsGroup {
		t.Errorf("conv = %+v, want name falling back to JID and not a group", conv)
	}
}

func TestAddMessageInOpenChatMarksRead(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff := c.AddMessage(Message{ID: "n1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)}, jid)

	if want := []Receipt{{Chat: jid, Sender: jid, IDs: []string{"n1"}}}; !reflect.DeepEqual(eff.Receipts, want) {
		t.Fatalf("receipts = %+v, want %+v", eff.Receipts, want)
	}
	if got := msgIDs(eff.Append); !reflect.DeepEqual(got, []string{"n1"}) || !eff.InView {
		t.Errorf("append = %v (in view %v), want [n1]", got, eff.InView)
	}
	if conv, _ := c.Conversation(jid); conv.UnreadCount != 0 {
		t.Errorf("UnreadCount = %d, want 0 for open chat", conv.UnreadCount)
	}
}

func TestAddMessageInOpenGroupMarksReadWithSender(t *testing.T) {
	jid, sender := "g@g.us", "alice@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, IsGroup: true})

	eff := c.AddMessage(Message{ID: "n1", ChatJID: jid, SenderJID: sender, Content: "hi", Timestamp: time.Unix(100, 0)}, jid)

	if want := []Receipt{{Chat: jid, Sender: sender, IDs: []string{"n1"}}}; !reflect.DeepEqual(eff.Receipts, want) {
		t.Fatalf("receipts = %+v, want %+v", eff.Receipts, want)
	}
}

func TestAddMessageOwnInOpenChatNotMarked(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff := c.AddMessage(Message{ID: "me1", ChatJID: jid, IsFromMe: true, Content: "hi", Timestamp: time.Unix(100, 0)}, jid)

	if len(eff.Receipts) != 0 {
		t.Errorf("receipts = %+v, want none for own message", eff.Receipts)
	}
	if len(eff.Append) != 1 {
		t.Errorf("append = %v, want own message shown", msgIDs(eff.Append))
	}
}

func TestAddMessageViewingAliasCountsAsOpen(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID})

	eff := c.AddMessage(Message{ID: "n1", ChatJID: pnJID, Timestamp: time.Unix(1, 0)}, lidJID)

	if !eff.InView {
		t.Errorf("viewing the LID alias of the chat must count as open")
	}
}

// --- outgoing ---

func TestAddOutgoing(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, LastMessage: "old", LastMsgTime: time.Unix(500, 0)})
	msg := Message{ID: "o1", ChatJID: jid, Content: "hello", IsFromMe: true, Status: "sending", Timestamp: time.Unix(100, 0)}

	eff := c.AddOutgoing(msg)

	if got := msgIDs(c.Messages(jid)); !reflect.DeepEqual(got, []string{"o1"}) {
		t.Fatalf("cache = %v, want [o1]", got)
	}
	conv, _ := c.Conversation(jid)
	if conv.LastMessage != "hello" || !conv.LastMsgTime.Equal(msg.Timestamp) {
		t.Errorf("preview = (%q, %v), want outgoing message", conv.LastMessage, conv.LastMsgTime)
	}
	if !reflect.DeepEqual(eff.Messages, []Message{msg}) || !reflect.DeepEqual(eff.Append, []Message{msg}) || len(eff.Conversations) != 1 {
		t.Errorf("effects = %+v, want persist+append+upsert", eff)
	}

	eff = c.AddOutgoing(Message{ID: "o2", ChatJID: "unknown@s.whatsapp.net"})
	if len(eff.Conversations) != 0 || len(eff.Append) != 1 {
		t.Errorf("unknown chat effects = %+v, want append without upsert", eff)
	}
}

// --- history ---

func TestAddHistoryMergesNotOverwrites(t *testing.T) {
	jid := "123@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})
	c.AddMessage(Message{ID: "live", ChatJID: jid, Timestamp: time.Unix(500, 0)}, "")

	hist := []Message{
		{ID: "hist1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "hist2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
		{ID: "live", ChatJID: jid, Timestamp: time.Unix(500, 0)},
	}
	eff := c.AddHistory(jid, hist, "")

	if got, want := msgIDs(c.Messages(jid)), []string{"hist1", "hist2", "live"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v (live msg must survive)", got, want)
	}
	if !reflect.DeepEqual(eff.Messages, hist) {
		t.Errorf("persisted = %v, want the loaded batch", msgIDs(eff.Messages))
	}
	if eff.InView {
		t.Errorf("InView = true, want false when chat not open")
	}
}

func TestAddHistoryPreviewUsesPreviewText(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff := c.AddHistory(jid, []Message{{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)}}, "")

	conv, _ := c.Conversation(jid)
	if conv.LastMessage != "[image]" {
		t.Errorf("LastMessage = %q, want [image]", conv.LastMessage)
	}
	if len(eff.Conversations) != 1 || eff.Conversations[0] != conv {
		t.Errorf("upserts = %+v, want %+v", eff.Conversations, conv)
	}
}

func TestAddHistoryAdvancesPreviewToNewest(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, LastMessage: "p", LastMsgTime: time.Unix(150, 0)})

	c.AddHistory(jid, []Message{
		{ID: "b", Content: "newest", Timestamp: time.Unix(300, 0)},
		{ID: "a", Content: "older", Timestamp: time.Unix(100, 0)},
	}, "")

	if conv, _ := c.Conversation(jid); conv.LastMessage != "newest" || !conv.LastMsgTime.Equal(time.Unix(300, 0)) {
		t.Errorf("preview = (%q, %v), want newest", conv.LastMessage, conv.LastMsgTime.Unix())
	}
}

func TestAddHistoryDoesNotRegressPreview(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, LastMessage: "live", LastMsgTime: time.Unix(500, 0)})

	eff := c.AddHistory(jid, []Message{{ID: "h", Content: "hist", Timestamp: time.Unix(100, 0)}}, jid)

	if conv, _ := c.Conversation(jid); conv.LastMessage != "live" {
		t.Errorf("LastMessage = %q, want live kept", conv.LastMessage)
	}
	if len(eff.Conversations) != 0 {
		t.Errorf("upserts = %+v, want none", eff.Conversations)
	}
	if !eff.InView {
		t.Errorf("InView = false, want true when chat is open")
	}
}

func TestAddHistoryResolvesAlias(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID})

	eff := c.AddHistory(lidJID, []Message{{ID: "h", ChatJID: lidJID, Timestamp: time.Unix(1, 0)}}, "")

	if eff.Chat != pnJID || len(c.Messages(pnJID)) != 1 {
		t.Errorf("history for LID must land under pn: chat=%q msgs=%v", eff.Chat, msgIDs(c.Messages(pnJID)))
	}
}

// --- opening a chat ---

func TestOpenUnknownChat(t *testing.T) {
	if _, ok := NewChats(nil).Open("x@s.whatsapp.net", nil); ok {
		t.Fatalf("Open(unknown) ok = true, want false")
	}
}

func TestOpenMergesStoreAndCache(t *testing.T) {
	jid := "123@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})
	// A live message present only in the in-memory cache.
	c.AddMessage(Message{ID: "c1", ChatJID: jid, Timestamp: time.Unix(300, 0)}, "")

	stored := []Message{
		{ID: "s1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "s2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
	}
	eff, ok := c.Open(jid, stored)

	if !ok || eff.Chat != jid {
		t.Fatalf("Open() = %+v, %v", eff, ok)
	}
	if got, want := msgIDs(c.Messages(jid)), []string{"s1", "s2", "c1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
}

func TestOpenMergesAliasCache(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID})
	c.msgs[lidJID] = []Message{{ID: "l1", Timestamp: time.Unix(1, 0)}}

	c.Open(pnJID, nil)

	if got := msgIDs(c.Messages(pnJID)); !reflect.DeepEqual(got, []string{"l1"}) {
		t.Errorf("Messages(pn) = %v, want [l1]", got)
	}
}

func TestOpenPreviewUsesPreviewText(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff, _ := c.Open(jid, []Message{{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)}})

	conv, _ := c.Conversation(jid)
	if conv.LastMessage != "[image]" {
		t.Errorf("LastMessage = %q, want [image]", conv.LastMessage)
	}
	if len(eff.Conversations) != 1 || eff.Conversations[0] != conv {
		t.Errorf("upserts = %+v, want %+v", eff.Conversations, conv)
	}
}

func TestOpenKeepsNewerPreview(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, LastMessage: "p", LastMsgTime: time.Unix(900, 0)})

	eff, _ := c.Open(jid, incoming(jid, 2))

	if conv, _ := c.Conversation(jid); conv.LastMessage != "p" {
		t.Errorf("LastMessage = %q, want p kept", conv.LastMessage)
	}
	if len(eff.Conversations) != 0 {
		t.Errorf("upserts = %+v, want none", eff.Conversations)
	}
}

func TestOpenClearsUnread(t *testing.T) {
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: a, UnreadCount: 5}, Conversation{JID: b})

	eff, _ := c.Open(a, nil)
	if conv, _ := c.Conversation(a); conv.UnreadCount != 0 {
		t.Fatalf("UnreadCount after open = %d, want 0", conv.UnreadCount)
	}
	if !reflect.DeepEqual(eff.ClearUnread, []string{a}) {
		t.Errorf("ClearUnread = %v, want [%s]", eff.ClearUnread, a)
	}

	c.Open(b, nil)
	c.AddMessage(Message{ID: "n1", ChatJID: a, Content: "hi", Timestamp: time.Unix(100, 0)}, b)
	if conv, _ := c.Conversation(a); conv.UnreadCount != 1 {
		t.Errorf("UnreadCount = %d, want 1 (stale count must not resurface)", conv.UnreadCount)
	}
}

func TestOpenNoUnreadSendsNoReceipts(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff, _ := c.Open(jid, incoming(jid, 5))

	if len(eff.Receipts) != 0 {
		t.Errorf("receipts = %+v, want none", eff.Receipts)
	}
}

func TestOpenMarksOnlyUnreadMessages(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, UnreadCount: 2})
	msgs := incoming(jid, 5)
	msgs = append(msgs[:4:4], Message{ID: "own", ChatJID: jid, IsFromMe: true, Timestamp: time.Unix(103, 500)}, msgs[4])

	eff, _ := c.Open(jid, msgs)

	want := []Receipt{{Chat: jid, Sender: jid, IDs: []string{"m4", "m5"}}}
	if !reflect.DeepEqual(eff.Receipts, want) {
		t.Fatalf("receipts = %+v, want %+v", eff.Receipts, want)
	}
}

func TestOpenGroupMarksUnreadPerSender(t *testing.T) {
	jid := "g@g.us"
	alice, bob := "alice@s.whatsapp.net", "bob@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid, IsGroup: true, UnreadCount: 3})
	msgs := incoming(jid, 5)
	for i, s := range []string{alice, bob, alice, bob, alice} {
		msgs[i].SenderJID = s
	}

	eff, _ := c.Open(jid, msgs)

	got := map[string][]string{}
	for _, r := range eff.Receipts {
		if r.Chat != jid {
			t.Errorf("receipt chat = %s, want %s", r.Chat, jid)
		}
		got[r.Sender] = append(got[r.Sender], r.IDs...)
	}
	if want := map[string][]string{alice: {"m3", "m5"}, bob: {"m4"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("receipts by sender = %v, want %v", got, want)
	}
}

func TestOpenAutoDownloadsNewestStickers(t *testing.T) {
	jid := "123@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff, _ := c.Open(jid, stickerMsgs(jid, 15))

	want := []string{"s14", "s13", "s12", "s11", "s10", "s9", "s8", "s7", "s6", "s5"}
	if got := msgIDs(eff.Downloads); !reflect.DeepEqual(got, want) {
		t.Fatalf("downloads = %v, want %v", got, want)
	}
}

// --- older messages ---

func TestPrependOlderEmptyMeansNoMore(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff := c.PrependOlder(jid, nil, jid)

	if !eff.NoOlder || len(eff.Prepend) != 0 {
		t.Errorf("effects = %+v, want NoOlder", eff)
	}
}

func TestPrependOlderDedupesAgainstCache(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})
	c.AddHistory(jid, []Message{
		{ID: "b", Timestamp: time.Unix(200, 0)},
		{ID: "c", Timestamp: time.Unix(300, 0)},
	}, "")

	eff := c.PrependOlder(jid, []Message{
		{ID: "a", Timestamp: time.Unix(100, 0)},
		{ID: "b", Timestamp: time.Unix(200, 0)},
	}, jid)

	if got, want := msgIDs(c.Messages(jid)), []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cache = %v, want %v", got, want)
	}
	if got := msgIDs(eff.Prepend); !reflect.DeepEqual(got, []string{"a"}) || !eff.InView || eff.NoOlder {
		t.Errorf("effects = %+v, want in-view prepend of [a]", eff)
	}
}

func TestPrependOlderAllDuplicatesMeansNoMore(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})
	c.AddHistory(jid, []Message{{ID: "a", Timestamp: time.Unix(100, 0)}}, "")

	eff := c.PrependOlder(jid, []Message{{ID: "a", Timestamp: time.Unix(100, 0)}}, jid)

	if !eff.NoOlder || len(eff.Prepend) != 0 {
		t.Errorf("effects = %+v, want NoOlder with nothing to prepend", eff)
	}
	if got := len(c.Messages(jid)); got != 1 {
		t.Errorf("cache len = %d, want 1", got)
	}
}

func TestPrependOlderBackgroundChat(t *testing.T) {
	jid := "a@s.whatsapp.net"
	c := newChats(nil, Conversation{JID: jid})

	eff := c.PrependOlder(jid, []Message{{ID: "a", Timestamp: time.Unix(100, 0)}}, "other@s.whatsapp.net")

	if eff.InView || len(eff.Prepend) != 0 {
		t.Errorf("effects = %+v, want no view change", eff)
	}
	if len(c.Messages(jid)) != 1 {
		t.Errorf("cache must still receive the older messages")
	}
}

// --- status / media ---

func TestSetStatus(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID})
	c.msgs[lidJID] = []Message{{ID: "m1", Status: "sending", Timestamp: time.Unix(1, 0)}}

	eff, ok := c.SetStatus(lidJID, "m1", "read", pnJID)

	if !ok || eff.Chat != pnJID || !eff.InView {
		t.Fatalf("SetStatus() = %+v, %v; want applied in view under pn", eff, ok)
	}
	if got := c.Messages(pnJID); len(got) != 1 || got[0].Status != "read" {
		t.Errorf("cache = %+v, want m1 read under pn", got)
	}

	eff, _ = c.SetStatus(pnJID, "missing", "read", "")
	if eff.InView {
		t.Errorf("InView = true, want false when not viewing")
	}
}

func TestSetStatusIgnoresEmptyID(t *testing.T) {
	if _, ok := NewChats(nil).SetStatus("a@s.whatsapp.net", "", "read", ""); ok {
		t.Errorf("SetStatus(empty id) ok = true, want false")
	}
}

func TestSetMediaPathAndFind(t *testing.T) {
	c := newChats(pnLIDAliases(), Conversation{JID: pnJID})
	c.AddMessage(Message{ID: "img", ChatJID: pnJID, MediaType: "image", Timestamp: time.Unix(1, 0)}, "")

	eff := c.SetMediaPath(lidJID, "img", "/cache/img.jpg", pnJID)

	if eff.Chat != pnJID || !eff.InView {
		t.Fatalf("effects = %+v, want in-view update under pn", eff)
	}
	msg, ok := c.Find(lidJID, "img")
	if !ok || msg.MediaPath != "/cache/img.jpg" {
		t.Fatalf("Find() = %+v, %v; want downloaded path", msg, ok)
	}
	if _, ok := c.Find(pnJID, "nope"); ok {
		t.Errorf("Find(unknown) ok = true, want false")
	}
	if eff := c.SetMediaPath(pnJID, "img", "/x", "other@s.whatsapp.net"); eff.InView {
		t.Errorf("InView = true, want false for other chat")
	}
}
