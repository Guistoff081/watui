package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/ui/chatview"
	"github.com/watui/watui/internal/ui/input"
)

// delays returns the timers cmd would start, without delivering them.
func delays(t *testing.T, cmd tea.Cmd) []delayedMsg {
	t.Helper()
	var out []delayedMsg
	for _, msg := range collect(t, cmd) {
		if d, ok := msg.(delayedMsg); ok {
			out = append(out, d)
		}
	}
	return out
}

func TestInitConnects(t *testing.T) {
	m, _, wa := newFakeStoreModel(t)
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init() = nil, want auth and connect commands")
	}
	if wa.connects != 1 {
		t.Errorf("connects = %d, want 1", wa.connects)
	}
}

func TestQRFlow(t *testing.T) {
	m, _, wa := newFakeStoreModel(t)
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	m = send(t, m, core.QRCode{Code: "2@abc"})
	if m.state != StateAuth {
		t.Fatalf("state = %v, want StateAuth", m.state)
	}

	m = send(t, m, core.QRTimeout{})
	if wa.connects != 1 {
		t.Errorf("connects after QR timeout = %d, want 1 (new codes requested)", wa.connects)
	}
	if !strings.Contains(m.View(), "expired") {
		t.Errorf("auth view = %q, want expiry notice", m.View())
	}

	m = send(t, m, core.LoginSuccess{JID: types.NewJID("1", types.DefaultUserServer)})
	if m.connectedJID == "" || !strings.Contains(m.View(), "Finishing login") {
		t.Errorf("jid=%q view=%q, want login in progress", m.connectedJID, m.View())
	}
}

func TestFatalStatesRenderErrorView(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.Msg
		want string
	}{
		{"client outdated", core.ClientOutdated{}, "client version outdated"},
		{"disconnect before chat", core.Disconnected{}, "disconnected from WhatsApp"},
		{"disconnect error before chat", core.Disconnected{Err: errors.New("stream end")}, "stream end"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, _, _ := newFakeStoreModel(t)
			m = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
			m = send(t, m, c.msg)
			if m.state != StateError {
				t.Fatalf("state = %v, want StateError", m.state)
			}
			if view := m.View(); !strings.Contains(view, "Connection error") || !strings.Contains(view, c.want) {
				t.Errorf("view = %q, want error screen with %q", view, c.want)
			}
		})
	}

	m, _, _ := newFakeStoreModel(t)
	m.state = StateError
	if !strings.Contains(m.View(), "unknown error") {
		t.Errorf("view without lastErr = %q, want unknown error", m.View())
	}
}

func TestReconnectBackoff(t *testing.T) {
	m, _, wa := newFakeStoreModel(t)
	m.statusBar.SetWidth(200)
	m = send(t, m, core.Connected{})

	for attempt := 1; attempt <= 5; attempt++ {
		var cmd tea.Cmd
		m, cmd = update(t, m, core.Disconnected{})
		ds := delays(t, cmd)
		want := time.Duration(attempt) * 3 * time.Second
		if len(ds) != 1 || ds[0].d != want || ds[0].msg != (reconnectMsg{}) {
			t.Fatalf("attempt %d timers = %+v, want reconnect after %v", attempt, ds, want)
		}
		if !strings.Contains(m.statusBar.View(), "Reconnecting") {
			t.Errorf("status = %q, want reconnecting", m.statusBar.View())
		}
		m = send(t, m, reconnectMsg{})
	}
	if wa.connects != 5 {
		t.Errorf("connects = %d, want 5", wa.connects)
	}

	// Attempts exhausted: no more timers, but the chat UI stays up.
	m, cmd := update(t, m, core.Disconnected{})
	if ds := delays(t, cmd); len(ds) != 0 || m.state != StateChat {
		t.Errorf("after 5 attempts timers=%+v state=%v, want none and StateChat", ds, m.state)
	}

	// A successful reconnect resets the budget.
	m = send(t, m, core.Connected{})
	if m.reconnectAttempts != 0 {
		t.Errorf("reconnectAttempts = %d, want reset", m.reconnectAttempts)
	}
}

