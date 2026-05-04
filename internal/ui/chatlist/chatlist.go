package chatlist

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

type Model struct {
	items    []Item
	cursor   int
	offset   int
	width    int
	height   int
	focused  bool
	selected string // JID of selected chat
}

func New() Model {
	return Model{}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *Model) SetFocused(focused bool) {
	m.focused = focused
}

func (m Model) Focused() bool {
	return m.focused
}

func (m Model) SelectedJID() string {
	return m.selected
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.focused {
			return m, nil
		}
		switch {
		case key.Matches(msg, theme.DefaultKeyMap.Up):
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
		case key.Matches(msg, theme.DefaultKeyMap.Down):
			if m.cursor < len(m.items)-1 {
				m.cursor++
				visible := m.visibleCount()
				if m.cursor >= m.offset+visible {
					m.offset = m.cursor - visible + 1
				}
			}
		case key.Matches(msg, theme.DefaultKeyMap.Enter):
			if m.cursor >= 0 && m.cursor < len(m.items) {
				m.selected = m.items[m.cursor].JID()
				return m, func() tea.Msg {
					return ChatSelectedCmd{JID: m.selected}
				}
			}
		case key.Matches(msg, theme.DefaultKeyMap.Top):
			m.cursor = 0
			m.offset = 0
		case key.Matches(msg, theme.DefaultKeyMap.Bottom):
			if len(m.items) > 0 {
				m.cursor = len(m.items) - 1
				visible := m.visibleCount()
				if m.cursor >= visible {
					m.offset = m.cursor - visible + 1
				}
			}
		}
	}
	return m, nil
}

// ChatSelectedCmd is emitted when the user selects a chat from the list.
type ChatSelectedCmd struct {
	JID string
}

func (m *Model) UpsertConversation(conv theme.Conversation) {
	for i, item := range m.items {
		if item.JID() == conv.JID {
			m.items[i] = NewItem(conv)
			m.sortItems()
			return
		}
	}
	m.items = append(m.items, NewItem(conv))
	m.sortItems()
}

func (m *Model) ClearUnread(jid string) {
	for i, item := range m.items {
		if item.JID() == jid {
			conv := item.conversation
			conv.UnreadCount = 0
			m.items[i] = NewItem(conv)
			return
		}
	}
}

func (m *Model) sortItems() {
	sort.SliceStable(m.items, func(i, j int) bool {
		// Pinned items first
		if m.items[i].IsPinned() != m.items[j].IsPinned() {
			return m.items[i].IsPinned()
		}
		// Then by last message time (newest first)
		return m.items[i].LastMsgTime().After(m.items[j].LastMsgTime())
	})
}

func (m Model) visibleCount() int {
	// Each item takes 3 lines (name+time, preview, blank)
	if m.height <= 0 {
		return 10
	}
	return max(1, m.height/3)
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}

	var style lipgloss.Style
	if m.focused {
		style = theme.ChatListFocusedStyle
	} else {
		style = theme.ChatListStyle
	}

	if len(m.items) == 0 {
		placeholder := lipgloss.NewStyle().
			Foreground(theme.ColorTextDim).
			Width(m.width-2).
			Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("No conversations yet")
		return style.Width(m.width).Height(m.height).Render(placeholder)
	}

	visible := m.visibleCount()
	var lines []string

	for i := m.offset; i < len(m.items) && i < m.offset+visible; i++ {
		item := m.items[i]
		isSelected := i == m.cursor
		isCurrent := item.JID() == m.selected

		line := m.renderItem(item, isSelected, isCurrent)
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	return style.Width(m.width).Height(m.height).Render(content)
}

func (m Model) renderItem(item Item, isSelected, isCurrent bool) string {
	w := m.width - 3 // account for border + padding

	name := item.Title()
	timeStr := item.FormatTime()
	unread := item.FormatUnread()

	// Truncate name if needed
	nameMaxW := w - lipgloss.Width(timeStr) - 2
	if nameMaxW < 0 {
		nameMaxW = 0
	}
	if lipgloss.Width(name) > nameMaxW {
		name = name[:max(0, nameMaxW-1)] + "…"
	}

	// First line: name + time
	nameStyle := theme.ChatItemName
	if unread != "" {
		nameStyle = nameStyle.Foreground(theme.ColorText)
	}
	timeStyle := theme.ChatItemTime
	if unread != "" {
		timeStyle = timeStyle.Foreground(theme.ColorUnread)
	}

	nameRendered := nameStyle.Render(name)
	gap := max(0, w-lipgloss.Width(nameRendered)-lipgloss.Width(timeStr))
	line1 := nameRendered + strings.Repeat(" ", gap) + timeStyle.Render(timeStr)

	// Second line: preview + unread badge
	preview := item.Description()
	previewMaxW := w
	if unread != "" {
		previewMaxW = w - lipgloss.Width(unread) - 2
	}
	if lipgloss.Width(preview) > previewMaxW {
		preview = preview[:max(0, previewMaxW-1)] + "…"
	}

	previewRendered := theme.ChatItemPreview.Render(preview)
	line2 := previewRendered
	if unread != "" {
		gap2 := max(0, w-lipgloss.Width(previewRendered)-lipgloss.Width(unread)-1)
		line2 = previewRendered + strings.Repeat(" ", gap2) + theme.ChatItemUnread.Render(unread)
	}

	block := line1 + "\n" + line2 + "\n"

	if isSelected {
		prefix := ">"
		if isCurrent {
			prefix = "●"
		}
		block = lipgloss.NewStyle().
			Background(theme.ColorBgInput).
			Width(w).
			Render(prefix + " " + block)
	} else {
		prefix := " "
		if isCurrent {
			prefix = "●"
		}
		block = prefix + " " + block
	}

	return block
}
