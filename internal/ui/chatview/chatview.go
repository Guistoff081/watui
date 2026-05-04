package chatview

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

type Model struct {
	viewport viewport.Model
	messages []theme.Message
	chatJID  string
	isGroup  bool
	width    int
	height   int
	focused  bool
	atBottom bool
}

func New() Model {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	return Model{
		viewport: vp,
		atBottom: true,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
	m.rebuildContent()
}

func (m *Model) SetFocused(focused bool) {
	m.focused = focused
}

func (m Model) Focused() bool {
	return m.focused
}

func (m *Model) SetChat(jid string, isGroup bool, messages []theme.Message) {
	m.chatJID = jid
	m.isGroup = isGroup
	m.messages = messages
	m.atBottom = true
	m.rebuildContent()
	m.viewport.GotoBottom()
}

func (m *Model) AppendMessage(msg theme.Message) {
	if msg.ChatJID != m.chatJID {
		return
	}
	m.messages = append(m.messages, msg)
	wasAtBottom := m.atBottom
	m.rebuildContent()
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
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

func (m Model) ChatJID() string {
	return m.chatJID
}

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
	return m, nil
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	if m.chatJID == "" {
		placeholder := lipgloss.NewStyle().
			Foreground(theme.ColorTextDim).
			Width(m.width).
			Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Select a conversation to start chatting")
		return placeholder
	}

	return m.viewport.View()
}

func (m *Model) rebuildContent() {
	if m.width <= 0 {
		return
	}

	var lines []string
	var lastDate string

	for _, msg := range m.messages {
		dateStr := msg.Timestamp.Format("2006-01-02")
		if dateStr != lastDate {
			lines = append(lines, renderDateSeparator(msg.Timestamp, m.width))
			lastDate = dateStr
		}
		lines = append(lines, renderMessage(msg, m.width, m.isGroup))
	}

	content := strings.Join(lines, "\n")
	m.viewport.SetContent(content)
}
