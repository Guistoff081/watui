package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/core"
)

func space() tea.KeyMsg         { return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}} }
func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func leaderModel(t *testing.T) Model {
	t.Helper()
	m, _ := newTestModel(t)
	m = send(t, m, core.Connected{})
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	convs := []core.Conversation{
		{JID: "a@s.whatsapp.net", Name: "Ana", LastMsgTime: time.Unix(2, 0)},
		{JID: "b@s.whatsapp.net", Name: "Bia", LastMsgTime: time.Unix(1, 0)},
	}
	return send(t, m, conversationsLoadedMsg{Conversations: convs})
}

func frameWidth(v string) int {
	w := 0
	for _, l := range strings.Split(v, "\n") {
		w = max(w, lipgloss.Width(l))
	}
	return w
}

func TestSpaceOpensWhichKeyInNormalMode(t *testing.T) {
	m := leaderModel(t)
	plainW := frameWidth(m.View())
	m = send(t, m, space())
	if m.leader == nil {
		t.Fatal("Space in chat list did not start the leader")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "+attach") || !strings.Contains(v, "All keybindings") {
		t.Errorf("which-key popup not shown:\n%s", v)
	}
	if got := strings.Count(m.View(), "\n") + 1; got != 30 {
		t.Errorf("frame is %d rows with popup, want 30", got)
	}
	if got := frameWidth(m.View()); got != plainW {
		t.Errorf("frame is %d columns with popup, %d without; the popup must not widen it", got, plainW)
	}
}

func TestSpaceTypesInInput(t *testing.T) {
	m := leaderModel(t)
	m = open(t, m, "a@s.whatsapp.net")
	m = send(t, m, runeKey('i'))
	m = send(t, m, runeKey('o'))
	m = send(t, m, space())
	m = send(t, m, runeKey('i'))
	if m.leader != nil || m.input.Value() != "o i" {
		t.Errorf("input = %q, leader = %v; want Space typed", m.input.Value(), m.leader)
	}
}

func TestUnknownLeaderKeyIsSwallowed(t *testing.T) {
	m := leaderModel(t)
	before := m.chatList.SelectedJID()
	m = send(t, m, space())
	m = send(t, m, runeKey('j'))
	if m.leader != nil {
		t.Error("unknown key did not cancel the leader")
	}
	if got := m.chatList.SelectedJID(); got != before {
		t.Errorf("j after Space moved the list (%s → %s)", before, got)
	}
	m = send(t, m, space())
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.leader != nil {
		t.Error("Esc did not cancel the leader")
	}
}

func TestLeaderAttachFileFocusesInputPrompt(t *testing.T) {
	m := leaderModel(t)
	m = open(t, m, "a@s.whatsapp.net")
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // back to chat list (normal mode)
	for _, k := range []tea.KeyMsg{space(), runeKey('a'), runeKey('f')} {
		m = send(t, m, k)
	}
	if m.focus != PanelInput || !m.input.InPathPrompt() {
		t.Errorf("focus = %v, prompt = %v; want input in file prompt", m.focus, m.input.InPathPrompt())
	}
}

func TestLeaderAttachWithoutChatReports(t *testing.T) {
	m := leaderModel(t)
	m.statusBar.SetWidth(200)
	for _, k := range []tea.KeyMsg{space(), runeKey('a'), runeKey('f')} {
		m = send(t, m, k)
	}
	if !strings.Contains(m.statusBar.View(), "Open a chat first") || m.focus == PanelInput {
		t.Errorf("status = %q focus = %v", m.statusBar.View(), m.focus)
	}
}

func TestLeaderHelpOverlay(t *testing.T) {
	m := leaderModel(t)
	m = send(t, m, space())
	m = send(t, m, runeKey('?'))
	v := ansi.Strip(m.View())
	if m.overlay == nil || !strings.Contains(v, "a f") || !strings.Contains(v, "Tab") {
		t.Fatalf("help overlay missing:\n%s", v)
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != nil {
		t.Error("Esc did not close help")
	}
}

func TestCtrlCQuitsWithLeaderOrOverlayOpen(t *testing.T) {
	m := leaderModel(t)
	m = send(t, m, space())
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c with leader open did not quit")
	}
	m = send(t, m, runeKey('?'))
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c with help open did not quit")
	}
}

func TestOverlayOnTinyTerminalKeepsFrameHeight(t *testing.T) {
	m := leaderModel(t)
	m = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})
	m = send(t, m, space())
	m = send(t, m, runeKey('?'))
	if got := strings.Count(m.View(), "\n") + 1; got != 12 {
		t.Errorf("frame %d rows on a 40x12 terminal with help open, want 12", got)
	}
}

func TestPlaceOverlayKeepsBodySize(t *testing.T) {
	const bodyW, bodyH = 30, 6
	cell := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Background(lipgloss.Color("4"))
	var rows []string
	for i := 0; i < bodyH; i++ {
		rows = append(rows, cell.Render(strings.Repeat("ab", 5))+"界界界"+cell.Render(strings.Repeat("x", 14)))
	}
	body := strings.Join(rows, "\n")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Render("hello\nworld")
	for _, centered := range []bool{false, true} {
		out := placeOverlay(body, box, bodyW, bodyH, centered)
		lines := strings.Split(out, "\n")
		if len(lines) != bodyH {
			t.Fatalf("centered=%v: %d lines, want %d", centered, len(lines), bodyH)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != bodyW {
				t.Errorf("centered=%v line %d width %d, want %d: %q", centered, i, w, bodyW, ansi.Strip(l))
			}
		}
		if !strings.Contains(ansi.Strip(out), "hello") {
			t.Errorf("centered=%v: box not drawn:\n%s", centered, ansi.Strip(out))
		}
	}
	// A box taller than the body is clipped, never growing it.
	tall := strings.Repeat("row\n", 20) + "row"
	if got := strings.Count(placeOverlay(body, tall, bodyW, bodyH, true), "\n") + 1; got != bodyH {
		t.Errorf("tall box: %d lines, want %d", got, bodyH)
	}
}
