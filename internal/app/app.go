package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.mau.fi/whatsmeow/types"

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
	DownloadMedia(msg theme.Message) tea.Cmd
	OpenMedia(path, mediaType string) tea.Cmd
}

// --- private message types ---

type conversationsLoadedMsg struct{ Conversations []theme.Conversation }
type contactNamesMsg struct{ Names map[string]string }
type olderMessagesLoadedMsg struct {
	ChatJID  string
	Messages []theme.Message
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

	chatMessages  map[string][]theme.Message
	conversations map[string]theme.Conversation

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
		state:         StateAuth,
		wa:            wa,
		store:         s,
		log:           log,
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
	case theme.QRCodeMsg:
		m.state = StateAuth
		m.auth.SetDimensions(m.width, m.height)
		m.auth.SetQRCode(msg.Code)

	case theme.QRTimeoutMsg:
		m.state = StateAuth
		m.auth.SetDimensions(m.width, m.height)
		m.auth.SetQRTimeout()
		if m.log != nil {
			m.log.Info("QR code expired, requesting new codes")
		}
		cmds = append(cmds, m.wa.Connect())

	case theme.LoginSuccessMsg:
		m.connectedJID = msg.JID.String()
		m.auth.SetStatus("QR scanned. Finishing login...")

	case theme.ConnectedMsg:
		m.connectedJID = msg.JID.String()
		m.state = StateChat
		m.lastErr = nil
		m.reconnectAttempts = 0
		m.statusBar.SetConnected(m.connectedJID)
		m.statusBar.SetMessage("Loading conversations...")
		m.applyFocus()
		m.layout()
		cmds = append(cmds, m.loadConversationsCmd(), m.loadContactNamesCmd())

	case theme.LoginFailedMsg:
		m.state = StateError
		m.lastErr = msg.Err
		if m.log != nil && msg.Err != nil {
			m.log.Error(msg.Err, "login failed")
		}

	case theme.ClientOutdatedMsg:
		m.state = StateError
		m.lastErr = fmt.Errorf(
			"WhatsApp rejected the connection: client version outdated (405).\n\n" +
				"Update the WhatsApp library and rebuild:\n" +
				"  go get -u go.mau.fi/whatsmeow@latest && go mod tidy",
		)
		if m.log != nil {
			m.log.Error(m.lastErr, "client outdated")
		}

	case theme.DisconnectedMsg:
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
		for _, conv := range msg.Conversations {
			m.conversations[conv.JID] = conv
			m.chatList.UpsertConversation(conv)
		}
		m.statusBar.ClearMessage()

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

	case theme.ConversationUpdatedMsg:
		jid := msg.Conversation.JID
		updated := msg.Conversation
		if existing, ok := m.conversations[jid]; ok {
			// History sync may send a conversation update whose Last* is computed
			// only from the (limited) messages in that sync payload. Do not regress
			// the preview we have from live messages or prior state.
			if !updated.LastMsgTime.After(existing.LastMsgTime) {
				updated.LastMessage = existing.LastMessage
				updated.LastMsgTime = existing.LastMsgTime
			}
		}
		m.conversations[jid] = updated
		m.chatList.UpsertConversation(updated)
		_ = m.store.UpsertConversation(context.Background(), updated)

	case theme.MessagesLoadedMsg:
		jid := m.resolveConversationJID(msg.ChatJID.String())
		m.mergeAliasChatCache(jid)
		// Merge (don't overwrite): history-sync batches can arrive after live
		// messages, and must not discard them. Dedupe by ID and keep ascending order.
		merged := mergeMessages(m.chatMessages[jid], msg.Messages)
		m.chatMessages[jid] = merged
		_ = m.store.InsertMessages(context.Background(), msg.Messages)
		// If the loaded history batch contains a message newer than our current
		// preview, advance the conversation last so list/preview is up to date.
		if len(msg.Messages) > 0 {
			latest := msg.Messages[0]
			for _, m := range msg.Messages {
				if m.Timestamp.After(latest.Timestamp) {
					latest = m
				}
			}
			if conv, ok := m.conversations[jid]; ok && latest.Timestamp.After(conv.LastMsgTime) {
				conv.LastMessage = latest.Content
				conv.LastMsgTime = latest.Timestamp
				m.conversations[jid] = conv
				m.chatList.UpsertConversation(conv)
				_ = m.store.UpsertConversation(context.Background(), conv)
			}
		}
		if m.chatView.ChatJID() == jid {
			conv := m.conversations[jid]
			m.chatView.SetChat(jid, conv.IsGroup, merged)
		}

	case theme.HistorySyncCompleteMsg:
		m.statusBar.ClearMessage()
		// Contacts and groups are populated by now; re-resolve any names that are
		// still showing as raw JIDs (e.g. LID-addressed chats synced after connect).
		cmds = append(cmds, m.loadContactNamesCmd())

	// --- Lazy-load older messages ---
	case chatview.LoadOlderMsg:
		cmds = append(cmds, m.loadOlderMessagesCmd(msg.ChatJID))

	case olderMessagesLoadedMsg:
		if len(msg.Messages) == 0 {
			m.chatView.SetNoMoreMessages()
		} else {
			m.chatMessages[msg.ChatJID] = append(msg.Messages, m.chatMessages[msg.ChatJID]...)
			if m.chatView.ChatJID() == msg.ChatJID {
				m.chatView.PrependMessages(msg.Messages)
			}
		}

	// --- Chat selection ---
	case chatlist.ChatSelectedCmd:
		return m.selectChat(msg.JID)

	// --- Messages ---
	case theme.NewMessageMsg:
		return m.handleNewMessage(msg.Message)

	case theme.MessageSentMsg:
		m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, "sent")

	case theme.MessageSendFailedMsg:
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

	case theme.MessageStatusMsg:
		m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, msg.Status)

	// --- Media ---
	case theme.MediaDownloadedMsg:
		cmds = append(cmds, m.handleMediaDownloaded(msg))

	case theme.MediaDownloadFailedMsg:
		if m.log != nil && msg.Err != nil {
			m.log.Error(msg.Err, "media download failed", "msg", msg.MessageID)
		}

	case chatview.MediaOpenMsg:
		cmds = append(cmds, m.handleMediaOpen(msg.ChatJID, msg.MessageID))

	// --- Typing indicators ---
	case theme.TypingMsg:
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
	conv, ok := m.conversations[jid]
	if !ok {
		return *m, nil
	}

	m.titleBar.SetChat(conv.Name, conv.JID, conv.IsGroup)

	// Always merge the persisted recent history with whatever is cached in memory
	// (live + offline-sync messages), deduped and time-ordered. Relying on the
	// cache alone could show only a handful of offline-synced messages.
	stored, _ := m.store.GetMessagesForChats(context.Background(), m.chatJIDAliases(jid), 200)
	m.mergeAliasChatCache(jid)
	messages := mergeMessages(m.chatMessages[jid], stored)
	m.chatMessages[jid] = messages
	m.chatView.SetChat(jid, conv.IsGroup, messages)

	// Ensure the conversation preview reflects the actual newest message we just
	// loaded (defensive against any stale data from history sync etc).
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if last.Timestamp.After(conv.LastMsgTime) {
			conv.LastMessage = last.Content
			conv.LastMsgTime = last.Timestamp
			m.conversations[jid] = conv
			m.chatList.UpsertConversation(conv)
			_ = m.store.UpsertConversation(context.Background(), conv)
		}
	}

	m.chatList.ClearUnread(jid)
	_ = m.store.ClearUnread(context.Background(), jid)

	// Send read receipts to WhatsApp for received messages.
	m.markChatRead(jid, conv.IsGroup, messages)

	// Auto-download sticker files for the visible window (stickers have no embedded
	// thumbnail, so they need the full file before a half-block preview can render).
	const stickerAutoDownloadCap = 10
	var stickerCmds []tea.Cmd
	for _, msg := range messages {
		if msg.MediaType == "sticker" && msg.MediaPath == "" && msg.DirectPath != "" {
			stickerCmds = append(stickerCmds, m.wa.DownloadMedia(msg))
			if len(stickerCmds) >= stickerAutoDownloadCap {
				break
			}
		}
	}

	focusCmd := m.setFocus(PanelMessages)
	return *m, tea.Batch(append(stickerCmds, focusCmd)...)
}

