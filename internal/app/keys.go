package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if key == "ctrl+c" {
		// Drain queued writes synchronously: tea.Quit exits before pending
		// flush commands run, and main closes the store right after.
		if err := m.writes.flush(); err != nil {
			m.log.Error(err, "store flush on quit")
		}
		m.wa.Disconnect()
		return m, tea.Quit
	}

	if m.state != StateChat {
		if m.state == StateAuth {
			var cmd tea.Cmd
			m.auth, cmd = m.auth.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Routing order: overlay, then an active leader sequence, then insert
	// mode (Space types), then Space starting the leader in normal mode.
	if m.overlay != nil {
		var cmd tea.Cmd
		m.overlay, cmd = m.overlay.Update(msg)
		return m, cmd
	}
	if m.leader != nil {
		return m.handleLeaderKey(msg)
	}
	if key == " " && m.focus != PanelInput {
		m.leader = []string{}
		return m, nil
	}

	if m.focus == PanelInput {
		switch key {
		case "tab":
			return m, m.cycleFocus(1)
		case "shift+tab":
			return m, m.cycleFocus(-1)
		case "esc":
			return m, m.setFocus(PanelChatList)
		default:
			var cmds []tea.Cmd
			var inputCmd tea.Cmd
			m.input, inputCmd = m.input.Update(msg)
			cmds = append(cmds, inputCmd)

			// Send composing presence for printable keystrokes in text mode.
			if m.input.IsComposing() && isTypingKey(key) && m.chatView.ChatJID() != "" {
				if !m.isTyping {
					m.isTyping = true
					cmds = append(cmds, m.sendTypingPresenceCmd(true))
				}
				m.typingGen++
				cmds = append(cmds, m.delay(typingIdleTimeout, typingStopMsg{gen: m.typingGen}))
			}
			return m, tea.Batch(cmds...)
		}
	}

	switch key {
	case "tab":
		return m, m.cycleFocus(1)
	case "shift+tab":
		return m, m.cycleFocus(-1)
	case "esc":
		if m.focus == PanelMessages {
			return m, m.setFocus(PanelChatList)
		}
		return m, nil
	case "i":
		return m, m.setFocus(PanelInput)
	}

	var cmd tea.Cmd
	switch m.focus {
	case PanelChatList:
		m.chatList, cmd = m.chatList.Update(msg)
	case PanelMessages:
		m.chatView, cmd = m.chatView.Update(msg)
	}
	return m, cmd
}

// --- Focus management ---

// setFocus changes focus and, if leaving the input panel while composing,
// sends the "paused" typing presence.
func (m *Model) setFocus(p Panel) tea.Cmd {
	var cmd tea.Cmd
	if m.focus == PanelInput && p != PanelInput && m.isTyping {
		m.isTyping = false
		m.typingGen++
		cmd = m.sendTypingPresenceCmd(false)
	}
	m.focus = p
	m.applyFocus()
	return cmd
}

func (m *Model) cycleFocus(dir int) tea.Cmd {
	panels := []Panel{PanelChatList, PanelMessages, PanelInput}
	current := 0
	for i, p := range panels {
		if p == m.focus {
			current = i
			break
		}
	}
	next := (current + dir + len(panels)) % len(panels)
	return m.setFocus(panels[next])
}

func (m *Model) applyFocus() {
	m.chatList.SetFocused(m.focus == PanelChatList)
	m.chatView.SetFocused(m.focus == PanelMessages)
	m.input.SetFocused(m.focus == PanelInput)
}

// isTypingKey returns true for keys that represent real text input
// (as opposed to navigation, control, or mode-switch keys).
func isTypingKey(key string) bool {
	switch key {
	case "tab", "shift+tab", "ctrl+c", "ctrl+f", "ctrl+p", "enter", "esc",
		"up", "down", "left", "right", "home", "end", "pgup", "pgdown",
		"backspace", "delete":
		return false
	}
	return !strings.HasPrefix(key, "ctrl+") && !strings.HasPrefix(key, "alt+") &&
		!strings.HasPrefix(key, "f") // function keys
}
