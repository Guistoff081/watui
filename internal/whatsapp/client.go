package whatsapp

import (
	"context"
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	_ "github.com/mattn/go-sqlite3"
	"github.com/watui/watui/internal/theme"
	"go.mau.fi/whatsmeow"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	wm      *whatsmeow.Client
	store   *sqlstore.Container
	sendMsg func(tea.Msg)
	mu      sync.Mutex
	log     waLog.Logger
}

func NewClient(dbPath string) (*Client, error) {
	container, err := sqlstore.New(
		context.Background(),
		"sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", dbPath),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("whatsmeow store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}

	log := waLog.Stdout("whatsapp", "WARN", true)
	wm := whatsmeow.NewClient(deviceStore, log)

	c := &Client{
		wm:    wm,
		store: container,
		log:   log,
	}

	wm.AddEventHandler(c.handleEvent)

	return c, nil
}

// SetSendMsg sets the callback used to send messages to the Bubble Tea program.
// Must be called before Connect.
func (c *Client) SetSendMsg(fn func(tea.Msg)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sendMsg = fn
}

func (c *Client) send(msg tea.Msg) {
	c.mu.Lock()
	fn := c.sendMsg
	c.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// Connect starts the WhatsApp connection. If not logged in, initiates QR flow.
func (c *Client) Connect() tea.Cmd {
	return func() tea.Msg {
		if c.wm.Store.ID == nil {
			// Not logged in — start QR code flow
			qrChan, _ := c.wm.GetQRChannel(context.Background())
			err := c.wm.Connect()
			if err != nil {
				return tea.Msg(fmt.Errorf("connect: %w", err))
			}

			for evt := range qrChan {
				switch evt.Event {
				case "code":
					c.send(theme.QRCodeMsg{Code: evt.Code})
				case "timeout":
					c.send(theme.QRTimeoutMsg{})
				case "success":
					// Login event will be handled by the event handler
					return nil
				case "error":
					return theme.LoginFailedMsg{Err: fmt.Errorf("QR login failed")}
				}
			}
			return nil
		}

		// Already logged in
		err := c.wm.Connect()
		if err != nil {
			return theme.LoginFailedMsg{Err: err}
		}
		return nil
	}
}

// Disconnect cleanly disconnects from WhatsApp.
func (c *Client) Disconnect() {
	if c.wm != nil {
		c.wm.Disconnect()
	}
}

// SendTextMessage sends a text message to the given JID.
func (c *Client) SendTextMessage(jid types.JID, text string) tea.Cmd {
	return func() tea.Msg {
		resp, err := c.wm.SendMessage(context.Background(), jid, &waE2E.Message{
			Conversation: proto.String(text),
		})
		if err != nil {
			return theme.MessageSendFailedMsg{
				ChatJID:   jid,
				MessageID: "",
				Err:       err,
			}
		}
		return theme.MessageSentMsg{
			ChatJID:   jid,
			MessageID: resp.ID,
			Timestamp: resp.Timestamp,
		}
	}
}

// MarkRead sends read receipts for the given message IDs.
func (c *Client) MarkRead(chatJID types.JID, sender types.JID, messageIDs []string) {
	ids := make([]types.MessageID, 0, len(messageIDs))
	for _, id := range messageIDs {
		ids = append(ids, types.MessageID(id))
	}
	_ = c.wm.MarkRead(context.Background(), ids, time.Now(), chatJID.ToNonAD(), sender.ToNonAD())
}

// SendPresence sets the user's presence (available/unavailable).
func (c *Client) SendPresence(available bool) {
	if available {
		_ = c.wm.SendPresence(context.Background(), types.PresenceAvailable)
	} else {
		_ = c.wm.SendPresence(context.Background(), types.PresenceUnavailable)
	}
}

// SendChatPresence sends typing/stopped indicator.
func (c *Client) SendChatPresence(jid types.JID, composing bool) {
	if composing {
		_ = c.wm.SendChatPresence(context.Background(), jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	} else {
		_ = c.wm.SendChatPresence(context.Background(), jid, types.ChatPresencePaused, types.ChatPresenceMediaText)
	}
}

// GetContactName returns the best available name for a JID from whatsmeow's contact store.
func (c *Client) GetContactName(jid types.JID) string {
	contact, err := c.wm.Store.Contacts.GetContact(context.Background(), jid)
	if err != nil || !contact.Found {
		return ""
	}
	if contact.FullName != "" {
		return contact.FullName
	}
	if contact.PushName != "" {
		return contact.PushName
	}
	return contact.BusinessName
}

// GetAllContactNames returns a map of JID string -> best name for all contacts.
func (c *Client) GetAllContactNames() map[string]string {
	contacts, err := c.wm.Store.Contacts.GetAllContacts(context.Background())
	if err != nil {
		return nil
	}
	names := make(map[string]string, len(contacts))
	for jid, contact := range contacts {
		name := contact.FullName
		if name == "" {
			name = contact.PushName
		}
		if name == "" {
			name = contact.BusinessName
		}
		if name != "" {
			names[jid.String()] = name
		}
	}
	return names
}

// GetGroupNames returns a map of JID string -> group name for all joined groups.
func (c *Client) GetGroupNames() map[string]string {
	groups, err := c.wm.GetJoinedGroups(context.Background())
	if err != nil {
		return nil
	}
	names := make(map[string]string, len(groups))
	for _, g := range groups {
		if g.Name != "" {
			names[g.JID.String()] = g.Name
		}
	}
	return names
}

// JID returns the current user's JID (nil if not logged in).
func (c *Client) JID() *types.JID {
	return c.wm.Store.ID
}

// WMClient returns the underlying whatsmeow client for advanced operations.
func (c *Client) WMClient() *whatsmeow.Client {
	return c.wm
}