func (m *Model) markChatRead(jid string, isGroup bool, messages []theme.Message) {
	parsedJID, err := types.ParseJID(jid)
	if err != nil || len(messages) == 0 {
		return
	}

	if isGroup {
		// Group chats require per-sender receipts.
		bySender := make(map[string][]string)
		for _, msg := range messages {
			if !msg.IsFromMe && msg.SenderJID != "" {
				bySender[msg.SenderJID] = append(bySender[msg.SenderJID], msg.ID)
			}
		}
		for senderStr, ids := range bySender {
			if senderJID, err := types.ParseJID(senderStr); err == nil {
				m.wa.MarkRead(parsedJID, senderJID, ids)
			}
		}
	} else {
		var ids []string
		for _, msg := range messages {
			if !msg.IsFromMe {
				ids = append(ids, msg.ID)
			}
		}
		if len(ids) > 0 {
			m.wa.MarkRead(parsedJID, parsedJID, ids)
		}
	}
}

// --- Message handlers ---

func (m *Model) handleNewMessage(msg theme.Message) (Model, tea.Cmd) {
	jid := m.resolveConversationJID(msg.ChatJID)
	msg.ChatJID = jid
	m.mergeAliasChatCache(jid)

	// WhatsApp can deliver the same message more than once — e.g. a group stanza
	// that carries both a sender-key distribution (pkmsg) and the content (skmsg)
	// is decrypted and dispatched twice. Drop duplicates by ID so they don't
	// appear twice or double-count as unread.
	for _, existing := range m.chatMessages[jid] {
		if existing.ID == msg.ID {
			return *m, nil
		}
	}

	m.chatMessages[jid] = insertMessageSorted(m.chatMessages[jid], msg)
	_ = m.store.InsertMessage(context.Background(), msg)

	conv, ok := m.conversations[jid]
	if !ok {
		name := msg.SenderName
		if name == "" {
			name = jid
		}
		conv = theme.Conversation{
			JID:     jid,
			Name:    name,
			IsGroup: strings.HasSuffix(jid, "@g.us"),
		}
		m.conversations[jid] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
		ok = true
	}
	if ok {
		// Don't let an older (offline-replayed) message overwrite a newer preview.
		if msg.Timestamp.After(conv.LastMsgTime) {
			conv.LastMessage = msg.Content
			conv.LastMsgTime = msg.Timestamp
		}
		if m.resolveConversationJID(m.chatView.ChatJID()) != jid {
			conv.UnreadCount++
		}
		m.conversations[jid] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
	}

	if m.resolveConversationJID(m.chatView.ChatJID()) == jid {
		m.chatView.AppendMessage(msg)
	}
	return *m, nil
}

