package app

import (
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
)

// addOutgoingMessage records an optimistic outgoing message in the cache, view,
// conversation preview and (via the returned command) the store.
func (m *Model) addOutgoingMessage(chatJID, id, content string) tea.Cmd {
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
	return m.applyEffects(eff)
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
	cmds := []tea.Cmd{m.addOutgoingMessage(chatJID, id, text)}
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
	cmds := []tea.Cmd{m.addOutgoingMessage(chatJID, id, label)}
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
	cmds := []tea.Cmd{m.addOutgoingMessage(chatJID, id, label)}
	if m.isTyping {
		m.isTyping = false
		m.typingGen++
		cmds = append(cmds, m.sendTypingPresenceCmd(false))
	}
	cmds = append(cmds, m.wa.SendAudioMessage(jid, id, path))
	return *m, tea.Batch(cmds...)
}
