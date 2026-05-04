package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.mau.fi/whatsmeow/types"

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

// Focus panel identifiers
type Panel int

const (
	PanelChatList Panel = iota
	PanelMessages
	PanelInput
)

type WAClient interface {
	Connect() tea.Cmd
	Disconnect()
	SendTextMessage(jid types.JID, text string) tea.Cmd
	SendFileMessage(jid types.JID, path string) tea.Cmd
	SendAudioMessage(jid types.JID, path string) tea.Cmd
	MarkRead(chatJID types.JID, sender types.JID, messageIDs []string)
	GetAllContactNames() map[string]string
	GetGroupNames() map[string]string
}

// conversationsLoadedMsg is sent after loading conversations from the app store.
type conversationsLoadedMsg struct {
	Conversations []theme.Conversation
}

// contactNamesMsg is sent after loading contact names from whatsmeow.
type contactNamesMsg struct {
	Names map[string]string // JID -> name
}

type Model struct {
	state State
	wa    WAClient
	store *store.Store
	focus Panel

	// Sub-models
	auth      auth.Model
	chatList  chatlist.Model
	chatView  chatview.Model
	input     input.Model
	titleBar  titlebar.Model
	statusBar statusbar.Model

	// Window dimensions
	width  int
	height int

	// State
	connectedJID string
	lastErr      error

	// Conversation data: maps JID -> messages (in-memory cache)
	chatMessages map[string][]theme.Message
	// Conversation data: maps JID -> Conversation (in-memory cache)
	conversations map[string]theme.Conversation
}

