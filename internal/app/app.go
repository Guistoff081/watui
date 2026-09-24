package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/debug"
	"github.com/watui/watui/internal/store"
	"github.com/watui/watui/internal/theme"
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
}

// --- private message types ---

type conversationsLoadedMsg struct{ Conversations []core.Conversation }
type contactNamesMsg struct{ Names map[string]string }
type olderMessagesLoadedMsg struct {
	ChatJID  string
	Messages []core.Message
}
type reconnectMsg struct{}
type typingStopMsg struct{ gen int }
type clearStatusMsg struct{}

// ---

type Model struct {
	state State
	wa    WAClient
	store *store.Store
	focus Panel

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

	log *debug.Logger
}

func NewModel(wa WAClient, s *store.Store, version string, log *debug.Logger) Model {
	return Model{
		state:     StateAuth,
		wa:        wa,
		store:     s,
		log:       log,
		focus:     PanelChatList,
		auth:      auth.New(),
		chatList:  chatlist.New(),
		chatView:  chatview.New(),
		input:     input.New(),
		titleBar:  titlebar.New(),
		statusBar: statusbar.New(version),
		chats:     core.NewChats(wa),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.auth.Init(), m.wa.Connect())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if m.log != nil {
		m.log.LogMsg(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		if m.state == StateAuth {
			var cmd tea.Cmd
			m.auth, cmd = m.auth.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	// --- Auth ---
	case core.QRCode:
		m.state = StateAuth
		m.auth.SetDimensions(m.width, m.height)
		m.auth.SetQRCode(msg.Code)

	case core.QRTimeout:
		m.state = StateAuth
		m.auth.SetDimensions(m.width, m.height)
		m.auth.SetQRTimeout()
		if m.log != nil {
			m.log.Info("QR code expired, requesting new codes")
		}
		cmds = append(cmds, m.wa.Connect())

	case core.LoginSuccess:
		m.connectedJID = msg.JID.String()
		m.auth.SetStatus("QR scanned. Finishing login...")

	case core.Connected:
		m.connectedJID = msg.JID.String()
		m.state = StateChat
		m.lastErr = nil
		m.reconnectAttempts = 0
		m.statusBar.SetConnected(m.connectedJID)
		m.statusBar.SetMessage("Loading conversations...")
		m.applyFocus()
		m.layout()
		cmds = append(cmds, m.loadConversationsCmd(), m.loadContactNamesCmd())

	case core.LoginFailed:
		m.state = StateError
		m.lastErr = msg.Err
		if m.log != nil && msg.Err != nil {
			m.log.Error(msg.Err, "login failed")
		}

	case core.ClientOutdated:
		m.state = StateError
		m.lastErr = fmt.Errorf(
			"WhatsApp rejected the connection: client version outdated (405).\n\n" +
				"Update the WhatsApp library and rebuild:\n" +
				"  go get -u go.mau.fi/whatsmeow@latest && go mod tidy",
		)
		if m.log != nil {
			m.log.Error(m.lastErr, "client outdated")
		}

	case core.Disconnected:
		m.statusBar.SetDisconnected()
		if m.state == StateChat && msg.Err == nil && m.reconnectAttempts < 5 {
			// Unexpected disconnect — schedule a reconnect attempt with linear backoff.
			m.reconnectAttempts++
			delay := time.Duration(m.reconnectAttempts) * 3 * time.Second
			m.statusBar.SetMessage(fmt.Sprintf("Reconnecting... (attempt %d)", m.reconnectAttempts))
			cmds = append(cmds, reconnectAfterDelay(delay))
		} else if m.state != StateChat {
			m.state = StateError
			if msg.Err != nil {
				m.lastErr = msg.Err
			} else {
				m.lastErr = fmt.Errorf("disconnected from WhatsApp")
			}
			if m.log != nil {
				m.log.Error(m.lastErr, "disconnected")
			}
		}

	case reconnectMsg:
		cmds = append(cmds, m.wa.Connect())

	// --- Conversations ---
	case conversationsLoadedMsg:
		m.chats.Load(msg.Conversations)
		for _, conv := range msg.Conversations {
			m.chatList.UpsertConversation(conv)
		}
		m.statusBar.ClearMessage()

	case contactNamesMsg:
		m.applyEffects(m.chats.ApplyNames(msg.Names))

	case core.ConversationUpdated:
		m.applyEffects(m.chats.UpdateConversation(msg.Conversation))

	case core.MessagesLoaded:
		// Merge (don't overwrite): history-sync batches can arrive after live
		// messages, and must not discard them.
		eff := m.chats.AddHistory(msg.ChatJID.String(), msg.Messages, m.chatView.ChatJID())
		m.applyEffects(eff)
		if eff.InView {
			m.reloadChatView(eff.Chat)
		}

	case core.HistorySyncComplete:
		m.statusBar.ClearMessage()
		// Contacts and groups are populated by now; re-resolve any names that are
		// still showing as raw JIDs (e.g. LID-addressed chats synced after connect).
		cmds = append(cmds, m.loadContactNamesCmd())

	// --- Lazy-load older messages ---
	case chatview.LoadOlderMsg:
		cmds = append(cmds, m.loadOlderMessagesCmd(msg.ChatJID))

	case olderMessagesLoadedMsg:
		eff := m.chats.PrependOlder(msg.ChatJID, msg.Messages, m.chatView.ChatJID())
		if eff.NoOlder {
			m.chatView.SetNoMoreMessages()
		} else if eff.InView {
			m.chatView.PrependMessages(eff.Prepend)
		}

	// --- Chat selection ---
	case chatlist.ChatSelectedCmd:
		return m.selectChat(msg.JID)

	// --- Messages ---
	case core.NewMessage:
		return m.handleNewMessage(msg.Message)

	case core.MessageSent:
		m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, "sent")

	case core.MessageSendFailed:
		m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, "failed")
		errText := "Send failed"
		if msg.Err != nil {
			errText = "Send failed: " + msg.Err.Error()
			if m.log != nil {
				m.log.Error(msg.Err, "message send failed", "chat", msg.ChatJID.String(), "id", msg.MessageID)
			}
		}
		m.statusBar.SetMessage(errText)
		cmds = append(cmds, clearStatusAfterDelay(4*time.Second))

	case core.MessageStatus:
		m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, msg.Status)

	// --- Media ---
	case core.MediaDownloaded:
		cmds = append(cmds, m.handleMediaDownloaded(msg))

	case core.MediaDownloadFailed:
		cmds = append(cmds, m.handleMediaDownloadFailed(msg))

	case chatview.MediaOpenMsg:
		cmds = append(cmds, m.handleMediaOpen(msg.ChatJID, msg.MessageID))

	// --- Typing indicators ---
	case core.Typing:
		if m.chatView.ChatJID() == msg.ChatJID.String() {
			if msg.IsTyping {
				m.titleBar.SetTyping(msg.Sender.User)
			} else {
				m.titleBar.SetTyping("")
			}
		}

	case typingStopMsg:
		// Only act if this timer is the most recent one (generation matches).
		if msg.gen == m.typingGen && m.isTyping {
			m.isTyping = false
			cmds = append(cmds, m.sendTypingPresenceCmd(false))
		}

	// --- Status bar ---
	case clearStatusMsg:
		m.statusBar.ClearMessage()

	// --- Input ---
	case input.SendMsg:
		return m.handleSendMessage(msg.Text)
	case input.SendFileMsg:
		return m.handleSendFile(msg.Path)
	case input.SendAudioMsg:
		return m.handleSendAudio(msg.Path)
	case input.PickerErrorMsg:
		m.statusBar.SetMessage(msg.Err)
		cmds = append(cmds, clearStatusAfterDelay(4*time.Second))

	case error:
		m.state = StateError
		m.lastErr = msg
		if m.log != nil {
			m.log.Error(msg, "unhandled error")
		}
	}

	if m.state == StateAuth {
		var cmd tea.Cmd
		m.auth, cmd = m.auth.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// --- Key handling ---

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		m.wa.Disconnect()
		return m, tea.Quit
	}

	if m.state != StateChat {
		if m.state == StateAuth {
			var cmd tea.Cmd
			m.auth, cmd = m.auth.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.focus == PanelInput {
		switch key {
		case "tab":
			return m, m.cycleFocus(1)
		case "shift+tab":
			return m, m.cycleFocus(-1)
		case "esc":
			return m, m.setFocus(PanelChatList)
		default:
			var cmds []tea.Cmd
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)

			// Send composing presence for printable keystrokes in text mode.
			if m.input.IsComposing() && isTypingKey(key) && m.chatView.ChatJID() != "" {
				if !m.isTyping {
					m.isTyping = true
					cmds = append(cmds, m.sendTypingPresenceCmd(true))
				}
				m.typingGen++
				cmds = append(cmds, typingStopAfterIdle(m.typingGen))
			}
			return m, tea.Batch(cmds...)
		}
	}

	switch key {
	case "tab":
		return m, m.cycleFocus(1)
	case "shift+tab":
		return m, m.cycleFocus(-1)
	case "esc":
		if m.focus == PanelMessages {
			return m, m.setFocus(PanelChatList)
		}
		return m, nil
	case "i":
		return m, m.setFocus(PanelInput)
	}

	var cmd tea.Cmd
	switch m.focus {
	case PanelChatList:
		m.chatList, cmd = m.chatList.Update(msg)
	case PanelMessages:
		m.chatView, cmd = m.chatView.Update(msg)
	}
	return m, cmd
}

