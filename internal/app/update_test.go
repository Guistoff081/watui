package app

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
)

func msgIDs(msgs []core.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestHandleNewMessageDeduplicates(t *testing.T) {
	m, s := newTestModel(t)
	jid := "123@s.whatsapp.net"
	_ = s.UpsertConversation(context.Background(), core.Conversation{JID: jid})
	m.conversations[jid] = core.Conversation{JID: jid}

	msg := core.Message{ID: "m1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)}
	m, _ = m.handleNewMessage(msg)
	m, _ = m.handleNewMessage(msg) // duplicate dispatch (e.g. group pkmsg+skmsg)

	if got := m.chatMessages[jid]; len(got) != 1 {
		t.Fatalf("messages = %v, want exactly 1 (deduped)", msgIDs(got))
	}
}

func TestHandleNewMessageOrdersOfflineReplay(t *testing.T) {
	m, s := newTestModel(t)
	jid := "g@g.us"
	_ = s.UpsertConversation(context.Background(), core.Conversation{JID: jid})
	m.conversations[jid] = core.Conversation{JID: jid}

	// Newest arrives first, then an older (offline-replayed) message.
	m, _ = m.handleNewMessage(core.Message{ID: "new", ChatJID: jid, Content: "newest", Timestamp: time.Unix(300, 0)})
	m, _ = m.handleNewMessage(core.Message{ID: "old", ChatJID: jid, Content: "older", Timestamp: time.Unix(100, 0)})

	got := m.chatMessages[jid]
	if len(got) != 2 || got[0].ID != "old" || got[1].ID != "new" {
		t.Fatalf("order = %v, want [old new]", msgIDs(got))
	}

	// The preview must reflect the newer message, not the offline-replayed one.
	conv := m.conversations[jid]
	if !conv.LastMsgTime.Equal(time.Unix(300, 0)) || conv.LastMessage != "newest" {
		t.Errorf("preview = (%q, %v), want (newest, 300)", conv.LastMessage, conv.LastMsgTime.Unix())
	}
}

func TestSelectChatMergesStoreAndCache(t *testing.T) {
	m, s := newTestModel(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"
	_ = s.UpsertConversation(ctx, core.Conversation{JID: jid})
	_ = s.InsertMessages(ctx, []core.Message{
		{ID: "s1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "s2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
	})

	m.conversations[jid] = core.Conversation{JID: jid}
	// A live message present only in the in-memory cache.
	m.chatMessages[jid] = []core.Message{{ID: "c1", ChatJID: jid, Timestamp: time.Unix(300, 0)}}

	m, _ = m.selectChat(jid)

	got := m.chatMessages[jid]
	want := []string{"s1", "s2", "c1"}
	if len(got) != len(want) {
		t.Fatalf("merged = %v, want %v", msgIDs(got), want)
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("merged[%d] = %s, want %s", i, got[i].ID, w)
		}
	}
}

func TestMessagesLoadedMergesNotOverwrites(t *testing.T) {
	m, s := newTestModel(t)
	jid := "123@s.whatsapp.net"
	_ = s.UpsertConversation(context.Background(), core.Conversation{JID: jid})
	m.conversations[jid] = core.Conversation{JID: jid}

	// A live message already cached.
	m.chatMessages[jid] = []core.Message{{ID: "live", ChatJID: jid, Timestamp: time.Unix(500, 0)}}

	parsed, _ := types.ParseJID(jid)
	updated, _ := m.Update(core.MessagesLoaded{
		ChatJID: parsed,
		Messages: []core.Message{
			{ID: "hist1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
			{ID: "hist2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
		},
	})
	m = updated.(Model)

	got := m.chatMessages[jid]
	want := []string{"hist1", "hist2", "live"}
	if len(got) != len(want) {
		t.Fatalf("merged = %v, want %v (live msg must survive)", msgIDs(got), want)
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("merged[%d] = %s, want %s", i, got[i].ID, w)
		}
	}
}
