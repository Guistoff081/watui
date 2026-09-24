package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/core"
)

var _ Store = (*fakeStore)(nil)

func TestUpdatePerformsNoSynchronousIO(t *testing.T) {
	m, fs, wa := newFakeStoreModel(t)
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid}})
	m.chatView.SetChat(jid, false, nil)

	m, cmd := update(t, m, core.NewMessage{Message: core.Message{ID: "n1", ChatJID: jid, Timestamp: time.Unix(100, 0)}})

	if calls := fs.Calls(); len(calls) != 0 {
		t.Fatalf("store calls during Update = %v, want none", calls)
	}
	if len(wa.markReads) != 0 {
		t.Fatalf("MarkRead during Update = %+v, want none", wa.markReads)
	}
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want persistence and receipt commands")
	}
	// In-memory state is already updated.
	if got := msgIDs(m.chats.Messages(jid)); !reflect.DeepEqual(got, []string{"n1"}) {
		t.Fatalf("cache = %v, want n1", got)
	}

	run(t, m, cmd)

	want := []string{"UpsertConversation " + jid, "InsertMessages n1"}
	if got := fs.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("store calls = %v, want %v", got, want)
	}
	if got := wa.markedReadIDs(); !reflect.DeepEqual(got, []string{"n1"}) {
		t.Errorf("marked read = %v, want [n1]", got)
	}
}

func TestPersistenceKeepsUpdateOrderAcrossCommands(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	jid := "a@s.whatsapp.net"

	m, first := update(t, m, core.NewMessage{Message: core.Message{ID: "n1", ChatJID: jid, Timestamp: time.Unix(100, 0)}})
	m, second := update(t, m, core.NewMessage{Message: core.Message{ID: "n2", ChatJID: jid, Timestamp: time.Unix(200, 0)}})

	// Bubble Tea runs commands concurrently; the later one may run first.
	collect(t, second)
	collect(t, first)

	want := []string{
		"UpsertConversation " + jid, "InsertMessages n1",
		"UpsertConversation " + jid, "InsertMessages n2",
	}
	if got := fs.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("store calls = %v, want %v", got, want)
	}
}

// A message in a chat the store has never seen must land after its
// conversation row (messages.chat_jid is a foreign key).
func TestNewChatMessagePersistedWithForeignKey(t *testing.T) {
	m, s := newTestModel(t)
	jid := "new@s.whatsapp.net"

	send(t, m, core.NewMessage{Message: core.Message{ID: "n1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(100, 0)}})

	stored, err := s.GetMessagesForChats(context.Background(), []string{jid}, 10)
	if err != nil || !reflect.DeepEqual(msgIDs(stored), []string{"n1"}) {
		t.Fatalf("stored = %v, %v; want n1", msgIDs(stored), err)
	}
}

func TestPersistErrorIsReportedNotFatal(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	fs.errs = map[string]error{"InsertMessages": errors.New("disk full")}
	m.statusBar.SetWidth(200)
	m = send(t, m, core.Connected{})

	m, cmd := update(t, m, core.NewMessage{Message: core.Message{ID: "n1", ChatJID: "a@s.whatsapp.net", Timestamp: time.Unix(100, 0)}})
	msgs := collect(t, cmd)
	var perr persistErrMsg
	for _, msg := range msgs {
		if e, ok := msg.(persistErrMsg); ok {
			perr = e
		}
	}
	if perr.Err == nil || !strings.Contains(perr.Err.Error(), "disk full") {
		t.Fatalf("messages = %#v, want persistErrMsg carrying the store error", msgs)
	}

	m, cmd = update(t, m, perr)
	if m.state != StateChat {
		t.Fatalf("state = %v, want StateChat (persistence errors are not fatal)", m.state)
	}
	if view := m.statusBar.View(); !strings.Contains(view, "disk full") {
		t.Errorf("status bar = %q, want the store error", view)
	}
	if cmd == nil {
		t.Error("cmd = nil, want clear-status timer")
	}
}

func TestLoadConversationsErrorIsReported(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	fs.errs = map[string]error{"GetAllConversations": errors.New("locked")}
	m.statusBar.SetWidth(200)

	m = send(t, m, core.Connected{})

	if m.state != StateChat || !strings.Contains(m.statusBar.View(), "locked") {
		t.Errorf("state=%v status=%q, want StateChat with load error", m.state, m.statusBar.View())
	}
}

func TestSelectChatLoadsHistoryAsynchronously(t *testing.T) {
	m, fs, wa := newFakeStoreModel(t)
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid, Name: "Alice", UnreadCount: 1}})
	fs.messages = incoming(jid, 2)
	m.titleBar.SetWidth(80)

	m, cmd := m.selectChat(jid)

	if calls := fs.Calls(); len(calls) != 0 {
		t.Fatalf("store calls during selectChat = %v, want none", calls)
	}
	// Focus, title and target chat switch at once.
	if m.focus != PanelMessages || m.chatView.ChatJID() != jid || !strings.Contains(m.titleBar.View(), "Alice") {
		t.Fatalf("focus=%v view=%q, want messages panel on %s", m.focus, m.chatView.ChatJID(), jid)
	}

	m = run(t, m, cmd)

	if got := msgIDs(m.chats.Messages(jid)); !reflect.DeepEqual(got, []string{"m1", "m2"}) {
		t.Fatalf("cache = %v, want stored history merged", got)
	}
	if got := conv(m, jid).UnreadCount; got != 0 {
		t.Errorf("UnreadCount = %d, want cleared once loaded", got)
	}
	if got := wa.markedReadIDs(); !reflect.DeepEqual(got, []string{"m2"}) {
		t.Errorf("marked read = %v, want [m2]", got)
	}
	want := []string{"GetMessagesForChats " + jid, "UpsertConversation " + jid, "ClearUnread " + jid}
	if got := fs.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("store calls = %v, want %v", got, want)
	}
}