// --- Focus management ---

// setFocus changes focus and, if leaving the input panel while composing,
// sends the "paused" typing presence.
func (m *Model) setFocus(p Panel) tea.Cmd {
	var cmd tea.Cmd
	if m.focus == PanelInput && p != PanelInput && m.isTyping {
		m.isTyping = false
		m.typingGen++
		cmd = m.sendTypingPresenceCmd(false)
	}
	m.focus = p
	m.applyFocus()
	return cmd
}

func (m *Model) cycleFocus(dir int) tea.Cmd {
	panels := []Panel{PanelChatList, PanelMessages, PanelInput}
	current := 0
	for i, p := range panels {
		if p == m.focus {
			current = i
			break
		}
	}
	next := (current + dir + len(panels)) % len(panels)
	return m.setFocus(panels[next])
}

func (m *Model) applyFocus() {
	m.chatList.SetFocused(m.focus == PanelChatList)
	m.chatView.SetFocused(m.focus == PanelMessages)
	m.input.SetFocused(m.focus == PanelInput)
}

// --- Chat selection ---

func (m *Model) selectChat(jid string) (Model, tea.Cmd) {
	conv, ok := m.chats.Conversation(jid)
	if !ok {
		return *m, nil
	}

	m.titleBar.SetChat(conv.Name, conv.JID, conv.IsGroup)

	// Always merge the persisted recent history with whatever is cached in memory
	// (live + offline-sync messages). Relying on the cache alone could show only
	// a handful of offline-synced messages.
	stored, _ := m.store.GetMessagesForChats(context.Background(), m.chats.Aliases(jid), 200)
	eff, _ := m.chats.Open(jid, stored)
	m.reloadChatView(jid)
	m.applyEffects(eff)

	var cmds []tea.Cmd
	for _, msg := range eff.Downloads {
		cmds = append(cmds, m.wa.DownloadMedia(msg))
	}
	cmds = append(cmds, m.setFocus(PanelMessages))
	return *m, tea.Batch(cmds...)
}

