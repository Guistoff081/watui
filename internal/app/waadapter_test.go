package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/whatsapp"
)

// fakeSyncWA is a scriptable syncWAClient that records the arguments of the
// last call to each method.
type fakeSyncWA struct {
	connectErr error

	sent    core.MessageSent
	sendErr error
	lastOp  string
	lastArg string

	presenceErr  error
	presenceJID  types.JID
	presenceComp bool

	markReadErr error
	markChat    types.JID
	markSender  types.JID
	markIDs     []string

	contacts    map[string]string
	contactsErr error
	groups      map[string]string
	groupsErr   error

	alt    string
	altArg string

	dlPath string
	dlErr  error
	dlMsg  core.Message

	openErr  error
	openPath string

	verified    map[string]string
	verifiedErr error
	verifiedArg []string

	historyErr    error
	historyAnchor string
	historyCount  int
	openType      string
	disconned     bool
}

func (f *fakeSyncWA) Connect(context.Context) error { return f.connectErr }
func (f *fakeSyncWA) Disconnect()                   { f.disconned = true }
func (f *fakeSyncWA) GenerateMessageID() string     { return "gen-1" }

func (f *fakeSyncWA) send(op string, arg string) (core.MessageSent, error) {
	f.lastOp, f.lastArg = op, arg
	return f.sent, f.sendErr
}

func (f *fakeSyncWA) SendText(_ context.Context, _ types.JID, _, text string) (core.MessageSent, error) {
	return f.send("text", text)
}

func (f *fakeSyncWA) SendFile(_ context.Context, _ types.JID, _, path string) (core.MessageSent, error) {
	return f.send("file", path)
}

func (f *fakeSyncWA) SendAudio(_ context.Context, _ types.JID, _, path string) (core.MessageSent, error) {
	return f.send("audio", path)
}

func (f *fakeSyncWA) SendChatPresence(_ context.Context, jid types.JID, composing bool) error {
	f.presenceJID, f.presenceComp = jid, composing
	return f.presenceErr
}

func (f *fakeSyncWA) MarkRead(_ context.Context, chat, sender types.JID, ids []string) error {
	f.markChat, f.markSender, f.markIDs = chat, sender, ids
	return f.markReadErr
}

func (f *fakeSyncWA) GetAllContactNames(context.Context) (map[string]string, error) {
	return f.contacts, f.contactsErr
}

func (f *fakeSyncWA) GetGroupNames(context.Context) (map[string]string, error) {
	return f.groups, f.groupsErr
}

func (f *fakeSyncWA) AltChatJID(_ context.Context, jid string) string {
	f.altArg = jid
	return f.alt
}

func (f *fakeSyncWA) DownloadMedia(_ context.Context, msg core.Message) (string, error) {
	f.dlMsg = msg
	return f.dlPath, f.dlErr
}

func (f *fakeSyncWA) OpenMedia(path, mediaType string) error {
	f.openPath, f.openType = path, mediaType
	return f.openErr
}

func (f *fakeSyncWA) GetVerifiedNames(_ context.Context, jids []string) (map[string]string, error) {
	f.verifiedArg = jids
	return f.verified, f.verifiedErr
}

func (f *fakeSyncWA) RequestOlderHistory(_ context.Context, oldest core.Message, count int) error {
	f.historyAnchor, f.historyCount = oldest.ID, count
	return f.historyErr
}

var adapterChat = types.NewJID("5511999999999", types.DefaultUserServer)

func TestAdapterConnect(t *testing.T) {
	loginErr := errors.New("pairing rejected")
	connErr := &whatsapp.ConnectError{Err: errors.New("dial failed")}

	t.Run("success yields nil", func(t *testing.T) {
		a := newWAAdapter(&fakeSyncWA{})
		if msg := a.Connect()(); msg != nil {
			t.Fatalf("Connect() msg = %#v, want nil", msg)
		}
	})

	t.Run("socket failure yields connectFailedMsg", func(t *testing.T) {
		a := newWAAdapter(&fakeSyncWA{connectErr: connErr})
		msg := a.Connect()()
		cf, ok := msg.(connectFailedMsg)
		if !ok {
			t.Fatalf("Connect() msg = %T, want connectFailedMsg", msg)
		}
		if cf.Err.Error() != "connect: dial failed" {
			t.Errorf("error = %q, want %q", cf.Err.Error(), "connect: dial failed")
		}
	})

	t.Run("other failure yields LoginFailed", func(t *testing.T) {
		a := newWAAdapter(&fakeSyncWA{connectErr: loginErr})
		msg := a.Connect()()
		lf, ok := msg.(core.LoginFailed)
		if !ok {
			t.Fatalf("Connect() msg = %T, want core.LoginFailed", msg)
		}
		if !errors.Is(lf.Err, loginErr) {
			t.Errorf("LoginFailed.Err = %v, want %v", lf.Err, loginErr)
		}
	})
}

