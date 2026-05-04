package titlebar

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

type Model struct {
	chatName string
	chatJID  string
	isGroup  bool
	typing   string // who is typing, empty if nobody
	width    int
}

func New() Model {
	return Model{}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) SetWidth(w int) {
	m.width = w
}

func (m *Model) SetChat(name, jid string, isGroup bool) {
	m.chatName = name
	m.chatJID = jid
	m.isGroup = isGroup
	m.typing = ""
}

func (m *Model) SetTyping(who string) {
	m.typing = who
}

func (m *Model) ClearChat() {
	m.chatName = ""
	m.chatJID = ""
	m.isGroup = false
	m.typing = ""
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	return m, nil
}

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	if m.chatName == "" && m.chatJID == "" {
		return theme.TitleBarStyle.Width(m.width).Render("watui")
	}

	name := m.chatName
	if name == "" {
		name = m.chatJID
	}

	left := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText).Render(name)

	var right string
	if m.typing != "" {
		right = lipgloss.NewStyle().Foreground(theme.ColorPrimary).Italic(true).Render(m.typing + " is typing...")
	} else if m.isGroup {
		right = lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("group")
	}

	rightRendered := right
	gap := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(rightRendered)-2)
	content := left + strings.Repeat(" ", gap) + rightRendered

	return theme.TitleBarStyle.Width(m.width).Render(content)
}