// setMessageStatus updates a message's status in the in-memory cache, the open
// chat view, and the persistent store, keyed by message ID.
func (m *Model) setMessageStatus(chatJID, msgID, status string) {
	if msgID == "" {
		return
	}
	chatJID = m.resolveConversationJID(chatJID)
	m.mergeAliasChatCache(chatJID)

	msgs := m.chatMessages[chatJID]
	for i := range msgs {
		if msgs[i].ID == msgID {
			msgs[i].Status = status
			break
		}
	}
	m.chatMessages[chatJID] = msgs

	viewJID := m.resolveConversationJID(m.chatView.ChatJID())
	if viewJID == chatJID {
		m.chatView.UpdateMessageStatus(msgID, status)
	}
	_ = m.store.UpdateMessageStatus(context.Background(), msgID, status)
}

// handleMediaDownloaded updates the in-memory cache and store with the downloaded
// path, invalidates the chatview thumbnail cache, and opens the media if this
// download was triggered by a pending user open/play request.
func (m *Model) handleMediaDownloaded(msg theme.MediaDownloadedMsg) tea.Cmd {
	jid := m.resolveConversationJID(msg.ChatJID)
	msgs := m.chatMessages[jid]
	for i := range msgs {
		if msgs[i].ID == msg.MessageID {
			msgs[i].MediaPath = msg.Path
			break
		}
	}
	m.chatMessages[jid] = msgs

	_ = m.store.UpdateMessageMediaPath(context.Background(), jid, msg.MessageID, msg.Path)

	m.chatView.InvalidateThumbnail(msg.MessageID)
	if m.chatView.ChatJID() == jid {
		conv := m.conversations[jid]
		m.chatView.SetChat(jid, conv.IsGroup, msgs)
	}

	if m.pendingOpenMsgID == msg.MessageID {
		m.pendingOpenMsgID = ""
		for _, cached := range msgs {
			if cached.ID == msg.MessageID {
				return m.wa.OpenMedia(msg.Path, cached.MediaType)
			}
		}
	}
	return nil
}