func TestAdapterSend(t *testing.T) {
	sent := core.MessageSent{ChatJID: adapterChat, MessageID: "srv-1", Timestamp: time.Unix(1700000000, 0)}
	sendErr := errors.New("boom")

	methods := []struct {
		op  string
		cmd func(a *waAdapter) tea.Cmd
	}{
		{"text", func(a *waAdapter) tea.Cmd { return a.SendTextMessage(adapterChat, "id-1", "text") }},
		{"file", func(a *waAdapter) tea.Cmd { return a.SendFileMessage(adapterChat, "id-1", "file") }},
		{"audio", func(a *waAdapter) tea.Cmd { return a.SendAudioMessage(adapterChat, "id-1", "audio") }},
	}

	for _, m := range methods {
		t.Run(m.op+" success", func(t *testing.T) {
			f := &fakeSyncWA{sent: sent}
			got := m.cmd(newWAAdapter(f))()
			if got != sent {
				t.Errorf("msg = %#v, want %#v", got, sent)
			}
			if f.lastOp != m.op || f.lastArg != m.op {
				t.Errorf("called %s(%q), want %s(%q)", f.lastOp, f.lastArg, m.op, m.op)
			}
		})
		t.Run(m.op+" failure", func(t *testing.T) {
			f := &fakeSyncWA{sendErr: sendErr}
			got := m.cmd(newWAAdapter(f))()
			want := core.MessageSendFailed{ChatJID: adapterChat, MessageID: "id-1", Err: sendErr}
			if got != want {
				t.Errorf("msg = %#v, want %#v", got, want)
			}
		})
	}
}

func TestAdapterDownloadMedia(t *testing.T) {
	msg := core.Message{ID: "m1", ChatJID: adapterChat.String()}

	t.Run("success", func(t *testing.T) {
		f := &fakeSyncWA{dlPath: "/cache/m1.jpg"}
		got := newWAAdapter(f).DownloadMedia(msg)()
		want := core.MediaDownloaded{ChatJID: msg.ChatJID, MessageID: "m1", Path: "/cache/m1.jpg"}
		if got != want {
			t.Errorf("msg = %#v, want %#v", got, want)
		}
		if f.dlMsg.ID != "m1" {
			t.Errorf("downloaded %q, want m1", f.dlMsg.ID)
		}
	})

	t.Run("failure", func(t *testing.T) {
		dlErr := errors.New("download: 404")
		got := newWAAdapter(&fakeSyncWA{dlErr: dlErr}).DownloadMedia(msg)()
		want := core.MediaDownloadFailed{ChatJID: msg.ChatJID, MessageID: "m1", Err: dlErr}
		if got != want {
			t.Errorf("msg = %#v, want %#v", got, want)
		}
	})
}

func TestAdapterOpenMedia(t *testing.T) {
	f := &fakeSyncWA{}
	if got := newWAAdapter(f).OpenMedia("/cache/a.ogg", "voice")(); got != nil {
		t.Errorf("OpenMedia success msg = %#v, want nil", got)
	}
	if f.openPath != "/cache/a.ogg" || f.openType != "voice" {
		t.Errorf("OpenMedia called with (%q, %q)", f.openPath, f.openType)
	}

	fail := errors.New("no viewer for image/webp")
	got := newWAAdapter(&fakeSyncWA{openErr: fail}).OpenMedia("/cache/a.webp", "sticker")()
	if msg, ok := got.(mediaOpenFailedMsg); !ok || !errors.Is(msg.Err, fail) {
		t.Errorf("OpenMedia failure msg = %#v, want mediaOpenFailedMsg wrapping the error", got)
	}
}