func TestTypingIndicatorForOpenChat(t *testing.T) {
	m, _, jid := chatModel(t)
	parsed, _ := types.ParseJID(jid)
	sender := types.NewJID("555", types.DefaultUserServer)

	m = send(t, m, core.Typing{ChatJID: parsed, Sender: sender, IsTyping: true})
	if !strings.Contains(m.titleBar.View(), "555") {
		t.Errorf("title = %q, want typing sender", m.titleBar.View())
	}
	m = send(t, m, core.Typing{ChatJID: parsed, Sender: sender})
	if strings.Contains(m.titleBar.View(), "555") {
		t.Errorf("title = %q, want typing cleared", m.titleBar.View())
	}
}

func TestStatusMessagesClear(t *testing.T) {
	m, _, _ := chatModel(t)
	m.statusBar.SetWidth(200)

	m, cmd := update(t, m, input.PickerErrorMsg{Err: "no such file"})
	if !strings.Contains(m.statusBar.View(), "no such file") {
		t.Fatalf("status = %q, want picker error", m.statusBar.View())
	}
	ds := delays(t, cmd)
	if len(ds) != 1 || ds[0].d != statusTimeout {
		t.Fatalf("timers = %+v, want status timeout", ds)
	}
	m = send(t, m, ds[0].msg)
	if strings.Contains(m.statusBar.View(), "no such file") {
		t.Errorf("status = %q, want cleared", m.statusBar.View())
	}
}

func TestHistorySyncCompleteReloadsNames(t *testing.T) {
	s := newTestStore(t)
	jid := "a@s.whatsapp.net"
	m := testModel(namesWA{contacts: map[string]string{jid: "Alice"}}, s)
	m.chats.Load([]core.Conversation{{JID: jid, Name: jid}})

	m = send(t, m, core.HistorySyncComplete{})

	if got := conv(m, jid).Name; got != "Alice" {
		t.Errorf("Name = %q, want Alice", got)
	}
}

func TestLoadOlderRequestQueriesStore(t *testing.T) {
	m, fs, _ := newFakeStoreModel(t)
	jid := "a@s.whatsapp.net"
	m.chats.Load([]core.Conversation{{JID: jid}})
	m.chats.AddHistory(jid, []core.Message{{ID: "x", ChatJID: jid, Timestamp: time.Unix(1, 0)}}, "")

	send(t, m, chatview.LoadOlderMsg{ChatJID: jid})

	if got := fs.Calls(); !reflect.DeepEqual(got, []string{"GetMessagesBefore " + jid}) {
		t.Errorf("store calls = %v, want one GetMessagesBefore", got)
	}
	if cmd := m.loadOlderMessagesCmd("empty@s.whatsapp.net"); cmd != nil {
		t.Error("loadOlderMessagesCmd(no cache) != nil")
	}
}

func TestSleepThenDeliversMessage(t *testing.T) {
	if got := sleepThen(time.Millisecond, clearStatusMsg{})(); got != (clearStatusMsg{}) {
		t.Errorf("sleepThen() = %#v, want clearStatusMsg", got)
	}
}

// The frame must be exactly the terminal height: when a panel rendered taller
// (multi-line previews made the chat list 52 rows in a 45-row terminal) Bubble
// Tea dropped lines, hiding the title/status bars and the message column.
func TestViewFillsExactlyTerminalHeight(t *testing.T) {
	m, s := newTestModel(t)
	m = send(t, m, core.Connected{})
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})

	var convs []core.Conversation
	for i := 0; i < 20; i++ {
		convs = append(convs, core.Conversation{
			JID:         fmt.Sprintf("55119%08d@s.whatsapp.net", i),
			Name:        strings.Repeat("Nome comprido ", 5),
			LastMessage: "linha 1\n\nlinha 2\n📌 linha 3\n" + strings.Repeat("x", 200),
			LastMsgTime: time.Unix(int64(1000+i), 0),
		})
	}
	for _, c := range convs {
		_ = s.UpsertConversation(context.Background(), c)
	}
	m = send(t, m, conversationsLoadedMsg{Conversations: convs})
	m = open(t, m, convs[0].JID)

	if got := strings.Count(m.View(), "\n") + 1; got != 30 {
		t.Fatalf("View() is %d rows, want exactly the terminal height 30", got)
	}
}
