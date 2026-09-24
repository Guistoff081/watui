package app

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
	"github.com/watui/watui/internal/ui/whichkey"
)

func (m *Model) layout() {
	if m.state != StateChat || m.width <= 0 || m.height <= 0 {
		return
	}

	titleH := 1
	statusH := 1
	inputH := m.input.Height()
	bodyH := m.height - titleH - statusH - inputH
	if bodyH < 1 {
		bodyH = 1
	}

	chatListW := int(float64(m.width) * 0.30)
	if chatListW < 15 {
		chatListW = 15
	}
	msgViewW := m.width - chatListW

	m.titleBar.SetWidth(m.width)
	m.statusBar.SetWidth(m.width)
	m.chatList.SetSize(chatListW, bodyH)
	m.chatView.SetSize(msgViewW, bodyH)
	m.input.SetSize(m.width, inputH)
}

// --- View ---

func (m Model) View() string {
	switch m.state {
	case StateChat:
		return m.renderChat()
	case StateError:
		return m.renderError()
	default:
		return m.auth.View()
	}
}

func (m Model) renderChat() string {
	// Clip the body to its row budget so no panel can push the frame past the
	// terminal height (Bubble Tea would then drop lines, hiding the bars).
	bodyH := max(1, m.height-2-m.input.Height())
	body := lipgloss.NewStyle().MaxHeight(bodyH).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, m.chatList.View(), m.chatView.View()))
	// Overlays are spliced into the clipped body, so they never add rows.
	bodyW := m.width
	switch {
	case m.overlay != nil:
		box := m.overlay.View(bodyW-2, bodyH)
		body = placeOverlay(body, box, bodyW, bodyH, m.overlay.Centered())
	case m.leader != nil:
		box := whichkey.View(m.registry.Title(m.leader), m.registry.Options(m.leader), min(48, bodyW))
		body = placeOverlay(body, box, bodyW, bodyH, false)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.titleBar.View(),
		body,
		m.input.View(),
		m.statusBar.View(),
	)
}

func (m Model) renderError() string {
	errMsg := "unknown error"
	if m.lastErr != nil {
		errMsg = m.lastErr.Error()
	}
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Bold(true).Foreground(theme.ColorError).Render("Connection error")+
			"\n\n"+errMsg+"\n\nPress Ctrl+C to exit.",
	)
}
