package app

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/whatsapp"
)

// syncWAClient is the synchronous, context-aware API of *whatsapp.Client that
// the Bubble Tea adapter builds on. It exists so the adapter can be tested
// with a fake.
type syncWAClient interface {
	Connect(ctx context.Context) error
	Disconnect()
	GenerateMessageID() string
	SendText(ctx context.Context, jid types.JID, id, text string) (core.MessageSent, error)
	SendFile(ctx context.Context, jid types.JID, id, path string) (core.MessageSent, error)
	SendAudio(ctx context.Context, jid types.JID, id, path string) (core.MessageSent, error)
	SendChatPresence(ctx context.Context, jid types.JID, composing bool) error
	MarkRead(ctx context.Context, chatJID, sender types.JID, messageIDs []string) error
	GetAllContactNames(ctx context.Context) (map[string]string, error)
	GetGroupNames(ctx context.Context) (map[string]string, error)
	AltChatJID(ctx context.Context, jid string) string
	DownloadMedia(ctx context.Context, msg core.Message) (string, error)
	OpenMedia(path, mediaType string) error
}

var (
	_ syncWAClient = (*whatsapp.Client)(nil)
	_ WAClient     = (*waAdapter)(nil)
)

// waAdapter implements WAClient on top of the synchronous WhatsApp client:
// long-running calls are wrapped in tea.Cmds and their results mapped to the
// core events the app handles. Asynchronous session events (messages,
// receipts, QR codes, ...) do not pass through here; they reach the program
// via whatsapp.Client.SetEventHandler.
//
// Commands run until the operation finishes; the app has no cancellation
// story yet, so they use context.Background().
type waAdapter struct {
	c syncWAClient
}

// NewWAClient adapts c to the Bubble Tea WAClient interface.
func NewWAClient(c *whatsapp.Client) WAClient {
	return newWAAdapter(c)
}

func newWAAdapter(c syncWAClient) *waAdapter {
	return &waAdapter{c: c}
}

// Connect returns a command that connects (running the QR flow if needed).
// It yields nil on success, the bare error when the socket could not be opened
// before QR pairing (the app shows it as a fatal error), and core.LoginFailed
// for any other failure.
func (a *waAdapter) Connect() tea.Cmd {
	return func() tea.Msg {
		err := a.c.Connect(context.Background())
		if err == nil {
			return nil
		}
		var ce *whatsapp.ConnectError
		if errors.As(err, &ce) {
			return err
		}
		return core.LoginFailed{Err: err}
	}
}

func (a *waAdapter) Disconnect() { a.c.Disconnect() }

func (a *waAdapter) GenerateMessageID() string { return a.c.GenerateMessageID() }

type sendFunc func(ctx context.Context, jid types.JID, id, arg string) (core.MessageSent, error)

// sendCmd wraps a send call: success yields core.MessageSent, failure
// core.MessageSendFailed keyed by the optimistic message ID.
func sendCmd(send sendFunc, jid types.JID, id, arg string) tea.Cmd {
	return func() tea.Msg {
		sent, err := send(context.Background(), jid, id, arg)
		if err != nil {
			return core.MessageSendFailed{ChatJID: jid, MessageID: id, Err: err}
		}
		return sent
	}
}

func (a *waAdapter) SendTextMessage(jid types.JID, id, text string) tea.Cmd {
	return sendCmd(a.c.SendText, jid, id, text)
}

func (a *waAdapter) SendFileMessage(jid types.JID, id, path string) tea.Cmd {
	return sendCmd(a.c.SendFile, jid, id, path)
}

func (a *waAdapter) SendAudioMessage(jid types.JID, id, path string) tea.Cmd {
	return sendCmd(a.c.SendAudio, jid, id, path)
}

// SendChatPresence is best effort: a lost typing indicator is not worth
// surfacing.
func (a *waAdapter) SendChatPresence(jid types.JID, composing bool) {
	_ = a.c.SendChatPresence(context.Background(), jid, composing)
}

// MarkRead is best effort: the local unread state is already cleared, and a
// missed receipt only affects what the sender sees.
func (a *waAdapter) MarkRead(chatJID types.JID, sender types.JID, messageIDs []string) {
	_ = a.c.MarkRead(context.Background(), chatJID, sender, messageIDs)
}

// GetAllContactNames returns nil when the contact store cannot be read.
func (a *waAdapter) GetAllContactNames() map[string]string {
	names, err := a.c.GetAllContactNames(context.Background())
	if err != nil {
		return nil
	}
	return names
}

// GetGroupNames returns nil when the joined groups cannot be fetched.
func (a *waAdapter) GetGroupNames() map[string]string {
	names, err := a.c.GetGroupNames(context.Background())
	if err != nil {
		return nil
	}
	return names
}

func (a *waAdapter) AltChatJID(jid string) string {
	return a.c.AltChatJID(context.Background(), jid)
}

// DownloadMedia returns a command yielding core.MediaDownloaded or
// core.MediaDownloadFailed for msg.
func (a *waAdapter) DownloadMedia(msg core.Message) tea.Cmd {
	return func() tea.Msg {
		path, err := a.c.DownloadMedia(context.Background(), msg)
		if err != nil {
			return core.MediaDownloadFailed{ChatJID: msg.ChatJID, MessageID: msg.ID, Err: err}
		}
		return core.MediaDownloaded{ChatJID: msg.ChatJID, MessageID: msg.ID, Path: path}
	}
}

// OpenMedia returns a command that launches an external viewer. It yields no
// message: a viewer that fails to start has always been ignored.
func (a *waAdapter) OpenMedia(path, mediaType string) tea.Cmd {
	return func() tea.Msg {
		_ = a.c.OpenMedia(path, mediaType)
		return nil
	}
}
