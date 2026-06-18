package chatview

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/watui/watui/internal/theme"
)

// LoadOlderMsg is emitted when the user navigates past the oldest loaded message
// and more history may be available.
type LoadOlderMsg struct {
	ChatJID string
}

// MediaOpenMsg is emitted when the user presses Enter on a media message to
// open or play it.
type MediaOpenMsg struct {
	ChatJID   string
	MessageID string
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
	oldestTS time.Time
	loading  bool
	noMore   bool

	// Message selection cursor (-1 = none)
	selected int

	// lineOffsets[i] is the first line index of messages[i] in the viewport content.
	lineOffsets []int

	// Thumbnail ANSI block cache: key = msgID+":"+width → rendered ANSI string.
	// Populated by renderMessage; invalidated on width change or MediaDownloadedMsg.
	thumbCache map[string]string
}

func New() Model {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	return Model{
		atBottom:   true,
		selected:   -1,
		thumbCache: make(map[string]string),
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m *Model) SetSize(w, h int) {
	if w != m.width {
		// Width change invalidates all thumbnail cache entries.
		m.thumbCache = make(map[string]string)
	}
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
	m.selected = -1
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

	// Count lines the new messages will add using the cache where possible.
	var addedLines int
	var lastDate string
	for _, msg := range msgs {
		dateStr := msg.Timestamp.Format("2006-01-02")
		if dateStr != lastDate {
			addedLines++
			lastDate = dateStr
		}
		rendered := m.cachedRenderMessage(msg)
		addedLines += strings.Count(rendered, "\n") + 1
	}

	// Shift the selection index to account for the prepended messages.
	if m.selected >= 0 {
		m.selected += len(msgs)
	}

	m.messages = append(msgs, m.messages...)
	savedOffset := m.viewport.YOffset
	m.rebuildContent()
	m.viewport.SetYOffset(savedOffset + addedLines)
}

// SetNoMoreMessages signals that the store has no messages older than what is
// currently loaded.
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

// UpdateMessageMediaPath sets the local cache path on a message and rebuilds.
func (m *Model) UpdateMessageMediaPath(msgID, path string) {
	for i := range m.messages {
		if m.messages[i].ID == msgID {
			m.messages[i].MediaPath = path
			m.rebuildContent()
			return
		}
	}
}

// InvalidateThumbnail removes a message's rendered thumbnail from the cache so
// it will be re-generated on the next rebuild (e.g. after a sticker downloads).
func (m *Model) InvalidateThumbnail(msgID string) {
	key := fmt.Sprintf("%s:%d", msgID, m.width)
	delete(m.thumbCache, key)
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
			return m.moveSelection(-1)
		case key.Matches(msg, theme.DefaultKeyMap.Down):
			return m.moveSelection(1)
		case key.Matches(msg, theme.DefaultKeyMap.PageUp):
			m.viewport.HalfViewUp()
		case key.Matches(msg, theme.DefaultKeyMap.PageDown):
			m.viewport.HalfViewDown()
		case key.Matches(msg, theme.DefaultKeyMap.Top):
			m.selected = 0
			m.viewport.GotoTop()
		case key.Matches(msg, theme.DefaultKeyMap.Bottom):
			if len(m.messages) > 0 {
				m.selected = len(m.messages) - 1
			}
			m.viewport.GotoBottom()
		case key.Matches(msg, theme.DefaultKeyMap.Enter):
			if m.selected >= 0 && m.selected < len(m.messages) {
				sel := m.messages[m.selected]
				if sel.MediaType != "" {
					jid := m.chatJID
					id := sel.ID
					return m, func() tea.Msg {
						return MediaOpenMsg{ChatJID: jid, MessageID: id}
					}
				}
			}
		}
	}

	m.atBottom = m.viewport.AtBottom()
	m.rebuildContent()

	return m, nil
}

// moveSelection moves the selection cursor by delta (-1 up, +1 down) and emits
// a LoadOlderMsg when navigating past the oldest loaded message.
func (m Model) moveSelection(delta int) (Model, tea.Cmd) {
	n := len(m.messages)
	if n == 0 {
		return m, nil
	}

	if m.selected < 0 {
		// No selection yet — anchor at bottom.
		m.selected = n - 1
		m.scrollToSelected()
		m.rebuildContent()
		return m, nil
	}

	next := m.selected + delta
	if next >= n {
		next = n - 1
	}

	var cmd tea.Cmd
	if next < 0 {
		// Tried to go past the top — trigger lazy-load if possible.
		next = 0
		if !m.loading && !m.noMore && m.chatJID != "" {
			m.loading = true
			jid := m.chatJID
			cmd = func() tea.Msg { return LoadOlderMsg{ChatJID: jid} }
		}
	}

	m.selected = next
	m.scrollToSelected()
	m.rebuildContent()
	return m, cmd
}

// scrollToSelected adjusts the viewport so the selected message is visible.
func (m *Model) scrollToSelected() {
	if m.selected < 0 || m.selected >= len(m.lineOffsets) {
		return
	}
	top := m.lineOffsets[m.selected]

	// Determine the bottom line of the selected message.
	bottom := top + 1
	if m.selected+1 < len(m.lineOffsets) {
		bottom = m.lineOffsets[m.selected+1]
	}

	if top < m.viewport.YOffset {
		m.viewport.SetYOffset(top)
	} else if bottom > m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(bottom - m.viewport.Height)
	}
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
	currentLine := 0

	if m.loading {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(theme.ColorTextDim).
			Width(m.width).
			Align(lipgloss.Center).
			Render("Loading older messages..."))
		currentLine++
	}

	m.lineOffsets = make([]int, len(m.messages))

	for i, msg := range m.messages {
		dateStr := msg.Timestamp.Format("2006-01-02")
		if dateStr != lastDate {
			lines = append(lines, renderDateSeparator(msg.Timestamp, m.width))
			lastDate = dateStr
			currentLine++
		}

		m.lineOffsets[i] = currentLine
		isSelected := m.focused && i == m.selected
		rendered := renderMessage(msg, m.width, m.isGroup, isSelected, m.thumbCache)
		lines = append(lines, rendered)
		currentLine += strings.Count(rendered, "\n") + 1
	}

	m.viewport.SetContent(strings.Join(lines, "\n"))
}

// cachedRenderMessage returns the rendered message from cache or renders it fresh.
// Used by PrependMessages to estimate line counts.
func (m *Model) cachedRenderMessage(msg theme.Message) string {
	return renderMessage(msg, m.width, m.isGroup, false, m.thumbCache)
}
