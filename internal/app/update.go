package app

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/ui/chatlist"
	"github.com/watui/watui/internal/ui/chatview"
	"github.com/watui/watui/internal/ui/commands"
	"github.com/watui/watui/internal/ui/input"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if m.log != nil {
		m.log.LogMsg(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case commands.InvokeMsg:
		cmds = append(cmds, m.dispatchCommand(msg.ID))

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
		// Names are resolved once the stored conversations are loaded (see
		// conversationsLoadedMsg), so unnamed chats can be looked up.
		cmds = append(cmds, m.loadConversationsCmd())

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
			cmds = append(cmds, m.delay(delay, reconnectMsg{}))
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
		cmds = append(cmds, m.loadContactNamesCmd())

	case contactNamesMsg:
		cmds = append(cmds, m.applyEffects(m.chats.ApplyNames(msg.Names)))

	case core.ContactNameChanged:
		cmds = append(cmds, m.applyEffects(m.chats.ApplyNames(map[string]string{msg.JID: msg.Name})))

	case core.ConversationUpdated:
		cmds = append(cmds, m.applyEffects(m.chats.UpdateConversation(msg.Conversation)))

	case core.MessagesLoaded:
		// Merge (don't overwrite): history-sync batches can arrive after live
		// messages, and must not discard them.
		eff := m.chats.AddHistory(msg.ChatJID.String(), msg.Messages, m.chatView.ChatJID())
		cmds = append(cmds, m.applyEffects(eff))
		switch {
		case eff.InView && len(eff.Prepend) > 0:
			// An older page (on-demand from the phone): keep the reader's place.
			m.chatView.PrependMessages(eff.Prepend)
			m.statusBar.ClearMessage()
		case eff.InView:
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
		if msg.Err != nil {
			// A read failure is reported and loading stops; it is not the end
			// of history, so neither the phone is asked nor further loads
			// disabled.
			m.chatView.StopLoading()
			cmds = append(cmds, m.reportStoreErr(msg.Err))
			break
		}
		eff := m.chats.PrependOlder(msg.ChatJID, msg.Messages, m.chatView.ChatJID())
		if eff.NoOlder {
			cmds = append(cmds, m.olderFromPhone(eff.Chat))
		} else if eff.InView {
			m.chatView.PrependMessages(eff.Prepend)
		}

	// --- Chat selection ---
	case chatlist.ChatSelectedCmd:
		return m.selectChat(msg.JID)

	case chatLoadedMsg:
		return m.openLoadedChat(msg)

	case persistErrMsg:
		cmds = append(cmds, m.reportStoreErr(msg.Err))

	// --- Messages ---
	case core.NewMessage:
		return m.handleNewMessage(msg.Message)

	case core.MessageSent:
		cmds = append(cmds, m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, "sent"))

	case core.MessageSendFailed:
		cmds = append(cmds, m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, "failed"))
		errText := "Send failed"
		if msg.Err != nil {
			errText = "Send failed: " + msg.Err.Error()
			if m.log != nil {
				m.log.Error(msg.Err, "message send failed", "chat", msg.ChatJID.String(), "id", msg.MessageID)
			}
		}
		m.statusBar.SetMessage(errText)
		cmds = append(cmds, m.clearStatusAfter(statusTimeout))

	case core.MessageStatus:
		cmds = append(cmds, m.setMessageStatus(msg.ChatJID.String(), msg.MessageID, msg.Status))

	// --- Media ---
	case core.MediaDownloaded:
		cmds = append(cmds, m.handleMediaDownloaded(msg))

	case core.MediaDownloadFailed:
		cmds = append(cmds, m.handleMediaDownloadFailed(msg))

	case historyRequestFailedMsg:
		m.log.Error(msg.Err, "history request failed")
		m.statusBar.SetMessage("Could not ask your phone for older messages: " + msg.Err.Error())
		cmds = append(cmds, m.clearStatusAfter(statusTimeout))

	case mediaOpenFailedMsg:
		m.log.Error(msg.Err, "media open failed")
		m.statusBar.SetMessage("Could not open media: " + msg.Err.Error())
		cmds = append(cmds, m.clearStatusAfter(statusTimeout))

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
		cmds = append(cmds, m.clearStatusAfter(statusTimeout))

	case connectFailedMsg:
		m.state = StateError
		m.lastErr = msg.Err
		m.log.Error(msg.Err, "connect failed")

	case error:
		// A stray error is logged and shown; only connectFailedMsg (above) and
		// the login/disconnect events are fatal.
		m.log.Error(msg, "unhandled error")
		m.statusBar.SetMessage("Error: " + msg.Error())
		cmds = append(cmds, m.clearStatusAfter(statusTimeout))
	}

	if m.state == StateAuth {
		var cmd tea.Cmd
		m.auth, cmd = m.auth.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}