// applyEffects performs the store writes, chat-list updates and read receipts
// a core.Chats operation asked for. View changes are left to the caller since
// they depend on the operation.
func (m *Model) applyEffects(eff core.Effects) {
	ctx := context.Background()
	if len(eff.Messages) > 0 {
		_ = m.store.InsertMessages(ctx, eff.Messages)
	}
	for _, conv := range eff.Conversations {
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(ctx, conv)
	}
	for _, jid := range eff.ClearUnread {
		m.chatList.ClearUnread(jid)
		_ = m.store.ClearUnread(ctx, jid)
	}
	m.sendReceipts(eff.Receipts)
}

// sendReceipts forwards read receipts to WhatsApp, skipping unparsable JIDs.
func (m *Model) sendReceipts(receipts []core.Receipt) {
	for _, r := range receipts {
		chat, err := types.ParseJID(r.Chat)
		if err != nil {
			continue
		}
		sender, err := types.ParseJID(r.Sender)
		if err != nil {
			continue
		}
		m.wa.MarkRead(chat, sender, r.IDs)
	}
}

// reloadChatView shows jid's cached messages in the chat view.
func (m *Model) reloadChatView(jid string) {
	conv, _ := m.chats.Conversation(jid)
	m.chatView.SetChat(jid, conv.IsGroup, m.chats.Messages(jid))
}

// --- Message handlers ---

func (m *Model) handleNewMessage(msg core.Message) (Model, tea.Cmd) {
	eff := m.chats.AddMessage(msg, m.chatView.ChatJID())
	m.applyEffects(eff)
	for _, msg := range eff.Append {
		m.chatView.AppendMessage(msg)
	}
	return *m, nil
}

