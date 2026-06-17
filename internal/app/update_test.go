package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/store"
	"github.com/watui/watui/internal/theme"
)

// fakeWA is a no-op WAClient for exercising app logic without a real connection.
type fakeWA struct{}

func (fakeWA) Connect() tea.Cmd                                  { return nil }
func (fakeWA) Disconnect()                                       {}
func (fakeWA) GenerateMessageID() string                         { return "genid" }
func (fakeWA) SendTextMessage(types.JID, string, string) tea.Cmd { return nil }
func (fakeWA) SendFileMessage(types.JID, string, string) tea.Cmd { return nil }
func (fakeWA) SendAudioMessage(types.JID, string, string) tea.Cmd { return nil }
func (fakeWA) SendChatPresence(types.JID, bool)                  {}
func (fakeWA) MarkRead(types.JID, types.JID, []string)           {}
func (fakeWA) GetAllContactNames() map[string]string            { return nil }
func (fakeWA) GetGroupNames() map[string]string                 { return nil }
func (fakeWA) AltChatJID(jid string) string                     { return "" }

func newTestModel(t *testing.T) (Model, *store.Store) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return NewModel(fakeWA{}, s, "test", nil), s
}

func msgIDs(msgs []theme.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestHandleNewMessageDeduplicates(t *testing.T) {
	m, s := newTestModel(t)
	jid := "123@s.whatsapp.net"
	_ = s.UpsertConversation(context.Background(), theme.Conversation{JID: jid})
	m.conversations[jid] = theme.Conversation{JID: jid}

	msg := theme.Message{ID: "m1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)}
	m, _ = m.handleNewMessage(msg)
	m, _ = m.handleNewMessage(msg) // duplicate dispatch (e.g. group pkmsg+skmsg)

	if got := m.chatMessages[jid]; len(got) != 1 {
		t.Fatalf("messages = %v, want exactly 1 (deduped)", msgIDs(got))
	}
}

func TestHandleNewMessageOrdersOfflineReplay(t *testing.T) {
	m, s := newTestModel(t)
	jid := "g@g.us"
	_ = s.UpsertConversation(context.Background(), theme.Conversation{JID: jid})
	m.conversations[jid] = theme.Conversation{JID: jid}

	// Newest arrives first, then an older (offline-replayed) message.
	m, _ = m.handleNewMessage(theme.Message{ID: "new", ChatJID: jid, Content: "newest", Timestamp: time.Unix(300, 0)})
	m, _ = m.handleNewMessage(theme.Message{ID: "old", ChatJID: jid, Content: "older", Timestamp: time.Unix(100, 0)})

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
	_ = s.UpsertConversation(ctx, theme.Conversation{JID: jid})
	_ = s.InsertMessages(ctx, []theme.Message{
		{ID: "s1", ChatJID: jid, Timestamp: time.Unix(100, 0)},
		{ID: "s2", ChatJID: jid, Timestamp: time.Unix(200, 0)},
	})

	m.conversations[jid] = theme.Conversation{JID: jid}
	// A live message present only in the in-memory cache.
	m.chatMessages[jid] = []theme.Message{{ID: "c1", ChatJID: jid, Timestamp: time.Unix(300, 0)}}

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
	_ = s.UpsertConversation(context.Background(), theme.Conversation{JID: jid})
	m.conversations[jid] = theme.Conversation{JID: jid}

	// A live message already cached.
	m.chatMessages[jid] = []theme.Message{{ID: "live", ChatJID: jid, Timestamp: time.Unix(500, 0)}}

	parsed, _ := types.ParseJID(jid)
	updated, _ := m.Update(theme.MessagesLoadedMsg{
		ChatJID: parsed,
		Messages: []theme.Message{
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
