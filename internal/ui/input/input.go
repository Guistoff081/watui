package input

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

// SendMsg is emitted when the user presses Enter to send a message.
type SendMsg struct {
	Text string
}

type Model struct {
	textarea textarea.Model
	width    int
	height   int
	focused  bool
}

func New() Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 4096
	ta.SetHeight(2)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(theme.ColorText)
	ta.BlurredStyle.Text = lipgloss.NewStyle().Foreground(theme.ColorText)
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(theme.ColorPrimary)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	ta.Blur()

	return Model{
		textarea: ta,
		height:   3, // border (1) + 2 lines of textarea
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) SetSize(w, _ int) {
	m.width = w
	m.textarea.SetWidth(w - 2) // account for border padding
}

func (m *Model) SetFocused(focused bool) {
	m.focused = focused
	if focused {
		m.textarea.Focus()
	} else {
		m.textarea.Blur()
	}
}

func (m Model) Focused() bool {
	return m.focused
}

func (m Model) Value() string {
	return m.textarea.Value()
}

func (m *Model) Reset() {
	m.textarea.Reset()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			text := strings.TrimSpace(m.textarea.Value())
			if text != "" {
				m.textarea.Reset()
				return m, func() tea.Msg {
					return SendMsg{Text: text}
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var style lipgloss.Style
	if m.focused {
		style = theme.InputFocusedStyle
	} else {
		style = theme.InputStyle
	}

	return style.Width(m.width).Render(m.textarea.View())
}

// Height returns the total height of the input component (including border).
func (m Model) Height() int {
	return m.height
}