// setMessageStatus updates a message's status in the in-memory cache, the open
// chat view, and the persistent store, keyed by message ID.
func (m *Model) setMessageStatus(chatJID, msgID, status string) {
	eff, ok := m.chats.SetStatus(chatJID, msgID, status, m.chatView.ChatJID())
	if !ok {
		return
	}
	if eff.InView {
		m.chatView.UpdateMessageStatus(msgID, status)
	}
	_ = m.store.UpdateMessageStatus(context.Background(), msgID, status)
}

// handleMediaDownloaded updates the in-memory cache and store with the downloaded
// path, invalidates the chatview thumbnail cache, and opens the media if this
// download was triggered by a pending user open/play request.
func (m *Model) handleMediaDownloaded(msg core.MediaDownloaded) tea.Cmd {
	eff := m.chats.SetMediaPath(msg.ChatJID, msg.MessageID, msg.Path, m.chatView.ChatJID())
	_ = m.store.UpdateMessageMediaPath(context.Background(), eff.Chat, msg.MessageID, msg.Path)

	m.chatView.InvalidateThumbnail(msg.MessageID)
	if eff.InView {
		m.reloadChatView(eff.Chat)
	}

	if m.pendingOpenMsgID == msg.MessageID {
		m.pendingOpenMsgID = ""
		if cached, ok := m.chats.Find(eff.Chat, msg.MessageID); ok {
			return m.wa.OpenMedia(msg.Path, cached.MediaType)
		}
	}
	return nil
}

// handleMediaDownloadFailed logs a failed download. If it was the one the user
// is waiting to open, the pending open is dropped and the error is surfaced in
// the status bar; background (sticker auto-download) failures are only logged.
func (m *Model) handleMediaDownloadFailed(msg core.MediaDownloadFailed) tea.Cmd {
	if m.log != nil && msg.Err != nil {
		m.log.Error(msg.Err, "media download failed", "msg", msg.MessageID)
	}
	if msg.MessageID == "" || m.pendingOpenMsgID != msg.MessageID {
		return nil
	}
	m.pendingOpenMsgID = ""
	errText := "Media download failed"
	if msg.Err != nil {
		errText += ": " + msg.Err.Error()
	}
	m.statusBar.SetMessage(errText)
	return clearStatusAfterDelay(4 * time.Second)
}

// handleMediaOpen opens or downloads-then-opens the media for the selected message.
func (m *Model) handleMediaOpen(chatJID, msgID string) tea.Cmd {
	msg, ok := m.chats.Find(chatJID, msgID)
	if !ok || msg.MediaType == "" {
		return nil
	}
	if msg.MediaPath != "" {
		return m.wa.OpenMedia(msg.MediaPath, msg.MediaType)
	}
	// Not yet downloaded — start download; open on completion.
	m.pendingOpenMsgID = msgID
	return m.wa.DownloadMedia(msg)
}

// addOutgoingMessage records an optimistic outgoing message in the cache, view,
// store, and conversation preview, returning the message with its generated ID.
func (m *Model) addOutgoingMessage(chatJID, id, content string) core.Message {
	msg := core.Message{
		ID:        id,
		ChatJID:   chatJID,
		Content:   content,
		Timestamp: time.Now(),
		IsFromMe:  true,
		Status:    "sending",
	}
	eff := m.chats.AddOutgoing(msg)
	for _, msg := range eff.Append {
		m.chatView.AppendMessage(msg)
	}
	m.applyEffects(eff)
	return msg
}

func (m *Model) handleSendMessage(text string) (Model, tea.Cmd) {
	chatJID := m.chatView.ChatJID()
	if chatJID == "" {
		return *m, nil
	}
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return *m, nil
	}

	id := m.wa.GenerateMessageID()
	m.addOutgoingMessage(chatJID, id, text)

	var cmds []tea.Cmd
	if m.isTyping {
		m.isTyping = false
		m.typingGen++
		cmds = append(cmds, m.sendTypingPresenceCmd(false))
	}
	cmds = append(cmds, m.wa.SendTextMessage(jid, id, text))
	return *m, tea.Batch(cmds...)
}

func (m *Model) handleSendFile(path string) (Model, tea.Cmd) {
	chatJID := m.chatView.ChatJID()
	if chatJID == "" {
		return *m, nil
	}
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return *m, nil
	}

	id := m.wa.GenerateMessageID()
	label := fmt.Sprintf("[file] %s", filepath.Base(path))
	m.addOutgoingMessage(chatJID, id, label)

	var cmds []tea.Cmd
	if m.isTyping {
		m.isTyping = false
		m.typingGen++
		cmds = append(cmds, m.sendTypingPresenceCmd(false))
	}
	cmds = append(cmds, m.wa.SendFileMessage(jid, id, path))
	return *m, tea.Batch(cmds...)
}

