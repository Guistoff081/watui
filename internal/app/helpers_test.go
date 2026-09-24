package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/store"
)

// fakeWA is a no-op WAClient for exercising app logic without a real connection.
type fakeWA struct{}

func (fakeWA) Connect() tea.Cmd                                   { return nil }
func (fakeWA) Disconnect()                                        {}
func (fakeWA) GenerateMessageID() string                          { return "genid" }
func (fakeWA) SendTextMessage(types.JID, string, string) tea.Cmd  { return nil }
func (fakeWA) SendFileMessage(types.JID, string, string) tea.Cmd  { return nil }
func (fakeWA) SendAudioMessage(types.JID, string, string) tea.Cmd { return nil }
func (fakeWA) SendChatPresence(types.JID, bool)                   {}
func (fakeWA) MarkRead(types.JID, types.JID, []string)            {}
func (fakeWA) GetAllContactNames() map[string]string              { return nil }
func (fakeWA) GetGroupNames() map[string]string                   { return nil }
func (fakeWA) AltChatJID(jid string) string                       { return "" }
func (fakeWA) DownloadMedia(core.Message) tea.Cmd                 { return nil }
func (fakeWA) OpenMedia(string, string) tea.Cmd                   { return nil }

// markReadCall records one MarkRead invocation.
type markReadCall struct {
	Chat, Sender string
	IDs          []string
}

// openMediaCall records one OpenMedia invocation.
type openMediaCall struct {
	Path, MediaType string
}

// recordingWA is a WAClient that records the calls app logic makes, so tests
// can assert on side effects (receipts, downloads, opens). DownloadMedia and
// OpenMedia record at call time and return a non-nil no-op cmd.
type recordingWA struct {
	fakeWA

	mu        sync.Mutex
	alts      map[string]string
	markReads []markReadCall
	downloads []core.Message
	opens     []openMediaCall
}

func (r *recordingWA) AltChatJID(jid string) string {
	if r.alts == nil {
		return ""
	}
	return r.alts[jid]
}

func (r *recordingWA) MarkRead(chat, sender types.JID, ids []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markReads = append(r.markReads, markReadCall{
		Chat:   chat.String(),
		Sender: sender.String(),
		IDs:    append([]string(nil), ids...),
	})
}

func (r *recordingWA) DownloadMedia(msg core.Message) tea.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.downloads = append(r.downloads, msg)
	return func() tea.Msg { return nil }
}

func (r *recordingWA) OpenMedia(path, mediaType string) tea.Cmd {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.opens = append(r.opens, openMediaCall{Path: path, MediaType: mediaType})
	return func() tea.Msg { return nil }
}

// markedReadIDs flattens all message IDs passed to MarkRead.
func (r *recordingWA) markedReadIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, c := range r.markReads {
		out = append(out, c.IDs...)
	}
	return out
}

// newTestStore opens a fresh SQLite store in a temp dir, closed on cleanup.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newTestModel(t *testing.T) (Model, *store.Store) {
	t.Helper()
	s := newTestStore(t)
	return testModel(fakeWA{}, s), s
}

// testModel builds a Model whose timers yield delayedMsg immediately instead
// of sleeping, so run() can settle a model without waiting on them.
func testModel(wa WAClient, s Store) Model {
	m := NewModel(wa, s, "test", nil)
	m.delay = func(d time.Duration, msg tea.Msg) tea.Cmd {
		return func() tea.Msg { return delayedMsg{d: d, msg: msg} }
	}
	return m
}

// delayedMsg stands in for a timer's message in tests; run() never delivers it.
type delayedMsg struct {
	d   time.Duration
	msg tea.Msg
}

// collect executes cmd (expanding batches) and returns the messages it
// yields, without feeding them back to a model.
func collect(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			out = append(out, msg)
		}
	}
	return out
}

// run executes cmd and every command it leads to, feeding each message back
// through Update, and returns the settled model. Timers (delayedMsg) are not
// delivered.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	pending := []tea.Cmd{cmd}
	for i := 0; len(pending) > 0; i++ {
		if i > 1000 {
			t.Fatal("run: commands did not settle")
		}
		var next []tea.Cmd
		for _, msg := range collect(t, tea.Batch(pending...)) {
			if _, ok := msg.(delayedMsg); ok {
				continue
			}
			var c tea.Cmd
			m, c = update(t, m, msg)
			next = append(next, c)
		}
		pending = next
	}
	return m
}

