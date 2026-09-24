package whatsapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.mau.fi/whatsmeow"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/watui/watui/internal/core"
)

// newOfflineClient builds a real Client on a temporary whatsmeow store. It is
// never connected, so every network call fails fast with a "not connected" or
// "not logged in" error, while store-backed lookups work for real.
func newOfflineClient(t *testing.T) (*Client, *eventRecorder) {
	t.Helper()
	dir := t.TempDir()
	c, err := NewClient(filepath.Join(dir, "wa.db"), dir, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.store.Close() })
	rec := &eventRecorder{}
	c.SetEventHandler(rec.add)
	return c, rec
}

// newStoreClient is newOfflineClient with a device JID saved, which whatsmeow
// requires before its per-device stores (contacts, LID map) are usable. The
// account identity is zero-filled to the sizes the schema checks.
func newStoreClient(t *testing.T) (*Client, *eventRecorder) {
	t.Helper()
	c, rec := newOfflineClient(t)
	own := types.NewADJID("5511000000000", 0, 1)
	c.wm.Store.ID = &own
	c.wm.Store.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{},
		AccountSignature:    make([]byte, 64),
		AccountSignatureKey: make([]byte, 32),
		DeviceSignature:     make([]byte, 64),
	}
	if err := c.store.PutDevice(context.Background(), c.wm.Store); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}
	return c, rec
}

type eventRecorder struct {
	mu     sync.Mutex
	events []core.Event
}

func (r *eventRecorder) add(e core.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *eventRecorder) take() []core.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	evs := r.events
	r.events = nil
	return evs
}

var (
	testPN  = types.NewJID("5511999999999", types.DefaultUserServer)
	testLID = types.NewJID("123456789", types.HiddenUserServer)
	testGrp = types.NewJID("120363000000000000", types.GroupServer)
)

func TestSendWithoutHandlerIsNoop(t *testing.T) {
	(&Client{}).send(core.QRTimeout{})
}