func NewModel(wa WAClient, s *store.Store, version string) Model {
	return Model{
		state:         StateAuth,
		wa:            wa,
		store:         s,
		focus:         PanelChatList,
		auth:          auth.New(),
		chatList:      chatlist.New(),
		chatView:      chatview.New(),
		input:         input.New(),
		titleBar:      titlebar.New(),
		statusBar:     statusbar.New(version),
		chatMessages:  make(map[string][]theme.Message),
		conversations: make(map[string]theme.Conversation),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.auth.Init(),
		m.wa.Connect(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

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

	// --- Auth messages ---
	case theme.QRCodeMsg:
		m.state = StateAuth
		m.auth.SetQRCode(msg.Code)

	case theme.QRTimeoutMsg:
		m.state = StateAuth
		m.auth.SetQRTimeout()

	case theme.LoginSuccessMsg:
		m.connectedJID = msg.JID.String()
		m.auth.SetStatus("QR scanned. Finishing login...")

	case theme.ConnectedMsg:
		m.connectedJID = msg.JID.String()
		m.state = StateChat
		m.lastErr = nil
		m.statusBar.SetConnected(m.connectedJID)
		m.statusBar.SetMessage("Loading conversations...")
		m.applyFocus()
		m.layout()
		// Load conversations from the app-level store + enrich names from whatsmeow
		cmds = append(cmds, m.loadConversationsCmd(), m.loadContactNamesCmd())

	case theme.LoginFailedMsg:
		m.state = StateError
		m.lastErr = msg.Err

	case theme.DisconnectedMsg:
		m.statusBar.SetDisconnected()
		if m.state == StateChat {
			// Stay in chat state but show disconnected status
			m.statusBar.SetMessage("Reconnecting...")
		} else {
			m.state = StateError
			if msg.Err != nil {
				m.lastErr = msg.Err
			} else {
				m.lastErr = fmt.Errorf("disconnected from WhatsApp")
			}
		}

	// --- Conversations loaded from store ---
	case conversationsLoadedMsg:
		for _, conv := range msg.Conversations {
			m.conversations[conv.JID] = conv
			m.chatList.UpsertConversation(conv)
		}
		m.statusBar.ClearMessage()

	// --- Contact names enrichment from whatsmeow ---
	case contactNamesMsg:
		for jid, name := range msg.Names {
			if conv, ok := m.conversations[jid]; ok {
				if name != "" && (conv.Name == "" || conv.Name == conv.JID) {
					conv.Name = name
					m.conversations[jid] = conv
					m.chatList.UpsertConversation(conv)
					_ = m.store.UpsertConversation(context.Background(), conv)
				}
			}
		}

	// --- History sync (first pairing) ---
	case theme.ConversationUpdatedMsg:
		m.conversations[msg.Conversation.JID] = msg.Conversation
		m.chatList.UpsertConversation(msg.Conversation)
		// Persist to app store
		_ = m.store.UpsertConversation(context.Background(), msg.Conversation)

	case theme.MessagesLoadedMsg:
		jid := msg.ChatJID.String()
		m.chatMessages[jid] = msg.Messages
		// Persist to app store
		_ = m.store.InsertMessages(context.Background(), msg.Messages)
		// If this is the currently viewed chat, refresh the view
		if m.chatView.ChatJID() == jid {
			conv := m.conversations[jid]
			m.chatView.SetChat(jid, conv.IsGroup, msg.Messages)
		}

	case theme.HistorySyncCompleteMsg:
		m.statusBar.ClearMessage()

	// --- Chat selection ---
	case chatlist.ChatSelectedCmd:
		return m.selectChat(msg.JID)

	// --- New incoming message ---
	case theme.NewMessageMsg:
		return m.handleNewMessage(msg.Message)

	// --- Message sent ---
	case theme.MessageSentMsg:
		m.chatView.UpdateMessageStatus(msg.MessageID, "sent")

	case theme.MessageSendFailedMsg:
		m.chatView.UpdateMessageStatus(msg.MessageID, "failed")

	// --- Message status (delivery/read receipts) ---
	case theme.MessageStatusMsg:
		if m.chatView.ChatJID() == msg.ChatJID.String() {
			m.chatView.UpdateMessageStatus(msg.MessageID, msg.Status)
		}

	// --- Typing indicators ---
	case theme.TypingMsg:
		if m.chatView.ChatJID() == msg.ChatJID.String() {
			if msg.IsTyping {
				name := msg.Sender.User
				m.titleBar.SetTyping(name)
			} else {
				m.titleBar.SetTyping("")
			}
		}

	// --- Send message from input ---
	case input.SendMsg:
		return m.handleSendMessage(msg.Text)

	case input.SendFileMsg:
		return m.handleSendFile(msg.Path)

	case input.SendAudioMsg:
		return m.handleSendAudio(msg.Path)

	case error:
		m.state = StateError
		m.lastErr = msg
	}

	// Forward to sub-models based on state
	if m.state == StateAuth {
		var cmd tea.Cmd
		m.auth, cmd = m.auth.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// loadConversationsCmd loads all conversations from the app-level SQLite store.
func (m Model) loadConversationsCmd() tea.Cmd {
	return func() tea.Msg {
		convs, err := m.store.GetAllConversations(context.Background())
		if err != nil {
			return nil
		}
		return conversationsLoadedMsg{Conversations: convs}
	}
}

// loadContactNamesCmd loads contact and group names from whatsmeow's store
// and merges them into conversations that are missing names.
func (m Model) loadContactNamesCmd() tea.Cmd {
	return func() tea.Msg {
		names := m.wa.GetAllContactNames()
		if names == nil {
			names = make(map[string]string)
		}
		// Group names override contact names
		groupNames := m.wa.GetGroupNames()
		for jid, name := range groupNames {
			names[jid] = name
		}
		return contactNamesMsg{Names: names}
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keys
	if key == "ctrl+c" {
		m.wa.Disconnect()
		return m, tea.Quit
	}

	if m.state != StateChat {
		// In auth/error state, only handle window-level keys
		if m.state == StateAuth {
			var cmd tea.Cmd
			m.auth, cmd = m.auth.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// If input is focused, let it handle most keys except Tab/Escape
	if m.focus == PanelInput {
		switch key {
		case "tab":
			m.cycleFocus(1)
			return m, nil
		case "shift+tab":
			m.cycleFocus(-1)
			return m, nil
		case "esc":
			m.setFocus(PanelChatList)
			return m, nil
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	// Chat state key handling for non-input panels
	switch key {
	case "tab":
		m.cycleFocus(1)
		return m, nil
	case "shift+tab":
		m.cycleFocus(-1)
		return m, nil
	case "esc":
		if m.focus == PanelMessages {
			m.setFocus(PanelChatList)
		}
		return m, nil
	case "i":
		m.setFocus(PanelInput)
		return m, nil
	}

	// Forward to focused panel
	var cmd tea.Cmd
	switch m.focus {
	case PanelChatList:
		m.chatList, cmd = m.chatList.Update(msg)
	case PanelMessages:
		m.chatView, cmd = m.chatView.Update(msg)
	}

	return m, cmd
}

func (m *Model) selectChat(jid string) (Model, tea.Cmd) {
	conv, ok := m.conversations[jid]
	if !ok {
		return *m, nil
	}

	// Set title bar
	m.titleBar.SetChat(conv.Name, conv.JID, conv.IsGroup)

	// Load messages: try in-memory cache first, then from store
	messages := m.chatMessages[jid]
	if len(messages) == 0 {
		if stored, err := m.store.GetMessages(context.Background(), jid, 200); err == nil && len(stored) > 0 {
			messages = stored
			m.chatMessages[jid] = messages
		}
	}
	m.chatView.SetChat(jid, conv.IsGroup, messages)

	// Clear unread count
	m.chatList.ClearUnread(jid)
	_ = m.store.ClearUnread(context.Background(), jid)

	// Focus the message view
	m.setFocus(PanelMessages)

	return *m, nil
}

func (m *Model) handleNewMessage(msg theme.Message) (Model, tea.Cmd) {
	jid := msg.ChatJID

	// Store message in memory and persist
	m.chatMessages[jid] = append(m.chatMessages[jid], msg)
	_ = m.store.InsertMessage(context.Background(), msg)

	// Update conversation
	if conv, ok := m.conversations[jid]; ok {
		conv.LastMessage = msg.Content
		conv.LastMsgTime = msg.Timestamp
		if m.chatView.ChatJID() != jid {
			conv.UnreadCount++
		}
		m.conversations[jid] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
	}

	// If this is the currently viewed chat, append to the view
	if m.chatView.ChatJID() == jid {
		m.chatView.AppendMessage(msg)
	}

	return *m, nil
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

	// Create optimistic message
	msg := theme.Message{
		ID:        fmt.Sprintf("sending-%d", len(m.chatMessages[chatJID])),
		ChatJID:   chatJID,
		Content:   text,
		Timestamp: time.Now(),
		IsFromMe:  true,
		Status:    "sending",
	}

	m.chatMessages[chatJID] = append(m.chatMessages[chatJID], msg)
	m.chatView.AppendMessage(msg)

	// Update conversation preview
	if conv, ok := m.conversations[chatJID]; ok {
		conv.LastMessage = text
		conv.LastMsgTime = msg.Timestamp
		m.conversations[chatJID] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
	}

	return *m, m.wa.SendTextMessage(jid, text)
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

	label := fmt.Sprintf("[file] %s", filepath.Base(path))
	msg := theme.Message{
		ID:        fmt.Sprintf("sending-%d", time.Now().UnixNano()),
		ChatJID:   chatJID,
		Content:   label,
		Timestamp: time.Now(),
		IsFromMe:  true,
		Status:    "sending",
	}
	m.chatMessages[chatJID] = append(m.chatMessages[chatJID], msg)
	m.chatView.AppendMessage(msg)

	if conv, ok := m.conversations[chatJID]; ok {
		conv.LastMessage = label
		conv.LastMsgTime = msg.Timestamp
		m.conversations[chatJID] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
	}

	return *m, m.wa.SendFileMessage(jid, path)
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

	label := fmt.Sprintf("[voice] %s", filepath.Base(path))
	msg := theme.Message{
		ID:        fmt.Sprintf("sending-%d", time.Now().UnixNano()),
		ChatJID:   chatJID,
		Content:   label,
		Timestamp: time.Now(),
		IsFromMe:  true,
		Status:    "sending",
	}
	m.chatMessages[chatJID] = append(m.chatMessages[chatJID], msg)
	m.chatView.AppendMessage(msg)

	if conv, ok := m.conversations[chatJID]; ok {
		conv.LastMessage = label
		conv.LastMsgTime = msg.Timestamp
		m.conversations[chatJID] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
	}

	return *m, m.wa.SendAudioMessage(jid, path)
}

func (m *Model) cycleFocus(dir int) {
	panels := []Panel{PanelChatList, PanelMessages, PanelInput}
	current := 0
	for i, p := range panels {
		if p == m.focus {
			current = i
			break
		}
	}
	next := (current + dir + len(panels)) % len(panels)
	m.setFocus(panels[next])
}

func (m *Model) setFocus(p Panel) {
	m.focus = p
	m.applyFocus()
}

func (m *Model) applyFocus() {
	m.chatList.SetFocused(m.focus == PanelChatList)
	m.chatView.SetFocused(m.focus == PanelMessages)
	m.input.SetFocused(m.focus == PanelInput)
}

// layout recalculates dimensions for all sub-components.
func (m *Model) layout() {
	if m.state != StateChat || m.width <= 0 || m.height <= 0 {
		return
	}

	// Reserve: title bar (1 line), status bar (1 line), input (3 lines)
	titleH := 1
	statusH := 1
	inputH := m.input.Height()
	bodyH := m.height - titleH - statusH - inputH
	if bodyH < 1 {
		bodyH = 1
	}

	// Chat list: 30% width, message view: 70%
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
	// Title bar
	tb := m.titleBar.View()

	// Body: chat list | message view
	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.chatList.View(),
		m.chatView.View(),
	)

	// Input
	inp := m.input.View()

	// Status bar
	sb := m.statusBar.View()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		tb,
		body,
		inp,
		sb,
	)
}

func (m Model) renderError() string {
	errMsg := "unknown error"
	if m.lastErr != nil {
		errMsg = m.lastErr.Error()
	}
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(theme.ColorError).Render("Connection error")+
			"\n\n"+errMsg+
			"\n\nPress Ctrl+C to exit.",
	)
}