// send runs msg through Update and settles the commands it returns.
func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	m, cmd := update(t, m, msg)
	return run(t, m, cmd)
}

// open selects jid and settles the resulting load.
func open(t *testing.T, m Model, jid string) Model {
	t.Helper()
	m, cmd := m.selectChat(jid)
	return run(t, m, cmd)
}

// fakeStore is an in-memory Store that records calls in order and can be told
// to fail specific operations.
type fakeStore struct {
	mu    sync.Mutex
	calls []string
	errs  map[string]error // keyed by operation name, e.g. "InsertMessages"

	convs    []core.Conversation
	messages []core.Message
}

func (f *fakeStore) record(op, arg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+" "+arg)
	return f.errs[op]
}

func (f *fakeStore) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeStore) GetAllConversations(context.Context) ([]core.Conversation, error) {
	if err := f.record("GetAllConversations", ""); err != nil {
		return nil, err
	}
	return f.convs, nil
}

func (f *fakeStore) UpsertConversation(_ context.Context, c core.Conversation) error {
	return f.record("UpsertConversation", c.JID)
}

func (f *fakeStore) ClearUnread(_ context.Context, jid string) error {
	return f.record("ClearUnread", jid)
}

func (f *fakeStore) InsertMessages(_ context.Context, msgs []core.Message) error {
	return f.record("InsertMessages", strings.Join(msgIDs(msgs), ","))
}

func (f *fakeStore) GetMessagesForChats(_ context.Context, jids []string, limit int) ([]core.Message, error) {
	if err := f.record("GetMessagesForChats", strings.Join(jids, ",")); err != nil {
		return nil, err
	}
	return f.messages, nil
}

func (f *fakeStore) GetMessagesBefore(_ context.Context, jid string, before time.Time, limit int) ([]core.Message, error) {
	if err := f.record("GetMessagesBefore", jid); err != nil {
		return nil, err
	}
	return nil, nil
}

func (f *fakeStore) UpdateMessageStatus(_ context.Context, id, status string) error {
	return f.record("UpdateMessageStatus", fmt.Sprintf("%s=%s", id, status))
}

func (f *fakeStore) UpdateMessageMediaPath(_ context.Context, jid, id, path string) error {
	return f.record("UpdateMessageMediaPath", id)
}

// newFakeStoreModel returns a model backed by a fakeStore and a recordingWA.
func newFakeStoreModel(t *testing.T) (Model, *fakeStore, *recordingWA) {
	t.Helper()
	fs := &fakeStore{}
	wa := &recordingWA{}
	return testModel(wa, fs), fs, wa
}

// newRecordingModel returns a model wired to a recordingWA so tests can inspect
// the calls it receives. alts optionally seeds LID↔PN aliases.
func newRecordingModel(t *testing.T, alts ...map[string]string) (Model, *store.Store, *recordingWA) {
	t.Helper()
	s := newTestStore(t)
	wa := &recordingWA{}
	if len(alts) > 0 {
		wa.alts = alts[0]
	}
	return testModel(wa, s), s, wa
}

// seedConv registers a conversation in both the store and the chat engine.
func seedConv(t *testing.T, m *Model, conv core.Conversation) {
	t.Helper()
	if err := m.store.UpsertConversation(context.Background(), conv); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	m.chats.Load([]core.Conversation{conv})
}

// seedStored persists messages so that selectChat loads them from the store.
func seedStored(t *testing.T, m *Model, msgs []core.Message) {
	t.Helper()
	if err := m.store.InsertMessages(context.Background(), msgs); err != nil {
		t.Fatalf("InsertMessages() error = %v", err)
	}
}

// conv returns the engine's current state for a conversation.
func conv(m Model, jid string) core.Conversation {
	c, _ := m.chats.Conversation(jid)
	return c
}

// update runs one message through Update and returns the resulting model.
func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	return updated.(Model), cmd
}

func msgIDs(msgs []core.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}
