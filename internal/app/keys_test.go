package app

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/ui/input"
)

func TestCtrlCDisconnectsAndQuits(t *testing.T) {
	m, _, wa := newFakeStoreModel(t)
	_, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !wa.disconnected {
		t.Error("Disconnect not called")
	}
	if cmd == nil || cmd() != tea.Quit() {
		t.Error("cmd != tea.Quit")
	}
}

func TestKeysOutsideChatState(t *testing.T) {
	m, _, _ := newFakeStoreModel(t)
	// Auth: keys go to the QR screen and must not move focus.
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != PanelChatList {
		t.Errorf("focus changed in auth state: %v", m.focus)
	}
	m.state = StateError
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyTab}); cmd != nil {
		t.Error("key in error state returned a cmd")
	}
}

// chatModel returns a connected model with jid open.
func chatModel(t *testing.T) (Model, *recordingWA, string) {
	t.Helper()
	m, _, wa := newFakeStoreModel(t)
	m = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = send(t, m, core.Connected{})
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid}})
	m = open(t, m, jid)
	return m, wa, jid
}

func TestFocusKeys(t *testing.T) {
	m, _, _ := chatModel(t)
	if m.focus != PanelMessages {
		t.Fatalf("focus after open = %v, want PanelMessages", m.focus)
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.focus != PanelChatList {
		t.Errorf("esc from messages: focus = %v, want PanelChatList", m.focus)
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.focus != PanelChatList {
		t.Errorf("esc from chat list: focus = %v, want PanelChatList", m.focus)
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if m.focus != PanelInput {
		t.Errorf("i: focus = %v, want PanelInput", m.focus)
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != PanelMessages {
		t.Errorf("shift+tab from input: focus = %v, want PanelMessages", m.focus)
	}
	// Other keys are routed to the focused panel without changing focus.
	m = send(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = send(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = send(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.focus != PanelChatList {
		t.Errorf("focus = %v, want PanelChatList", m.focus)
	}
}

func TestTypingPresence(t *testing.T) {
	m, wa, _ := chatModel(t)
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})

	m, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	ds := delays(t, cmd)
	if len(ds) != 1 || ds[0].d != typingIdleTimeout {
		t.Fatalf("timers = %+v, want typing idle timer", ds)
	}
	firstStop := ds[0].msg
	m, cmd = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	secondStop := delays(t, cmd)[0].msg
	if !reflect.DeepEqual(wa.presence, []bool{true}) {
		t.Fatalf("presence = %v, want a single composing", wa.presence)
	}

	// A superseded idle timer does nothing; the latest one pauses.
	m = send(t, m, firstStop)
	if len(wa.presence) != 1 {
		t.Fatalf("presence after stale timer = %v, want unchanged", wa.presence)
	}
	m = send(t, m, secondStop)
	if !reflect.DeepEqual(wa.presence, []bool{true, false}) {
		t.Fatalf("presence = %v, want composing then paused", wa.presence)
	}

	// Leaving the input while composing pauses immediately.
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !reflect.DeepEqual(wa.presence, []bool{true, false, true, false}) || m.focus != PanelChatList {
		t.Errorf("presence = %v focus = %v, want paused on esc", wa.presence, m.focus)
	}

	// Sending while composing pauses and sends.
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = send(t, m, input.SendMsg{Text: "yo"})
	if got := wa.presence[len(wa.presence)-1]; got || m.isTyping {
		t.Errorf("presence = %v, want paused after send", wa.presence)
	}
	if !reflect.DeepEqual(wa.texts, []string{"yo"}) {
		t.Errorf("texts = %v, want [yo]", wa.texts)
	}
}

func TestSendAudioWhileTypingPausesPresence(t *testing.T) {
	m, wa, _ := chatModel(t)
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m = send(t, m, input.SendAudioMsg{Path: "/tmp/a.ogg"})
	m = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	m = send(t, m, input.SendFileMsg{Path: "/tmp/a.pdf"})
	if !reflect.DeepEqual(wa.presence, []bool{true, false, true, false}) {
		t.Errorf("presence = %v, want paused by each send", wa.presence)
	}
}
