package statusbar

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

type ConnState int

const (
	StateConnecting ConnState = iota
	StateConnected
	StateDisconnected
)

type Model struct {
	connState ConnState
	jid       string
	version   string
	width     int
	message   string // transient status message (e.g. "History sync...")
}

func New(version string) Model {
	return Model{
		connState: StateConnecting,
		version:   version,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) SetWidth(w int) {
	m.width = w
}

func (m *Model) SetConnected(jid string) {
	m.connState = StateConnected
	m.jid = jid
}

func (m *Model) SetDisconnected() {
	m.connState = StateDisconnected
}

func (m *Model) SetConnecting() {
	m.connState = StateConnecting
}

func (m *Model) SetMessage(msg string) {
	m.message = msg
}

func (m *Model) ClearMessage() {
	m.message = ""
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	return m, nil
}

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var connIndicator string
	switch m.connState {
	case StateConnected:
		connIndicator = theme.StatusConnected.Render("● Connected")
	case StateDisconnected:
		connIndicator = theme.StatusDisconnected.Render("● Disconnected")
	default:
		connIndicator = lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render("● Connecting...")
	}

	right := fmt.Sprintf("watui %s", m.version)
	if m.jid != "" {
		right += " | " + m.jid
	}

	left := connIndicator
	if m.message != "" {
		left += " | " + lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render(m.message)
	}

	rightRendered := lipgloss.NewStyle().Foreground(theme.ColorTextDim).Render(right)

	gap := max(0, m.width-lipgloss.Width(left)-lipgloss.Width(rightRendered)-2)
	content := left + strings.Repeat(" ", gap) + rightRendered

	return theme.StatusBarStyle.Width(m.width).Render(content)
}
