// Package app is the root Bubble Tea model. It routes messages to the UI
// panels, delegates conversation state to core.Chats and turns the returned
// effects into view updates plus background commands (store writes, read
// receipts, WhatsApp calls). Update itself never blocks on I/O.
package app

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/debug"
	"github.com/watui/watui/internal/store"
	"github.com/watui/watui/internal/ui/auth"
	"github.com/watui/watui/internal/ui/chatlist"
	"github.com/watui/watui/internal/ui/chatview"
	"github.com/watui/watui/internal/ui/input"
	"github.com/watui/watui/internal/ui/statusbar"
	"github.com/watui/watui/internal/ui/titlebar"
)

type State int

const (
	StateAuth State = iota
	StateChat
	StateError
)

type Panel int

const (
	PanelChatList Panel = iota
	PanelMessages
	PanelInput
)

type WAClient interface {
	Connect() tea.Cmd
	Disconnect()
	GenerateMessageID() string
	SendTextMessage(jid types.JID, id, text string) tea.Cmd
	SendFileMessage(jid types.JID, id, path string) tea.Cmd
	SendAudioMessage(jid types.JID, id, path string) tea.Cmd
	SendChatPresence(jid types.JID, composing bool)
	MarkRead(chatJID types.JID, sender types.JID, messageIDs []string)
	GetAllContactNames() map[string]string
	GetGroupNames() map[string]string
	AltChatJID(jid string) string
	DownloadMedia(msg core.Message) tea.Cmd
	OpenMedia(path, mediaType string) tea.Cmd
	// RequestOlderHistory asks the phone for messages older than oldest; they
	// arrive later as core.MessagesLoaded.
	RequestOlderHistory(oldest core.Message) tea.Cmd
}

// Store is the persistence the app needs. Every call runs inside a tea.Cmd,
// never in Update.
type Store interface {
	GetAllConversations(ctx context.Context) ([]core.Conversation, error)
	UpsertConversation(ctx context.Context, conv core.Conversation) error
	ClearUnread(ctx context.Context, jid string) error
	InsertMessages(ctx context.Context, msgs []core.Message) error
	GetMessagesForChats(ctx context.Context, chatJIDs []string, limit int) ([]core.Message, error)
	GetMessagesBefore(ctx context.Context, chatJID string, before time.Time, limit int) ([]core.Message, error)
	UpdateMessageStatus(ctx context.Context, msgID, status string) error
	UpdateMessageMediaPath(ctx context.Context, chatJID, msgID, path string) error
}

var _ Store = (*store.Store)(nil)

// --- private message types ---

type conversationsLoadedMsg struct{ Conversations []core.Conversation }
type contactNamesMsg struct{ Names map[string]string }
type olderMessagesLoadedMsg struct {
	ChatJID  string
	Messages []core.Message
	Err      error
}

// chatLoadedMsg carries the stored history for a chat the user selected. Gen
// ties it to that selection so a load that finishes after the user moved on
// is dropped.
type chatLoadedMsg struct {
	JID      string
	Gen      int
	Messages []core.Message
	Err      error
}

// persistErrMsg reports a failed store read or write. It is shown briefly in
// the status bar; the in-memory state stays authoritative.
type persistErrMsg struct{ Err error }

// connectFailedMsg reports that the WhatsApp socket could not be opened. It is
// the only error that switches the app to StateError.
type connectFailedMsg struct{ Err error }

// historyRequestFailedMsg reports that the phone could not be asked for
// older messages (e.g. not connected).
type historyRequestFailedMsg struct{ Err error }

// mediaOpenFailedMsg reports that no external app could open a media file.
type mediaOpenFailedMsg struct{ Err error }
type reconnectMsg struct{}
type typingStopMsg struct{ gen int }
type clearStatusMsg struct{}

// ---

type Model struct {
	state State
	wa    WAClient
	store Store
	focus Panel

	// writes serializes store writes produced by Update; see writeQueue.
	writes *writeQueue
	// openGen counts chat selections so that only the latest load applies.
	openGen int
	// delay builds timer commands; tests replace it to avoid sleeping.
	delay func(d time.Duration, msg tea.Msg) tea.Cmd

	auth      auth.Model
	chatList  chatlist.Model
	chatView  chatview.Model
	input     input.Model
	titleBar  titlebar.Model
	statusBar statusbar.Model

	width  int
	height int

	connectedJID string
	lastErr      error

	// chats owns conversation/message state and its rules; Model only
	// translates the returned effects into UI updates, store writes and
	// WhatsApp calls.
	chats *core.Chats

	// Typing indicator state
	isTyping  bool
	typingGen int

	// Reconnect backoff
	reconnectAttempts int

	// pendingOpenMsgID holds a message ID that the user asked to open/play but
	// whose media was not yet downloaded. When the matching MediaDownloadedMsg
	// arrives, OpenMedia is dispatched automatically.
	pendingOpenMsgID string

	// historyAsked records, per chat, which oldest message the phone was last
	// asked to page back from and when (see olderFromPhone).
	historyAsked map[string]historyAsk
	// now is the clock; tests replace it.
	now func() time.Time

	log *debug.Logger
}

func NewModel(wa WAClient, s Store, version string, log *debug.Logger) Model {
	return Model{
		state:     StateAuth,
		wa:        wa,
		store:     s,
		writes:    &writeQueue{store: s},
		delay:     sleepThen,
		log:       log,
		focus:     PanelChatList,
		auth:      auth.New(),
		chatList:  chatlist.New(),
		chatView:  chatview.New(),
		input:     input.New(),
		titleBar:  titlebar.New(),
		statusBar: statusbar.New(version),
		chats:     core.NewChats(wa),

		historyAsked: make(map[string]historyAsk),
		now:          time.Now,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.auth.Init(), m.wa.Connect())
}
