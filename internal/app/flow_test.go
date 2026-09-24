package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/ui/input"
)

// namesWA serves fixed contact and group names.
type namesWA struct {
	fakeWA
	contacts, groups map[string]string
}

func (n namesWA) GetAllContactNames() map[string]string { return n.contacts }
func (n namesWA) GetGroupNames() map[string]string      { return n.groups }

func TestLoadCommandsFeedConversationsAndNames(t *testing.T) {
	s := newTestStore(t)
	dm, group := "a@s.whatsapp.net", "g@g.us"
	for _, c := range []core.Conversation{{JID: dm}, {JID: group, IsGroup: true}} {
		if err := s.UpsertConversation(context.Background(), c); err != nil {
			t.Fatal(err)
		}
	}
	m := NewModel(namesWA{
		contacts: map[string]string{dm: "Alice"},
		groups:   map[string]string{group: "Team"},
	}, s, "test", nil)

	m, _ = update(t, m, m.loadConversationsCmd()())
	m, _ = update(t, m, m.loadContactNamesCmd()())

	if got := conv(m, dm).Name; got != "Alice" {
		t.Errorf("dm name = %q, want Alice", got)
	}
	if got := conv(m, group).Name; got != "Team" {
		t.Errorf("group name = %q, want Team", got)
	}

	// No contacts at all must still apply group names.
	m2 := NewModel(namesWA{groups: map[string]string{group: "Team"}}, s, "test", nil)
	if msg := m2.loadContactNamesCmd()().(contactNamesMsg); msg.Names[group] != "Team" {
		t.Errorf("names = %v, want group name", msg.Names)
	}
}

func TestSendFileAndAudioAddOutgoingMessages(t *testing.T) {
	m, s := newTestModel(t)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})

	// Nothing open: sends are ignored.
	if _, cmd := m.handleSendFile("/tmp/x.pdf"); cmd != nil {
		t.Fatalf("send with no open chat returned a cmd")
	}

	m, _ = m.selectChat(jid)
	m, _ = update(t, m, input.SendFileMsg{Path: "/tmp/report.pdf"})
	if got := conv(m, jid).LastMessage; got != "[file] report.pdf" {
		t.Errorf("preview = %q, want file label", got)
	}
	stored, _ := s.GetAllConversations(context.Background())
	if len(stored) != 1 || stored[0].LastMessage != "[file] report.pdf" {
		t.Errorf("stored = %+v, want outgoing preview persisted", stored)
	}
	m, _ = update(t, m, input.SendAudioMsg{Path: "/tmp/note.ogg"})
	if got := conv(m, jid).LastMessage; got != "[voice] note.ogg" {
		t.Errorf("preview = %q, want voice label", got)
	}
	if got := msgIDs(m.chats.Messages(jid)); len(got) != 2 {
		t.Errorf("cache = %v, want both outgoing messages", got)
	}
}

func TestMessageSendFailedMarksFailed(t *testing.T) {
	m, _ := newTestModel(t)
	m.statusBar.SetWidth(200)
	jid := "a@s.whatsapp.net"
	seedConv(t, &m, core.Conversation{JID: jid})
	m, _ = m.selectChat(jid)
	m, _ = update(t, m, input.SendMsg{Text: "hi"})

	parsed, _ := types.ParseJID(jid)
	m, cmd := update(t, m, core.MessageSendFailed{ChatJID: parsed, MessageID: "genid", Err: errors.New("nope")})

	if got := m.chats.Messages(jid); len(got) != 1 || got[0].Status != "failed" {
		t.Fatalf("cache = %+v, want failed", got)
	}
	if !strings.Contains(m.statusBar.View(), "Send failed: nope") || cmd == nil {
		t.Errorf("status bar = %q, want failure message and clear timer", m.statusBar.View())
	}

	m, _ = update(t, m, core.MessageSent{ChatJID: parsed, MessageID: "genid"})
	if got := m.chats.Messages(jid); got[0].Status != "sent" {
		t.Errorf("status = %q, want sent", got[0].Status)
	}
}

func TestConnectedLayoutAndView(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = update(t, m, core.Connected{})
	if m.state != StateChat {
		t.Fatalf("state = %v, want StateChat", m.state)
	}
	if m.View() == "" {
		t.Errorf("chat View() is empty")
	}

	m, _ = update(t, m, core.LoginFailed{Err: errors.New("bad qr")})
	if m.state != StateError || !strings.Contains(m.View(), "bad qr") {
		t.Errorf("error view = %q, want login error", m.View())
	}
}

func TestTabCyclesFocus(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = update(t, m, core.Connected{})

	want := []Panel{PanelMessages, PanelInput, PanelChatList}
	for _, p := range want {
		m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if m.focus != p {
			t.Fatalf("focus = %v, want %v", m.focus, p)
		}
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != PanelInput {
		t.Errorf("shift+tab focus = %v, want PanelInput", m.focus)
	}
	m, _ = update(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.focus != PanelChatList {
		t.Errorf("esc focus = %v, want PanelChatList", m.focus)
	}
}

func TestIsTypingKey(t *testing.T) {
	for key, want := range map[string]bool{
		"a": true, " ": true, "enter": false, "ctrl+x": false, "alt+b": false, "tab": false, "f5": false,
	} {
		if got := isTypingKey(key); got != want {
			t.Errorf("isTypingKey(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestDisconnectedInChatSchedulesReconnect(t *testing.T) {
	m, _ := newTestModel(t)
	m, _ = update(t, m, core.Connected{})
	m, cmd := update(t, m, core.Disconnected{})
	if m.state != StateChat || m.reconnectAttempts != 1 || cmd == nil {
		t.Fatalf("state=%v attempts=%d cmd=%v, want reconnect scheduled", m.state, m.reconnectAttempts, cmd != nil)
	}
}
