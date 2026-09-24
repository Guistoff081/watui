package app

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"
)

func (m Model) loadConversationsCmd() tea.Cmd {
	s := m.store
	return func() tea.Msg {
		convs, err := s.GetAllConversations(context.Background())
		if err != nil {
			return persistErrMsg{Err: fmt.Errorf("load conversations: %w", err)}
		}
		return conversationsLoadedMsg{Conversations: convs}
	}
}

// loadChatCmd reads the recent stored history of jid and its alias for the
// selection numbered gen.
func (m Model) loadChatCmd(jid string, gen int) tea.Cmd {
	s, aliases := m.store, m.chats.Aliases(jid)
	return func() tea.Msg {
		msgs, err := s.GetMessagesForChats(context.Background(), aliases, 200)
		if err != nil {
			err = fmt.Errorf("load messages: %w", err)
		}
		return chatLoadedMsg{JID: jid, Gen: gen, Messages: msgs, Err: err}
	}
}

// loadContactNamesCmd resolves names for the chat list: address book and
// group subjects first, then verified business names for 1:1 chats still
// unnamed (numbers outside the address book, typically companies). The
// candidate list is taken now, on the Update goroutine; the lookups run in
// the command.
func (m Model) loadContactNamesCmd() tea.Cmd {
	unnamed := m.chats.UnnamedDirectChats()
	return func() tea.Msg {
		names := m.wa.GetAllContactNames()
		if names == nil {
			names = make(map[string]string)
		}
		for jid, name := range m.wa.GetGroupNames() {
			names[jid] = name
		}
		var ask []string
		for _, jid := range unnamed {
			if names[jid] == "" {
				ask = append(ask, jid)
			}
		}
		if len(ask) > 0 {
			for jid, name := range m.wa.GetVerifiedNames(ask) {
				names[jid] = name
			}
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
	s := m.store
	return func() tea.Msg {
		older, err := s.GetMessagesBefore(context.Background(), chatJID, before, 50)
		if err != nil {
			return olderMessagesLoadedMsg{ChatJID: chatJID, Err: fmt.Errorf("load older messages: %w", err)}
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

const (
	// statusTimeout is how long transient status-bar messages stay up.
	statusTimeout = 4 * time.Second
	// typingIdleTimeout ends the composing presence after the last keystroke.
	typingIdleTimeout = 4 * time.Second
)

// sleepThen is the production Model.delay: a command that yields msg after d.
func sleepThen(d time.Duration, msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return msg
	}
}

func (m *Model) clearStatusAfter(d time.Duration) tea.Cmd {
	return m.delay(d, clearStatusMsg{})
}
