package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/ui/input"
)

func TestHandleNewMessageDeduplicates(t *testing.T) {
	m, s := newTestModel(t)
	jid := "123@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})

	msg := core.Message{ID: "m1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)}
	m = send(t, m, core.NewMessage{Message: msg})
	m = send(t, m, core.NewMessage{Message: msg}) // duplicate dispatch (e.g. group pkmsg+skmsg)

	if got := m.chats.Messages(jid); len(got) != 1 {
		t.Fatalf("messages = %v, want exactly 1 (deduped)", msgIDs(got))
	}
	stored, _ := s.GetAllConversations(context.Background())
	if len(stored) != 1 || stored[0].UnreadCount != 1 {
		t.Fatalf("stored conversations = %+v, want unread 1 persisted", stored)
	}
}

func TestHandleNewMessageCreatesConversation(t *testing.T) {
	m, s := newTestModel(t)

	m = send(t, m, core.NewMessage{Message: core.Message{ID: "m1", ChatJID: "g@g.us", SenderName: "Alice", Content: "hi", Timestamp: time.Unix(100, 0)}})

	stored, _ := s.GetAllConversations(context.Background())
	// Group subjects arrive via group names; a member's push name is not one.
	if len(stored) != 1 || stored[0].JID != "g@g.us" || !stored[0].IsGroup || stored[0].Name == "Alice" {
		t.Fatalf("stored conversations = %+v, want new group g@g.us not named after its sender", stored)
	}
}

func TestSelectChatMergesStoreAndCache(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "123@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	seedStored(t, &m, []core.Message{
		{ID: "s1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "s2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
	})
	// A live message present only in the in-memory cache.
	m.chats.AddHistory(jid, []core.Message{{ID: "c1", ChatJID: jid, Timestamp: time.Unix(300, 0)}}, "")

	m = open(t, m, jid)

	if got, want := msgIDs(m.chats.Messages(jid)), []string{"s1", "s2", "c1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v", got, want)
	}
	if m.chatView.ChatJID() != jid {
		t.Errorf("chat view shows %q, want %q", m.chatView.ChatJID(), jid)
	}
}

func TestSelectChatUnknownIsNoop(t *testing.T) {
	m, _ := newTestModel(t)
	m, cmd := m.selectChat("nope@s.whatsapp.net")
	if cmd != nil || m.chatView.ChatJID() != "" {
		t.Fatalf("selectChat(unknown) changed state: view=%q cmd=%v", m.chatView.ChatJID(), cmd != nil)
	}
}

func TestMessagesLoadedMergesNotOverwrites(t *testing.T) {
	m, s := newTestModel(t)
	jid := "123@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m = open(t, m, jid)
	// A live message already cached.
	m = send(t, m, core.NewMessage{Message: core.Message{ID: "live", ChatJID: jid, Timestamp: time.Unix(500, 0)}})

	parsed, _ := types.ParseJID(jid)
	m = send(t, m, core.MessagesLoaded{
		ChatJID: parsed,
		Messages: []core.Message{
			{ID: "hist1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
			{ID: "hist2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
		},
	})

	want := []string{"hist1", "hist2", "live"}
	if got := msgIDs(m.chats.Messages(jid)); !reflect.DeepEqual(got, want) {
		t.Fatalf("merged = %v, want %v (live msg must survive)", got, want)
	}
	stored, _ := s.GetMessagesForChats(context.Background(), []string{jid}, 10)
	if got := msgIDs(stored); len(got) != 3 {
		t.Errorf("stored = %v, want history persisted alongside live", got)
	}
}

func TestMessagesLoadedPreviewUsesPreviewText(t *testing.T) {
	m, s := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})

	parsed, _ := types.ParseJID(jid)
	m = send(t, m, core.MessagesLoaded{
		ChatJID:  parsed,
		Messages: []core.Message{{ID: "img", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(100, 0)}},
	})

	if got := conv(m, jid).LastMessage; got != "[image]" {
		t.Errorf("LastMessage = %q, want [image]", got)
	}
	stored, _ := s.GetAllConversations(context.Background())
	if len(stored) != 1 || stored[0].LastMessage != "[image]" {
		t.Errorf("stored = %+v, want preview persisted", stored)
	}
}

