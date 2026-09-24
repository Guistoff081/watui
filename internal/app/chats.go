package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
)

// selectChat switches to jid at once (title, view from the cache, focus) and
// loads its stored history in the background; openLoadedChat finishes the
// open when the history arrives.
func (m *Model) selectChat(jid string) (Model, tea.Cmd) {
	conv, ok := m.chats.Conversation(jid)
	if !ok {
		return *m, nil
	}

	m.titleBar.SetChat(core.DisplayName(conv), conv.JID, conv.IsGroup)
	m.reloadChatView(jid)
	m.openGen++
	return *m, tea.Batch(m.loadChatCmd(jid, m.openGen), m.setFocus(PanelMessages))
}

// openLoadedChat merges the persisted recent history with whatever is cached
// in memory (live + offline-sync messages) and applies the open: relying on
// the cache alone could show only a handful of offline-synced messages. A
// load for a selection the user already left is dropped.
func (m *Model) openLoadedChat(msg chatLoadedMsg) (Model, tea.Cmd) {
	if msg.Gen != m.openGen || msg.JID != m.chatView.ChatJID() {
		return *m, nil
	}
	var cmds []tea.Cmd
	if msg.Err != nil {
		cmds = append(cmds, m.reportStoreErr(msg.Err))
	}
	eff, ok := m.chats.Open(msg.JID, msg.Messages)
	if !ok {
		return *m, tea.Batch(cmds...)
	}
	m.reloadChatView(msg.JID)
	cmds = append(cmds, m.applyEffects(eff))
	for _, dl := range eff.Downloads {
		cmds = append(cmds, m.wa.DownloadMedia(dl))
	}
	return *m, tea.Batch(cmds...)
}

// applyEffects updates the chat list for a core.Chats operation and returns
// the commands that persist it and send its read receipts. View changes are
// left to the caller since they depend on the operation.
func (m *Model) applyEffects(eff core.Effects) tea.Cmd {
	for _, conv := range eff.Conversations {
		m.chatList.UpsertConversation(conv)
	}
	for _, jid := range eff.ClearUnread {
		m.chatList.ClearUnread(jid)
	}
	return tea.Batch(m.persistEffects(eff), m.receiptsCmd(eff.Receipts))
}

// receiptsCmd sends read receipts to WhatsApp in the background, skipping
// unparsable JIDs.
func (m *Model) receiptsCmd(receipts []core.Receipt) tea.Cmd {
	type receipt struct {
		chat, sender types.JID
		ids          []string
	}
	var rs []receipt
	for _, r := range receipts {
		chat, err := types.ParseJID(r.Chat)
		if err != nil {
			continue
		}
		sender, err := types.ParseJID(r.Sender)
		if err != nil {
			continue
		}
		rs = append(rs, receipt{chat, sender, append([]string(nil), r.IDs...)})
	}
	if len(rs) == 0 {
		return nil
	}
	wa := m.wa
	return func() tea.Msg {
		for _, r := range rs {
			wa.MarkRead(r.chat, r.sender, r.ids)
		}
		return nil
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
	cmd := m.applyEffects(eff)
	for _, msg := range eff.Append {
		m.chatView.AppendMessage(msg)
	}
	return *m, cmd
}

// setMessageStatus updates a message's status in the in-memory cache, the open
// chat view, and (via the returned command) the store, keyed by message ID.
func (m *Model) setMessageStatus(chatJID, msgID, status string) tea.Cmd {
	eff, ok := m.chats.SetStatus(chatJID, msgID, status, m.chatView.ChatJID())
	if !ok {
		return nil
	}
	if eff.InView {
		m.chatView.UpdateMessageStatus(msgID, status)
	}
	return m.writes.enqueue(storeOp{"save message status", func(ctx context.Context, s Store) error {
		return s.UpdateMessageStatus(ctx, msgID, status)
	}})
}
