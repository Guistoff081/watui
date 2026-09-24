package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/core"
)

// handleMediaDownloaded updates the in-memory cache and store with the downloaded
// path, invalidates the chatview thumbnail cache, and opens the media if this
// download was triggered by a pending user open/play request.
func (m *Model) handleMediaDownloaded(msg core.MediaDownloaded) tea.Cmd {
	eff := m.chats.SetMediaPath(msg.ChatJID, msg.MessageID, msg.Path, m.chatView.ChatJID())
	persist := m.writes.enqueue(storeOp{"save media path", func(ctx context.Context, s Store) error {
		return s.UpdateMessageMediaPath(ctx, eff.Chat, msg.MessageID, msg.Path)
	}})

	m.chatView.InvalidateThumbnail(msg.MessageID)
	if eff.InView {
		m.reloadChatView(eff.Chat)
	}

	if m.pendingOpenMsgID == msg.MessageID {
		m.pendingOpenMsgID = ""
		if cached, ok := m.chats.Find(eff.Chat, msg.MessageID); ok {
			return tea.Batch(persist, m.wa.OpenMedia(msg.Path, cached.MediaType))
		}
	}
	return persist
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
	return m.clearStatusAfter(statusTimeout)
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