func TestConversationUpdatedDoesNotRegressPreview(t *testing.T) {
	m, s := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid, LastMessage: "live", LastMsgTime: time.Unix(500, 0)})

	m = send(t, m, core.ConversationUpdated{Conversation: core.Conversation{
		JID: jid, Name: "A", LastMessage: "hist", LastMsgTime: time.Unix(100, 0),
	}})

	stored, _ := s.GetAllConversations(context.Background())
	if len(stored) != 1 || stored[0].Name != "A" || stored[0].LastMessage != "live" {
		t.Fatalf("stored = %+v, want name A with live preview kept", stored)
	}
}

func TestConversationsLoadedAndContactNames(t *testing.T) {
	m, s := newTestModel(t)
	jid := "a@s.whatsapp.net"
	m = send(t, m, conversationsLoadedMsg{Conversations: []core.Conversation{{JID: jid, Name: jid}}})
	if _, ok := m.chats.Conversation(jid); !ok {
		t.Fatalf("loaded conversation missing from engine")
	}

	m = send(t, m, contactNamesMsg{Names: map[string]string{jid: "Alice"}})

	if got := conv(m, jid).Name; got != "Alice" {
		t.Errorf("Name = %q, want Alice", got)
	}
	stored, _ := s.GetAllConversations(context.Background())
	if len(stored) != 1 || stored[0].Name != "Alice" {
		t.Errorf("stored = %+v, want name persisted", stored)
	}
}

func TestMessageStatusUpdatesCacheAndStore(t *testing.T) {
	m, s := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m = open(t, m, jid)
	m = send(t, m, input.SendMsg{Text: "hello"})

	parsed, _ := types.ParseJID(jid)
	m = send(t, m, core.MessageStatus{ChatJID: parsed, MessageID: "genid", Status: "read"})

	if got := m.chats.Messages(jid); len(got) != 1 || got[0].Status != "read" || !got[0].IsFromMe {
		t.Fatalf("cache = %+v, want own message genid read", got)
	}
	stored, _ := s.GetMessagesForChats(context.Background(), []string{jid}, 10)
	if len(stored) != 1 || stored[0].Status != "read" {
		t.Errorf("stored = %+v, want status persisted", stored)
	}
	if got := conv(m, jid).LastMessage; got != "hello" {
		t.Errorf("LastMessage = %q, want hello", got)
	}
}

func TestOlderMessagesDedupedAndPrepended(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	seedStored(t, &m, []core.Message{{ID: "b", ChatJID: jid, Timestamp: time.Unix(200, 0)}})
	m = open(t, m, jid)

	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid, Messages: []core.Message{
		{ID: "a", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "b", ChatJID: jid, Timestamp: time.Unix(200, 0)},
	}})

	if got, want := msgIDs(m.chats.Messages(jid)), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cache = %v, want %v (duplicate b dropped)", got, want)
	}
	if cmd := m.loadOlderMessagesCmd(jid); cmd == nil {
		t.Fatalf("loadOlderMessagesCmd() = nil, want a store query")
	}
	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid})
	if got := len(m.chats.Messages(jid)); got != 2 {
		t.Errorf("cache len = %d after empty page, want 2", got)
	}
}

func TestContactNameChangedNamesUnknownChat(t *testing.T) {
	m, _ := newTestModel(t)
	jid := "5511999999999@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid}, {JID: "k@s.whatsapp.net", Name: "Agenda"}})

	m = send(t, m, core.ContactNameChanged{JID: jid, Name: "Loja"})
	m = send(t, m, core.ContactNameChanged{JID: "k@s.whatsapp.net", Name: "Outro"})

	if conv, _ := m.chats.Conversation(jid); conv.Name != "Loja" {
		t.Errorf("unknown chat name = %q, want Loja", conv.Name)
	}
	if conv, _ := m.chats.Conversation("k@s.whatsapp.net"); conv.Name != "Agenda" {
		t.Errorf("known chat name = %q, push name must not override", conv.Name)
	}
}