// handleMediaOpen opens or downloads-then-opens the media for the selected message.
func (m *Model) handleMediaOpen(chatJID, msgID string) tea.Cmd {
	jid := m.resolveConversationJID(chatJID)
	for _, msg := range m.chatMessages[jid] {
		if msg.ID != msgID {
			continue
		}
		if msg.MediaType == "" {
			return nil
		}
		if msg.MediaPath != "" {
			return m.wa.OpenMedia(msg.MediaPath, msg.MediaType)
		}
		// Not yet downloaded — start download; open on completion.
		m.pendingOpenMsgID = msgID
		return m.wa.DownloadMedia(msg)
	}
	return nil
}

// addOutgoingMessage records an optimistic outgoing message in the cache, view,
// store, and conversation preview, returning the message with its generated ID.
func (m *Model) addOutgoingMessage(chatJID, id, content string) theme.Message {
	msg := theme.Message{
		ID:        id,
		ChatJID:   chatJID,
		Content:   content,
		Timestamp: time.Now(),
		IsFromMe:  true,
		Status:    "sending",
	}
	m.chatMessages[chatJID] = append(m.chatMessages[chatJID], msg)
	m.chatView.AppendMessage(msg)
	_ = m.store.InsertMessage(context.Background(), msg)

	if conv, ok := m.conversations[chatJID]; ok {
		conv.LastMessage = content
		conv.LastMsgTime = msg.Timestamp
		m.conversations[chatJID] = conv
		m.chatList.UpsertConversation(conv)
		_ = m.store.UpsertConversation(context.Background(), conv)
	}
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

// resolveConversationJID maps an incoming chat JID to the key used in m.conversations,
// trying the LID↔PN alternate when the direct key is missing.
func (m *Model) resolveConversationJID(jid string) string {
	if jid == "" {
		return jid
	}
	if _, ok := m.conversations[jid]; ok {
		return jid
	}
	if alt := m.wa.AltChatJID(jid); alt != "" {
		if _, ok := m.conversations[alt]; ok {
			return alt
		}
	}
	return jid
}

// chatJIDAliases returns jid and its LID↔PN alternate (if known) for cache/store lookups.
func (m *Model) chatJIDAliases(jid string) []string {
	if jid == "" {
		return nil
	}
	aliases := []string{jid}
	if alt := m.wa.AltChatJID(jid); alt != "" && alt != jid {
		aliases = append(aliases, alt)
	}
	return aliases
}

// mergeAliasChatCache folds messages cached under an alternate JID into the canonical key.
func (m *Model) mergeAliasChatCache(canonical string) {
	for _, alias := range m.chatJIDAliases(canonical) {
		if alias == canonical {
			continue
		}
		if msgs, ok := m.chatMessages[alias]; ok {
			m.chatMessages[canonical] = mergeMessages(m.chatMessages[canonical], msgs)
			delete(m.chatMessages, alias)
		}
	}
}

// mergeMessages combines message lists into a single slice, de-duplicating by ID
// (earlier lists win, so cached/live state isn't clobbered by history) and sorting
// ascending by timestamp for display.
func mergeMessages(lists ...[]theme.Message) []theme.Message {
	seen := make(map[string]struct{})
	var out []theme.Message
	for _, list := range lists {
		for _, msg := range list {
			if msg.ID != "" {
				if _, ok := seen[msg.ID]; ok {
					continue
				}
				seen[msg.ID] = struct{}{}
			}
			out = append(out, msg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

// insertMessageSorted inserts msg into a time-ascending slice at the correct
// position, so offline-replayed messages with older timestamps land in order.
func insertMessageSorted(msgs []theme.Message, msg theme.Message) []theme.Message {
	idx := sort.Search(len(msgs), func(i int) bool {
		return msgs[i].Timestamp.After(msg.Timestamp)
	})
	msgs = append(msgs, theme.Message{})
	copy(msgs[idx+1:], msgs[idx:])
	msgs[idx] = msg
	return msgs
}

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
	msgs := m.chatMessages[chatJID]
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
