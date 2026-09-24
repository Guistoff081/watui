package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
)

// seedConv registers a conversation in both the store and the in-memory cache.
func seedConv(t *testing.T, m *Model, conv core.Conversation) {
	t.Helper()
	if err := m.store.UpsertConversation(context.Background(), conv); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	m.conversations[conv.JID] = conv
}

func TestSelectChatClearsCachedUnreadCount(t *testing.T) {
	m, _ := newTestModel(t)
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: a, UnreadCount: 5})
	seedConv(t, &m, core.Conversation{JID: b})

	m, _ = m.selectChat(a)
	if got := m.conversations[a].UnreadCount; got != 0 {
		t.Fatalf("UnreadCount after select = %d, want 0", got)
	}

	m, _ = m.selectChat(b)
	m, _ = m.handleNewMessage(core.Message{ID: "n1", ChatJID: a, Content: "hi", Timestamp: time.Unix(100, 0)})

	if got := m.conversations[a].UnreadCount; got != 1 {
		t.Errorf("UnreadCount = %d, want 1 (stale count must not resurface)", got)
	}
}

func TestHandleNewMessageOwnMessageNotUnread(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})

	m, _ = m.handleNewMessage(core.Message{ID: "me1", ChatJID: jid, Content: "sent from phone", IsFromMe: true, Timestamp: time.Unix(100, 0)})

	if got := m.conversations[jid].UnreadCount; got != 0 {
		t.Errorf("UnreadCount = %d, want 0 for own message", got)
	}
}

func TestHandleNewMessagePreviewUsesPreviewText(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})

	m, _ = m.handleNewMessage(core.Message{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)})

	if got := m.conversations[jid].LastMessage; got != "[image]" {
		t.Errorf("LastMessage = %q, want %q", got, "[image]")
	}
}

func TestMessagesLoadedPreviewUsesPreviewText(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})

	parsed, _ := types.ParseJID(jid)
	updated, _ := m.Update(core.MessagesLoaded{
		ChatJID:  parsed,
		Messages: []core.Message{{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)}},
	})
	m = updated.(Model)

	if got := m.conversations[jid].LastMessage; got != "[image]" {
		t.Errorf("LastMessage = %q, want %q", got, "[image]")
	}
}

func TestSelectChatPreviewUsesPreviewText(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m.chatMessages[jid] = []core.Message{{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)}}

	m, _ = m.selectChat(jid)

	if got := m.conversations[jid].LastMessage; got != "[image]" {
		t.Errorf("LastMessage = %q, want %q", got, "[image]")
	}
}

// incoming builds n incoming messages m1..mn for a chat, oldest first.
func incoming(jid string, n int) []core.Message {
	msgs := make([]core.Message, n)
	for i := range msgs {
		msgs[i] = core.Message{
			ID:        "m" + string(rune('1'+i)),
			ChatJID:   jid,
			Content:   "x",
			Timestamp: time.Unix(int64(100+i), 0),
		}
	}
	return msgs
}

func TestSelectChatNoUnreadSendsNoReceipts(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m.chatMessages[jid] = incoming(jid, 5)

	m, _ = m.selectChat(jid)

	if len(wa.markReads) != 0 {
		t.Errorf("MarkRead calls = %+v, want none", wa.markReads)
	}
}

func TestSelectChatMarksOnlyUnreadMessages(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid, UnreadCount: 2})
	msgs := incoming(jid, 5)
	// An own message in between must neither be marked nor consume the budget.
	msgs = append(msgs[:4], core.Message{ID: "own", ChatJID: jid, IsFromMe: true, Timestamp: time.Unix(103, 500)}, msgs[4])
	m.chatMessages[jid] = msgs

	m, _ = m.selectChat(jid)

	if got, want := wa.markedReadIDs(), []string{"m4", "m5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("marked IDs = %v, want %v", got, want)
	}
	if c := wa.markReads[0]; c.Chat != jid || c.Sender != jid {
		t.Errorf("MarkRead(chat=%s, sender=%s), want both %s", c.Chat, c.Sender, jid)
	}
}

func TestSelectChatGroupMarksUnreadPerSender(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "g@g.us"
	alice, bob := "alice@s.whatsapp.net", "bob@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid, IsGroup: true, UnreadCount: 3})
	msgs := incoming(jid, 5)
	for i, s := range []string{alice, bob, alice, bob, alice} {
		msgs[i].SenderJID = s
	}
	m.chatMessages[jid] = msgs

	m, _ = m.selectChat(jid)

	got := map[string][]string{}
	for _, c := range wa.markReads {
		if c.Chat != jid {
			t.Errorf("MarkRead chat = %s, want %s", c.Chat, jid)
		}
		got[c.Sender] = append(got[c.Sender], c.IDs...)
	}
	want := map[string][]string{alice: {"m3", "m5"}, bob: {"m4"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("receipts by sender = %v, want %v", got, want)
	}
}

func TestHandleNewMessageInOpenChatMarksRead(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m, _ = m.selectChat(jid)

	m, _ = m.handleNewMessage(core.Message{ID: "n1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)})

	if len(wa.markReads) != 1 {
		t.Fatalf("MarkRead calls = %+v, want 1", wa.markReads)
	}
	if c := wa.markReads[0]; c.Chat != jid || c.Sender != jid || !reflect.DeepEqual(c.IDs, []string{"n1"}) {
		t.Errorf("MarkRead = %+v, want chat=sender=%s ids=[n1]", c, jid)
	}
	if got := m.conversations[jid].UnreadCount; got != 0 {
		t.Errorf("UnreadCount = %d, want 0 for open chat", got)
	}
}

func TestHandleNewMessageInOpenGroupMarksReadWithSender(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "g@g.us"
	sender := "alice@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid, IsGroup: true})
	m, _ = m.selectChat(jid)

	m, _ = m.handleNewMessage(core.Message{ID: "n1", ChatJID: jid, SenderJID: sender, Content: "hi", Timestamp: time.Unix(100, 0)})

	if len(wa.markReads) != 1 {
		t.Fatalf("MarkRead calls = %+v, want 1", wa.markReads)
	}
	if c := wa.markReads[0]; c.Chat != jid || c.Sender != sender || !reflect.DeepEqual(c.IDs, []string{"n1"}) {
		t.Errorf("MarkRead = %+v, want chat=%s sender=%s ids=[n1]", c, jid, sender)
	}
}

func TestHandleNewMessageOwnInOpenChatNotMarked(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m, _ = m.selectChat(jid)

	m, _ = m.handleNewMessage(core.Message{ID: "me1", ChatJID: jid, IsFromMe: true, Content: "hi", Timestamp: time.Unix(100, 0)})

	if len(wa.markReads) != 0 {
		t.Errorf("MarkRead calls = %+v, want none for own message", wa.markReads)
	}
}

func TestHandleNewMessageInOtherChatNotMarked(t *testing.T) {
	m, _, wa := newRecordingModel(t)
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: a})
	seedConv(t, &m, core.Conversation{JID: b})
	m, _ = m.selectChat(b)

	m, _ = m.handleNewMessage(core.Message{ID: "n1", ChatJID: a, Content: "hi", Timestamp: time.Unix(100, 0)})

	if len(wa.markReads) != 0 {
		t.Errorf("MarkRead calls = %+v, want none for background chat", wa.markReads)
	}
}