func TestConnectError(t *testing.T) {
	inner := errors.New("dial tcp: refused")
	err := error(&ConnectError{Err: inner})
	if err.Error() != "connect: dial tcp: refused" {
		t.Errorf("Error() = %q", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Error("ConnectError does not unwrap to its cause")
	}
}

func TestWaitQR(t *testing.T) {
	qrErr := errors.New("bad scan")
	tests := []struct {
		name    string
		items   []whatsmeow.QRChannelItem
		wantErr string
		want    []core.Event
	}{
		{
			name: "codes then success",
			items: []whatsmeow.QRChannelItem{
				{Event: "code", Code: "A"}, {Event: "code", Code: "B"}, whatsmeow.QRChannelSuccess,
				{Event: "code", Code: "never read"},
			},
			want: []core.Event{core.QRCode{Code: "A"}, core.QRCode{Code: "B"}},
		},
		{
			name:  "timeout then channel closed",
			items: []whatsmeow.QRChannelItem{{Event: "code", Code: "A"}, whatsmeow.QRChannelTimeout},
			want:  []core.Event{core.QRCode{Code: "A"}, core.QRTimeout{}},
		},
		{
			name:    "error with cause",
			items:   []whatsmeow.QRChannelItem{{Event: "error", Error: qrErr}},
			wantErr: "bad scan",
		},
		{
			name:    "error without cause",
			items:   []whatsmeow.QRChannelItem{{Event: "error"}},
			wantErr: "QR login failed",
		},
		{
			name:    "other terminal event",
			items:   []whatsmeow.QRChannelItem{whatsmeow.QRChannelClientOutdated},
			wantErr: "QR pairing failed: err-client-outdated",
		},
		{
			name:    "other terminal event with cause",
			items:   []whatsmeow.QRChannelItem{{Event: "err-unexpected-state", Error: qrErr}},
			wantErr: "QR pairing failed: err-unexpected-state: bad scan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan whatsmeow.QRChannelItem, len(tt.items))
			for _, it := range tt.items {
				ch <- it
			}
			close(ch)

			rec := &eventRecorder{}
			c := &Client{onEvent: rec.add}
			err := c.waitQR(ch)

			if tt.wantErr == "" && err != nil {
				t.Fatalf("waitQR() error = %v, want nil", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("waitQR() error = %v, want %q", err, tt.wantErr)
			}
			if got := rec.take(); len(got) != len(tt.want) {
				t.Errorf("events = %#v, want %#v", got, tt.want)
			} else {
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("event[%d] = %#v, want %#v", i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestOfflineNetworkCallsFail(t *testing.T) {
	c, _ := newOfflineClient(t)
	ctx := context.Background()

	if c.JID() != nil {
		t.Errorf("JID() = %v, want nil before login", c.JID())
	}
	if c.WMClient() == nil {
		t.Error("WMClient() = nil")
	}
	if c.GenerateMessageID() == "" {
		t.Error("GenerateMessageID() is empty")
	}
	if _, err := c.SendText(ctx, testPN, "id1", "hi"); err == nil {
		t.Error("SendText() offline succeeded")
	}
	if err := c.MarkRead(ctx, testPN, testPN, []string{"id1"}); err == nil {
		t.Error("MarkRead() offline succeeded")
	}
	for _, v := range []bool{true, false} {
		if err := c.SendPresence(ctx, v); err == nil {
			t.Errorf("SendPresence(%v) offline succeeded", v)
		}
		if err := c.SendChatPresence(ctx, testPN, v); err == nil {
			t.Errorf("SendChatPresence(%v) offline succeeded", v)
		}
	}
	if names, err := c.GetGroupNames(ctx); err == nil || names != nil {
		t.Errorf("GetGroupNames() offline = %v, %v", names, err)
	}
	c.Disconnect()
}

func TestSendFileAndAudioErrors(t *testing.T) {
	c, _ := newStoreClient(t)
	ctx := context.Background()
	dir := t.TempDir()

	small := filepath.Join(dir, "note.ogg")
	if err := os.WriteFile(small, []byte("OggS"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxUploadSize + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	for name, send := range map[string]func(context.Context, types.JID, string, string) (core.MessageSent, error){
		"SendFile":  c.SendFile,
		"SendAudio": c.SendAudio,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := send(ctx, testPN, "id", filepath.Join(dir, "missing")); err == nil || !strings.HasPrefix(err.Error(), "read file: ") {
				t.Errorf("missing file error = %v", err)
			}
			if _, err := send(ctx, testPN, "id", big); !errors.Is(err, ErrFileTooLarge) {
				t.Errorf("big file error = %v, want ErrFileTooLarge", err)
			}
			if _, err := send(ctx, testPN, "id", small); err == nil || !strings.HasPrefix(err.Error(), "upload: ") {
				t.Errorf("offline upload error = %v, want upload: ...", err)
			}
		})
	}
}

func TestDownloadMedia(t *testing.T) {
	c, _ := newStoreClient(t)
	ctx := context.Background()
	base := core.Message{ID: "MSG1", ChatJID: testPN.String(), MediaType: "image", MimeType: "image/jpeg"}

	if _, err := c.DownloadMedia(ctx, base); err == nil || !strings.Contains(err.Error(), "no download metadata") {
		t.Errorf("no metadata error = %v", err)
	}

	withMeta := base
	withMeta.DirectPath = "/v/t62/abc"
	withMeta.MediaKey = []byte("key")

	if _, err := c.DownloadMedia(ctx, withMeta); err == nil || !strings.HasPrefix(err.Error(), "download: ") {
		t.Errorf("offline download error = %v, want download: ...", err)
	}

	// A cached file is returned without touching the network.
	cached, err := mediaCachePath(c.mediaDir, "MSG1", extFromMime("image/jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cached, []byte("jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := c.DownloadMedia(ctx, withMeta); err != nil || got != cached {
		t.Errorf("cached DownloadMedia() = %q, %v; want %q", got, err, cached)
	}
}

func TestMediaCachePath(t *testing.T) {
	dir := t.TempDir()
	p, err := mediaCachePath(dir, "../../etc/passwd", ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dir || filepath.Base(p) != "etcpasswd.jpg" {
		t.Errorf("mediaCachePath() = %q, want sanitized file in %q", p, dir)
	}
	p, err = mediaCachePath(dir, "../..", "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dir || len(filepath.Base(p)) != 64 {
		t.Errorf("mediaCachePath() for all-unsafe ID = %q, want sha256 name", p)
	}
}

func TestMediaTypeMappings(t *testing.T) {
	cases := []struct {
		in  string
		wa  whatsmeow.MediaType
		mms string
	}{
		{"image", whatsmeow.MediaImage, "image"},
		{"sticker", whatsmeow.MediaImage, "image"},
		{"video", whatsmeow.MediaVideo, "video"},
		{"gif", whatsmeow.MediaVideo, "video"},
		{"audio", whatsmeow.MediaAudio, "audio"},
		{"voice", whatsmeow.MediaAudio, "audio"},
		{"document", whatsmeow.MediaDocument, "document"},
	}
	for _, c := range cases {
		if got := waMediaType(c.in); got != c.wa {
			t.Errorf("waMediaType(%q) = %q, want %q", c.in, got, c.wa)
		}
		if got := mmsType(c.in); got != c.mms {
			t.Errorf("mmsType(%q) = %q, want %q", c.in, got, c.mms)
		}
	}
}

func TestExtFromMime(t *testing.T) {
	if got := extFromMime("video/mp4; codecs=avc1"); got == "" {
		t.Error("extFromMime strips parameters: got empty")
	}
	if got := extFromMime("application/x-watui-unknown"); got != "" {
		t.Errorf("extFromMime(unknown) = %q, want empty", got)
	}
}

func TestMediaOpenCommand(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, h := range names {
				if h == n {
					return "/usr/bin/" + n, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	tests := []struct {
		mediaType string
		lookPath  func(string) (string, error)
		wantProg  string
	}{
		{"voice", has("ffplay", "mpv"), "mpv"},
		{"audio", has("aplay"), "aplay"},
		{"audio", has(), "xdg-open"},
		{"image", has("mpv"), "xdg-open"},
	}
	for _, tt := range tests {
		// A relative path starting with "-" must reach the program as an
		// absolute path (so it can't be read as a flag) and without "--":
		// xdg-open rejects "--" as an unknown option and opens nothing.
		cmd, err := mediaOpenCommand("-weird.ogg", tt.mediaType, tt.lookPath)
		if err != nil {
			t.Fatalf("mediaOpenCommand(%s) error = %v", tt.mediaType, err)
		}
		if len(cmd.Args) != 2 || filepath.Base(cmd.Args[0]) != tt.wantProg ||
			!filepath.IsAbs(cmd.Args[1]) || filepath.Base(cmd.Args[1]) != "-weird.ogg" {
			t.Errorf("mediaOpenCommand(%s) args = %v, want %s /abs/-weird.ogg", tt.mediaType, cmd.Args, tt.wantProg)
		}
	}
}

func TestCanonicalChatJIDWithStore(t *testing.T) {
	c, _ := newStoreClient(t)
	ctx := context.Background()

	if got := c.canonicalChatJID(testGrp, types.EmptyJID); got != testGrp {
		t.Errorf("group = %v, want unchanged", got)
	}
	if got := c.canonicalChatJID(testLID, types.EmptyJID); got != testLID {
		t.Errorf("unknown LID = %v, want unchanged", got)
	}
	// Seeing the LID with its PN alt stores the mapping and yields the PN...
	if got := c.canonicalChatJID(testLID, testPN); got != testPN {
		t.Errorf("LID with PN alt = %v, want %v", got, testPN)
	}
	// ...so later lookups resolve without the alt, in both directions.
	if got := c.canonicalChatJID(testLID, types.EmptyJID); got != testPN {
		t.Errorf("mapped LID = %v, want %v", got, testPN)
	}
	if got := c.canonicalChatJID(testPN, testLID); got != testPN {
		t.Errorf("PN = %v, want unchanged", got)
	}
	if got := c.AltChatJID(ctx, testPN.String()); got != testLID.String() {
		t.Errorf("AltChatJID(PN) = %q, want %q", got, testLID)
	}
	if got := c.AltChatJID(ctx, "not a:jid:x@y"); got != "" {
		t.Errorf("AltChatJID(invalid) = %q, want empty", got)
	}
	other := types.NewJID("555", types.HiddenUserServer)
	if got := c.AltChatJID(ctx, other.String()); got != "" {
		t.Errorf("AltChatJID(unmapped) = %q, want empty", got)
	}
}

func TestContactNamesFromStore(t *testing.T) {
	c, _ := newStoreClient(t)
	ctx := context.Background()

	if got := c.GetContactName(ctx, testPN); got != "" {
		t.Errorf("unknown contact name = %q", got)
	}

	c.canonicalChatJID(testLID, testPN) // store the LID↔PN mapping
	if _, _, err := c.wm.Store.Contacts.PutPushName(ctx, testPN, "Ana"); err != nil {
		t.Fatal(err)
	}

	if got := c.GetContactName(ctx, testPN); got != "Ana" {
		t.Errorf("GetContactName(PN) = %q, want Ana", got)
	}
	if got := c.GetContactName(ctx, testLID); got != "Ana" {
		t.Errorf("GetContactName(LID) = %q, want Ana via PN mapping", got)
	}

	names, err := c.GetAllContactNames(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if names[testPN.String()] != "Ana" || names[testLID.String()] != "Ana" {
		t.Errorf("GetAllContactNames() = %v, want Ana under PN and LID", names)
	}
}

func TestHandleEventConnectionEvents(t *testing.T) {
	c, rec := newOfflineClient(t)

	c.handleEvent(&events.Connected{}) // no own JID yet: nothing to report
	c.handleEvent(&events.Disconnected{})
	c.handleEvent(&events.LoggedOut{})
	c.handleEvent(&events.ClientOutdated{})
	c.handleEvent(&events.QR{Codes: []string{"code-1", "code-2"}})
	c.handleEvent(&events.QR{})
	c.handleEvent(&events.PairSuccess{ID: testPN})
	c.handleEvent(&events.PushName{})

	got := rec.take()
	if len(got) != 5 {
		t.Fatalf("events = %#v, want 5", got)
	}
	if _, ok := got[0].(core.Disconnected); !ok || got[0].(core.Disconnected).Err != nil {
		t.Errorf("event[0] = %#v, want clean Disconnected", got[0])
	}
	if d, ok := got[1].(core.Disconnected); !ok || d.Err == nil || !strings.Contains(d.Err.Error(), "logged out") {
		t.Errorf("event[1] = %#v, want Disconnected with logged-out error", got[1])
	}
	if got[2] != (core.ClientOutdated{}) {
		t.Errorf("event[2] = %#v, want ClientOutdated", got[2])
	}
	if got[3] != (core.QRCode{Code: "code-1"}) {
		t.Errorf("event[3] = %#v, want first QR code", got[3])
	}
	if got[4] != (core.LoginSuccess{JID: testPN}) {
		t.Errorf("event[4] = %#v, want LoginSuccess", got[4])
	}

	own := testPN
	c.wm.Store.ID = &own
	c.handleEvent(&events.Connected{})
	if got := rec.take(); len(got) != 1 || got[0] != (core.Connected{JID: testPN}) {
		t.Errorf("Connected events = %#v", got)
	}
}

func TestHandleEventReceiptAndPresence(t *testing.T) {
	c, rec := newStoreClient(t)

	receipt := func(typ types.ReceiptType, ids ...types.MessageID) *events.Receipt {
		return &events.Receipt{
			MessageSource: types.MessageSource{Chat: testGrp, IsGroup: true},
			MessageIDs:    ids,
			Type:          typ,
		}
	}
	c.handleEvent(receipt(types.ReceiptTypeDelivered, "a", "", "b"))
	c.handleEvent(receipt(types.ReceiptTypeRead, "c"))
	c.handleEvent(receipt(types.ReceiptTypePlayed, "d"))

	want := []core.Event{
		core.MessageStatus{ChatJID: testGrp, MessageID: "a", Status: "delivered"},
		core.MessageStatus{ChatJID: testGrp, MessageID: "b", Status: "delivered"},
		core.MessageStatus{ChatJID: testGrp, MessageID: "c", Status: "read"},
	}
	got := rec.take()
	if len(got) != len(want) {
		t.Fatalf("receipt events = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}

	c.handleEvent(&events.ChatPresence{
		MessageSource: types.MessageSource{Chat: testLID, Sender: testLID, SenderAlt: testPN},
		State:         types.ChatPresenceComposing,
	})
	c.handleEvent(&events.ChatPresence{
		MessageSource: types.MessageSource{Chat: testLID, Sender: testPN, IsFromMe: true},
		State:         types.ChatPresencePaused,
	})
	got = rec.take()
	if len(got) != 2 {
		t.Fatalf("presence events = %#v", got)
	}
	if got[0] != (core.Typing{ChatJID: testPN, Sender: testLID, IsTyping: true}) {
		t.Errorf("typing = %#v, want chat resolved to PN", got[0])
	}
	if ty := got[1].(core.Typing); ty.IsTyping || ty.ChatJID != testPN {
		t.Errorf("paused = %#v", got[1])
	}
}

func TestHandleEventMessageDirect(t *testing.T) {
	c, rec := newStoreClient(t)
	c.handleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: testLID, Sender: testLID, SenderAlt: testPN},
			ID:            "M1",
			PushName:      "Ana",
		},
		Message: &waProto.Message{Conversation: proto.String("oi")},
	})
	got := rec.take()
	if len(got) != 1 {
		t.Fatalf("events = %#v", got)
	}
	m := got[0].(core.NewMessage).Message
	if m.ChatJID != testPN.String() || m.SenderJID != testPN.String() || m.SenderName != "Ana" || m.Status != "received" {
		t.Errorf("message = %+v, want chat and sender canonicalized to PN", m)
	}

	c.handleEvent(&events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: testPN, Sender: testPN, IsFromMe: true},
			ID:            "M2",
		},
		Message: &waProto.Message{Conversation: proto.String("eu")},
	})
	if m := rec.take()[0].(core.NewMessage).Message; m.Status != "sent" || !m.IsFromMe {
		t.Errorf("own message = %+v, want status sent", m)
	}
}

func TestHandleEventHistorySync(t *testing.T) {
	c, rec := newStoreClient(t)

	c.handleEvent(&events.HistorySync{}) // no data: ignored
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("empty sync events = %#v", got)
	}

	withMsgs := historyConv(testGrp.String(),
		histMsg{id: "h1", ts: 10, participant: testPN.String(), msg: text("oi")},
	)
	empty := historyConv(testPN.String())
	c.handleEvent(&events.HistorySync{Data: &waHistorySync.HistorySync{
		Conversations: []*waHistorySync.Conversation{historyConv(""), withMsgs, empty},
	}})

	got := rec.take()
	if len(got) != 4 {
		t.Fatalf("events = %#v, want group conv + messages, empty conv, complete", got)
	}
	if cu, ok := got[0].(core.ConversationUpdated); !ok || cu.Conversation.JID != testGrp.String() {
		t.Errorf("event[0] = %#v, want group ConversationUpdated first", got[0])
	}
	if ml, ok := got[1].(core.MessagesLoaded); !ok || ml.ChatJID != testGrp || len(ml.Messages) != 1 {
		t.Errorf("event[1] = %#v, want one message loaded for group", got[1])
	}
	if cu, ok := got[2].(core.ConversationUpdated); !ok || cu.Conversation.JID != testPN.String() {
		t.Errorf("event[2] = %#v, want empty 1:1 conversation without MessagesLoaded", got[2])
	}
	if got[3] != (core.HistorySyncComplete{}) {
		t.Errorf("event[3] = %#v, want HistorySyncComplete", got[3])
	}
}

func TestHandleEventPushName(t *testing.T) {
	c, rec := newStoreClient(t)

	c.handleEvent(&events.PushName{JID: testPN, NewPushName: "Loja"})
	c.handleEvent(&events.PushName{JID: testPN}) // cleared name: nothing to apply

	got := rec.take()
	want := core.PushNameChanged{JID: testPN.String(), Name: "Loja"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("events = %#v, want [%#v]", got, want)
	}
}
