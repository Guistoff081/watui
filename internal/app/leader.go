package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/ui/commands"
)

// handleLeaderKey advances the active leader sequence. Every key is consumed:
// unknown keys and Esc cancel without reaching the panel underneath.
func (m Model) handleLeaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.leader = nil
		return m, nil
	}
	next, cmd, ok := m.registry.Step(m.leader, msg.String())
	switch {
	case !ok:
		m.leader = nil
		return m, nil
	case cmd != nil:
		m.leader = nil
		id := cmd.ID
		return m, func() tea.Msg { return commands.InvokeMsg{ID: id} }
	default:
		m.leader = next
		return m, nil
	}
}

// dispatchCommand runs the command with id.
func (m *Model) dispatchCommand(id string) tea.Cmd {
	switch id {
	case "attach.file", "attach.audio", "attach.picker":
		if m.chatView.ChatJID() == "" {
			m.statusBar.SetMessage("Open a chat first")
			return m.clearStatusAfter(statusTimeout)
		}
		focus := m.setFocus(PanelInput)
		switch id {
		case "attach.file":
			return tea.Batch(focus, m.input.StartFilePrompt())
		case "attach.audio":
			return tea.Batch(focus, m.input.StartAudioPrompt())
		default:
			return tea.Batch(focus, m.input.PickFile())
		}
	case "help":
		m.openOverlay(helpOverlay{reg: m.registry})
	}
	return nil
}