func (m *Model) handleSendAudio(path string) (Model, tea.Cmd) {
	chatJID := m.chatView.ChatJID()
	if chatJID == "" {
		return *m, nil
	}
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return *m, nil
	}

	id := m.wa.GenerateMessageID()
	label := fmt.Sprintf("[voice] %s", filepath.Base(path))
	m.addOutgoingMessage(chatJID, id, label)

	var cmds []tea.Cmd
	if m.isTyping {
		m.isTyping = false
		m.typingGen++
		cmds = append(cmds, m.sendTypingPresenceCmd(false))
	}
	cmds = append(cmds, m.wa.SendAudioMessage(jid, id, path))
	return *m, tea.Batch(cmds...)
}

// --- Commands ---

func (m Model) loadConversationsCmd() tea.Cmd {
	return func() tea.Msg {
		convs, err := m.store.GetAllConversations(context.Background())
		if err != nil {
			return nil
		}
		return conversationsLoadedMsg{Conversations: convs}
	}
}

func (m Model) loadContactNamesCmd() tea.Cmd {
	return func() tea.Msg {
		names := m.wa.GetAllContactNames()
		if names == nil {
			names = make(map[string]string)
		}
		for jid, name := range m.wa.GetGroupNames() {
			names[jid] = name
		}
		return contactNamesMsg{Names: names}
	}
}

func (m Model) loadOlderMessagesCmd(chatJID string) tea.Cmd {
	msgs := m.chats.Messages(chatJID)
	if len(msgs) == 0 {
		return nil
	}
	before := msgs[0].Timestamp
	return func() tea.Msg {
		older, err := m.store.GetMessagesBefore(context.Background(), chatJID, before, 50)
		if err != nil {
			return olderMessagesLoadedMsg{ChatJID: chatJID}
		}
		return olderMessagesLoadedMsg{ChatJID: chatJID, Messages: older}
	}
}

func (m *Model) sendTypingPresenceCmd(composing bool) tea.Cmd {
	chatJID := m.chatView.ChatJID()
	if chatJID == "" {
		return nil
	}
	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		m.wa.SendChatPresence(jid, composing)
		return nil
	}
}

func reconnectAfterDelay(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return reconnectMsg{}
	}
}

func typingStopAfterIdle(gen int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(4 * time.Second)
		return typingStopMsg{gen: gen}
	}
}

func clearStatusAfterDelay(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return clearStatusMsg{}
	}
}

// isTypingKey returns true for keys that represent real text input
// (as opposed to navigation, control, or mode-switch keys).
func isTypingKey(key string) bool {
	switch key {
	case "tab", "shift+tab", "ctrl+c", "ctrl+f", "ctrl+p", "enter", "esc",
		"up", "down", "left", "right", "home", "end", "pgup", "pgdown",
		"backspace", "delete":
		return false
	}
	return !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") &&
		!strings.HasPrefix(key, "f") // function keys
}

// --- Layout ---

func (m *Model) layout() {
	if m.state != StateChat || m.width <= 0 || m.height <= 0 {
		return
	}

	titleH := 1
	statusH := 1
	inputH := m.input.Height()
	bodyH := m.height - titleH - statusH - inputH
	if bodyH < 1 {
		bodyH = 1
	}

	chatListW := int(float64(m.width) * 0.30)
	if chatListW < 15 {
		chatListW = 15
	}
	msgViewW := m.width - chatListW

	m.titleBar.SetWidth(m.width)
	m.statusBar.SetWidth(m.width)
	m.chatList.SetSize(chatListW, bodyH)
	m.chatView.SetSize(msgViewW, bodyH)
	m.input.SetSize(m.width, inputH)
}

// --- View ---

func (m Model) View() string {
	switch m.state {
	case StateChat:
		return m.renderChat()
	case StateError:
		return m.renderError()
	default:
		return m.auth.View()
	}
}

func (m Model) renderChat() string {
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.chatList.View(), m.chatView.View())
	return lipgloss.JoinVertical(lipgloss.Left,
		m.titleBar.View(),
		body,
		m.input.View(),
		m.statusBar.View(),
	)
}

func (m Model) renderError() string {
	errMsg := "unknown error"
	if m.lastErr != nil {
		errMsg = m.lastErr.Error()
	}
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(theme.ColorError).Render("Connection error")+
			"\n\n"+errMsg+"\n\nPress Ctrl+C to exit.",
	)
}
