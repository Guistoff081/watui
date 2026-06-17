package chatview

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

// LoadOlderMsg is emitted when the user scrolls to the top and more history
// may be available.
type LoadOlderMsg struct {
	ChatJID string
}

type Model struct {
	viewport viewport.Model
	messages []theme.Message
	chatJID  string
	isGroup  bool
	width    int
	height   int
	focused  bool
	atBottom bool

	// Lazy-load state
	oldestTS time.Time // timestamp of the oldest message currently in view
	loading  bool      // a LoadOlderMsg has been emitted; waiting for the response
	noMore   bool      // store confirmed no messages exist before oldestTS
}

func New() Model {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	return Model{atBottom: true}
}

func (m Model) Init() tea.Cmd { return nil }

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
	m.rebuildContent()
}

func (m *Model) SetFocused(focused bool) { m.focused = focused }

func (m Model) Focused() bool { return m.focused }

func (m *Model) SetChat(jid string, isGroup bool, messages []theme.Message) {
	m.chatJID = jid
	m.isGroup = isGroup
	m.messages = messages
	m.atBottom = true
	m.loading = false
	m.noMore = false
	// Defensive: guarantee ascending order so the newest messages render at the
	// bottom and oldestTS (used for lazy-loading) is correct.
	sort.SliceStable(m.messages, func(i, j int) bool {
		return m.messages[i].Timestamp.Before(m.messages[j].Timestamp)
	})
	if len(m.messages) > 0 {
		m.oldestTS = m.messages[0].Timestamp
	} else {
		m.oldestTS = time.Time{}
	}
	m.rebuildContent()
	m.viewport.GotoBottom()
}

func (m *Model) AppendMessage(msg theme.Message) {
	if msg.ChatJID != m.chatJID {
		return
	}
	wasAtBottom := m.atBottom

	// Live messages are usually the newest, but offline-sync replay can deliver
	// messages with older timestamps; insert those in order instead of at the bottom.
	isNewest := len(m.messages) == 0 ||
		!msg.Timestamp.Before(m.messages[len(m.messages)-1].Timestamp)
	if isNewest {
		m.messages = append(m.messages, msg)
	} else {
		idx := sort.Search(len(m.messages), func(i int) bool {
			return m.messages[i].Timestamp.After(msg.Timestamp)
		})
		m.messages = append(m.messages, theme.Message{})
		copy(m.messages[idx+1:], m.messages[idx:])
		m.messages[idx] = msg
	}

	m.rebuildContent()
	if wasAtBottom && isNewest {
		m.viewport.GotoBottom()
	}
}

// PrependMessages inserts older messages above the current history and adjusts
// the viewport so the previously-visible content stays in view.
func (m *Model) PrependMessages(msgs []theme.Message) {
	if len(msgs) == 0 {
		m.loading = false
		return
	}

	m.loading = false
	m.oldestTS = msgs[0].Timestamp

	// Estimate how many rendered lines the new messages will add.
	var newLines []string
	var lastDate string
	for _, msg := range msgs {
		dateStr := msg.Timestamp.Format("2006-01-02")
		if dateStr != lastDate {
			newLines = append(newLines, renderDateSeparator(msg.Timestamp, m.width))
			lastDate = dateStr
		}
		newLines = append(newLines, renderMessage(msg, m.width, m.isGroup))
	}
	addedLines := strings.Count(strings.Join(newLines, "\n"), "\n") + 1

	m.messages = append(msgs, m.messages...)
	savedOffset := m.viewport.YOffset
	m.rebuildContent()
	m.viewport.SetYOffset(savedOffset + addedLines)
}

// SetNoMoreMessages signals that the store has no messages older than what is
// currently loaded, preventing further LoadOlderMsg emissions.
func (m *Model) SetNoMoreMessages() {
	m.loading = false
	m.noMore = true
}

func (m *Model) UpdateMessageStatus(msgID, status string) {
	for i := range m.messages {
		if m.messages[i].ID == msgID {
			m.messages[i].Status = status
			m.rebuildContent()
			return
		}
	}
}

func (m Model) ChatJID() string { return m.chatJID }

func (m Model) OldestTimestamp() time.Time { return m.oldestTS }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}
		switch {
		case key.Matches(msg, theme.DefaultKeyMap.Up):
			m.viewport.LineUp(1)
		case key.Matches(msg, theme.DefaultKeyMap.Down):
			m.viewport.LineDown(1)
		case key.Matches(msg, theme.DefaultKeyMap.PageUp):
			m.viewport.HalfViewUp()
		case key.Matches(msg, theme.DefaultKeyMap.PageDown):
			m.viewport.HalfViewDown()
		case key.Matches(msg, theme.DefaultKeyMap.Top):
			m.viewport.GotoTop()
		case key.Matches(msg, theme.DefaultKeyMap.Bottom):
			m.viewport.GotoBottom()
		}
	}

	m.atBottom = m.viewport.AtBottom()

	// Emit a lazy-load request when scrolled to the top and there's more history.
	var cmd tea.Cmd
	if m.viewport.AtTop() &&
		m.viewport.TotalLineCount() > m.viewport.Height &&
		!m.loading && !m.noMore &&
		m.chatJID != "" && len(m.messages) > 0 {
		m.loading = true
		jid := m.chatJID
		cmd = func() tea.Msg { return LoadOlderMsg{ChatJID: jid} }
	}

	return m, cmd
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if m.chatJID == "" {
		return lipgloss.NewStyle().
			Foreground(theme.ColorTextDim).
			Width(m.width).Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Select a conversation to start chatting")
	}
	return m.viewport.View()
}

func (m *Model) rebuildContent() {
	if m.width <= 0 {
		return
	}
	var lines []string
	var lastDate string

	if m.loading {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(theme.ColorTextDim).
			Width(m.width).
			Align(lipgloss.Center).
			Render("Loading older messages..."))
	}

	for _, msg := range m.messages {
		dateStr := msg.Timestamp.Format("2006-01-02")
		if dateStr != lastDate {
			lines = append(lines, renderDateSeparator(msg.Timestamp, m.width))
			lastDate = dateStr
		}
		lines = append(lines, renderMessage(msg, m.width, m.isGroup))
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
}
