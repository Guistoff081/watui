package input

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

// SendMsg is emitted when the user presses Enter to send a text message.
type SendMsg struct {
	Text string
}

// SendFileMsg is emitted when the user confirms a file path to send as a document.
type SendFileMsg struct {
	Path string
}

// SendAudioMsg is emitted when the user confirms an audio file path to send as PTT.
type SendAudioMsg struct {
	Path string
}

type inputMode int

const (
	modeText  inputMode = iota
	modeFile            // Ctrl+F: file path prompt
	modeAudio           // Ctrl+P: audio file path prompt
)

type Model struct {
	textarea        textarea.Model
	pathInput       textinput.Model
	mode            inputMode
	width           int
	height          int
	focused         bool
	pickerAvailable bool
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

	pi := textinput.New()
	pi.CharLimit = 4096

	return Model{
		textarea:        ta,
		pathInput:       pi,
		height:          4, // top border (1) + textarea rows (2) + hint row (1)
		pickerAvailable: filePickerAvailable(),
	}
}

func (m Model) Init() tea.Cmd { return nil }

// normalizePath cleans a file path the way a shell would, so paths pasted or
// dragged from a file manager work even when they carry shell quoting/escaping
// (e.g. "/home/user/'My File.png'" or "/home/user/My\ File.png"). It also expands
// a leading "~" to the user's home directory.
func normalizePath(raw string) string {
	s := strings.TrimSpace(raw)

	var b strings.Builder
	var quote rune // 0 when unquoted, otherwise the active quote char
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			// Backslash escapes the next char, except inside single quotes.
			escaped = true
		case quote == 0 && (r == '\'' || r == '"'):
			quote = r
		case quote != 0 && r == quote:
			quote = 0
		default:
			b.WriteRune(r)
		}
	}

	out := b.String()
	if out == "~" || strings.HasPrefix(out, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			out = home + out[1:]
		}
	}
	return out
}

func (m *Model) SetSize(w, _ int) {
	m.width = w
	m.textarea.SetWidth(w - 2)
	if w > 14 {
		m.pathInput.Width = w - 14
	} else {
		m.pathInput.Width = w / 2
	}
}

func (m *Model) SetFocused(focused bool) {
	m.focused = focused
	if !focused {
		// Reset to text mode whenever focus leaves the panel.
		m.mode = modeText
		m.pathInput.Blur()
		m.textarea.Blur()
	} else {
		m.textarea.Focus()
	}
}

func (m Model) Focused() bool { return m.focused }

// IsComposing returns true when the input is active in text mode (not in file/audio
// path prompt). Used by the app layer to decide whether to send typing presence.
func (m Model) IsComposing() bool { return m.focused && m.mode == modeText }

func (m Model) Value() string { return m.textarea.Value() }

func (m *Model) Reset() { m.textarea.Reset() }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.mode {
		case modeFile, modeAudio:
			switch msg.String() {
			case "esc":
				m.mode = modeText
				m.pathInput.Reset()
				m.pathInput.Blur()
				taCmd := m.textarea.Focus()
				return m, taCmd

			case "ctrl+o":
				audio := m.mode == modeAudio
				m.mode = modeText
				m.pathInput.Reset()
				m.pathInput.Blur()
				taCmd := m.textarea.Focus()
				return m, tea.Batch(taCmd, pickFileCmd(audio))

			case "enter":
				path := normalizePath(m.pathInput.Value())
				if path == "" {
					return m, nil
				}
				prevMode := m.mode
				m.pathInput.Reset()
				m.mode = modeText
				m.pathInput.Blur()
				taCmd := m.textarea.Focus()
				if prevMode == modeFile {
					return m, tea.Batch(taCmd, func() tea.Msg { return SendFileMsg{Path: path} })
				}
				return m, tea.Batch(taCmd, func() tea.Msg { return SendAudioMsg{Path: path} })
			}

		case modeText:
			switch msg.String() {
			case "ctrl+f":
				m.mode = modeFile
				m.pathInput.Placeholder = "File path..."
				m.textarea.Blur()
				m.pathInput.Reset()
				return m, m.pathInput.Focus()

			case "ctrl+p":
				m.mode = modeAudio
				m.pathInput.Placeholder = "Audio file path..."
				m.textarea.Blur()
				m.pathInput.Reset()
				return m, m.pathInput.Focus()

			case "ctrl+o":
				return m, pickFileCmd(false)

			case "enter":
				text := strings.TrimSpace(m.textarea.Value())
				if text != "" {
					m.textarea.Reset()
					return m, func() tea.Msg { return SendMsg{Text: text} }
				}
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	if m.mode == modeText {
		m.textarea, cmd = m.textarea.Update(msg)
	} else {
		m.pathInput, cmd = m.pathInput.Update(msg)
	}
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

	dimStyle := lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	promptStyle := lipgloss.NewStyle().Foreground(theme.ColorPrimary).Bold(true)

	promptHint := "Enter to send  Esc to cancel"
	textHint := "ctrl+f attach  ctrl+p audio"
	if m.pickerAvailable {
		promptHint += "  ctrl+o browse"
		textHint += "  ctrl+o browse"
	}

	var content string
	switch m.mode {
	case modeFile:
		prompt := promptStyle.Render("Attach: ")
		content = prompt + m.pathInput.View() +
			"\n\n" + dimStyle.Render(promptHint)
	case modeAudio:
		prompt := promptStyle.Render("Audio:  ")
		content = prompt + m.pathInput.View() +
			"\n\n" + dimStyle.Render(promptHint)
	default:
		content = m.textarea.View() + "\n" + dimStyle.Render(textHint)
	}

	return style.Width(m.width).Render(content)
}

// Height returns the total rendered height of the input component.
func (m Model) Height() int { return m.height }
