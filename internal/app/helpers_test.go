package app

import (
	"path/filepath"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/store"
	"github.com/watui/watui/internal/theme"
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
func (fakeWA) DownloadMedia(theme.Message) tea.Cmd                { return nil }
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
	downloads []theme.Message
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

func (r *recordingWA) DownloadMedia(msg theme.Message) tea.Cmd {
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
	return NewModel(fakeWA{}, s, "test", nil), s
}

// newRecordingModel returns a model wired to a recordingWA so tests can inspect
// the calls it receives.
func newRecordingModel(t *testing.T) (Model, *store.Store, *recordingWA) {
	t.Helper()
	s := newTestStore(t)
	wa := &recordingWA{}
	return NewModel(wa, s, "test", nil), s, wa
}