func TestSelectChatIgnoresStaleLoad(t *testing.T) {
	m, fs, wa := newFakeStoreModel(t)
	a, b := "a@s.whatsapp.net", "b@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: a, UnreadCount: 1}, {JID: b}})
	fs.messages = incoming(a, 1)

	m, loadA := m.selectChat(a)
	stale := collect(t, loadA)
	m, loadB := m.selectChat(b)

	// A's history arrives after the user already moved to B.
	for _, msg := range stale {
		m, _ = update(t, m, msg)
	}

	if m.chatView.ChatJID() != b {
		t.Fatalf("view = %q, want %q (stale load must not switch back)", m.chatView.ChatJID(), b)
	}
	if got := conv(m, a).UnreadCount; got != 1 {
		t.Errorf("A UnreadCount = %d, want 1 (A was never really opened)", got)
	}
	if len(wa.markReads) != 0 {
		t.Errorf("MarkRead = %+v, want none for the abandoned chat", wa.markReads)
	}

	fs.messages = nil
	m = run(t, m, loadB)
	if got := m.chats.Messages(a); len(got) != 0 {
		t.Errorf("A cache = %v, want untouched", msgIDs(got))
	}
}

func TestSelectChatLoadErrorStillOpensCache(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	fs.errs = map[string]error{"GetMessagesForChats": errors.New("io")}
	m.statusBar.SetWidth(200)
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid, UnreadCount: 3}})

	m = open(t, m, jid)

	if got := conv(m, jid).UnreadCount; got != 0 {
		t.Errorf("UnreadCount = %d, want cleared", got)
	}
	if !strings.Contains(m.statusBar.View(), "io") {
		t.Errorf("status bar = %q, want load error", m.statusBar.View())
	}
}

func TestStrayErrorDoesNotEnterErrorState(t *testing.T) {
	m, _, _ := newFakeStoreModel(t)
	m.statusBar.SetWidth(200)
	m = send(t, m, core.Connected{})

	m, _ = update(t, m, errors.New("something odd"))

	if m.state != StateChat {
		t.Fatalf("state = %v, want StateChat", m.state)
	}
	if !strings.Contains(m.statusBar.View(), "something odd") {
		t.Errorf("status bar = %q, want the error", m.statusBar.View())
	}
}

func TestConnectFailedEntersErrorState(t *testing.T) {
	m, _, _ := newFakeStoreModel(t)

	m, _ = update(t, m, connectFailedMsg{Err: errors.New("connect: dial failed")})

	if m.state != StateError || !strings.Contains(m.View(), "dial failed") {
		t.Errorf("state=%v view=%q, want connect error screen", m.state, m.View())
	}
}

func TestStatusAndMediaPathPersistedByCommand(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid}})
	m.chats.AddHistory(jid, []core.Message{{ID: "x", ChatJID: jid, MediaType: "image", Timestamp: time.Unix(1, 0)}}, "")

	statusCmd := m.setMessageStatus(jid, "x", "read")
	mediaCmd := m.handleMediaDownloaded(core.MediaDownloaded{ChatJID: jid, MessageID: "x", Path: "/p"})
	if calls := fs.Calls(); len(calls) != 0 {
		t.Fatalf("store calls before commands ran = %v, want none", calls)
	}
	collect(t, statusCmd)
	collect(t, mediaCmd)

	want := []string{"UpdateMessageStatus x=read", "UpdateMessageMediaPath x"}
	if got := fs.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("store calls = %v, want %v", got, want)
	}
}

func TestOlderMessagesLoadErrorIsReported(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	fs.errs = map[string]error{"GetMessagesBefore": errors.New("gone")}
	m.statusBar.SetWidth(200)
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid}})
	m.chats.AddHistory(jid, []core.Message{{ID: "x", ChatJID: jid, Timestamp: time.Unix(1, 0)}}, "")

	m = run(t, m, m.loadOlderMessagesCmd(jid))

	if !strings.Contains(m.statusBar.View(), "gone") {
		t.Errorf("status bar = %q, want load error", m.statusBar.View())
	}
}

// Quitting must not drop writes still in the queue: tea.Quit ends the program
// before pending flush commands run, and main closes the store right after.
func TestQuitFlushesPendingWrites(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	m.writes.enqueue(storeOp{"save conversation", func(ctx context.Context, s Store) error {
		return s.UpsertConversation(ctx, core.Conversation{JID: "q@s.whatsapp.net"})
	}})

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}) // flush cmd deliberately never run

	if got := fs.Calls(); len(got) != 1 || got[0] != "UpsertConversation q@s.whatsapp.net" {
		t.Fatalf("store calls on quit = %v, want the pending upsert", got)
	}
}