func TestAdapterNames(t *testing.T) {
	names := map[string]string{"a@s.whatsapp.net": "Ana"}
	fail := errors.New("store closed")

	f := &fakeSyncWA{contacts: names, groups: names}
	a := newWAAdapter(f)
	if got := a.GetAllContactNames(); !reflect.DeepEqual(got, names) {
		t.Errorf("GetAllContactNames() = %v, want %v", got, names)
	}
	if got := a.GetGroupNames(); !reflect.DeepEqual(got, names) {
		t.Errorf("GetGroupNames() = %v, want %v", got, names)
	}

	f = &fakeSyncWA{contacts: names, contactsErr: fail, groups: names, groupsErr: fail}
	a = newWAAdapter(f)
	if got := a.GetAllContactNames(); got != nil {
		t.Errorf("GetAllContactNames() on error = %v, want nil", got)
	}
	if got := a.GetGroupNames(); got != nil {
		t.Errorf("GetGroupNames() on error = %v, want nil", got)
	}
}

func TestAdapterPassThrough(t *testing.T) {
	f := &fakeSyncWA{alt: "123@lid", presenceErr: errors.New("x"), markReadErr: errors.New("y")}
	a := newWAAdapter(f)

	if got := a.GenerateMessageID(); got != "gen-1" {
		t.Errorf("GenerateMessageID() = %q", got)
	}
	if got := a.AltChatJID(adapterChat.String()); got != "123@lid" || f.altArg != adapterChat.String() {
		t.Errorf("AltChatJID() = %q (arg %q)", got, f.altArg)
	}

	// Errors from best-effort calls are swallowed; the arguments still arrive.
	a.SendChatPresence(adapterChat, true)
	if f.presenceJID != adapterChat || !f.presenceComp {
		t.Errorf("SendChatPresence called with (%v, %v)", f.presenceJID, f.presenceComp)
	}
	sender := types.NewJID("5511888888888", types.DefaultUserServer)
	a.MarkRead(adapterChat, sender, []string{"m1", "m2"})
	if f.markChat != adapterChat || f.markSender != sender || !reflect.DeepEqual(f.markIDs, []string{"m1", "m2"}) {
		t.Errorf("MarkRead called with (%v, %v, %v)", f.markChat, f.markSender, f.markIDs)
	}

	a.Disconnect()
	if !f.disconned {
		t.Error("Disconnect() not forwarded")
	}
}

func TestAdapterRequestOlderHistory(t *testing.T) {
	f := &fakeSyncWA{}
	if got := newWAAdapter(f).RequestOlderHistory(core.Message{ID: "OLD"})(); got != nil {
		t.Errorf("success msg = %#v, want nil", got)
	}
	if f.historyAnchor != "OLD" || f.historyCount != olderHistoryPage {
		t.Errorf("request = (%q, %d), want (OLD, %d)", f.historyAnchor, f.historyCount, olderHistoryPage)
	}
	fail := errors.New("not connected")
	got := newWAAdapter(&fakeSyncWA{historyErr: fail}).RequestOlderHistory(core.Message{ID: "OLD"})()
	if msg, ok := got.(historyRequestFailedMsg); !ok || !errors.Is(msg.Err, fail) {
		t.Errorf("failure msg = %#v, want historyRequestFailedMsg", got)
	}
}

func TestAdapterGetVerifiedNames(t *testing.T) {
	f := &fakeSyncWA{verified: map[string]string{"b@s.whatsapp.net": "Jeitto"}}
	got := newWAAdapter(f).GetVerifiedNames([]string{"b@s.whatsapp.net"})
	if got["b@s.whatsapp.net"] != "Jeitto" || !reflect.DeepEqual(f.verifiedArg, []string{"b@s.whatsapp.net"}) {
		t.Errorf("GetVerifiedNames() = %v (asked %v)", got, f.verifiedArg)
	}
	// On error the names found so far are kept (usync is batched).
	f = &fakeSyncWA{verified: map[string]string{"b@s.whatsapp.net": "Jeitto"}, verifiedErr: errors.New("offline")}
	if got := newWAAdapter(f).GetVerifiedNames([]string{"b@s.whatsapp.net"}); got["b@s.whatsapp.net"] != "Jeitto" {
		t.Errorf("partial result dropped on error: %v", got)
	}
}
