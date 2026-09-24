package whatsapp

import (
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// fakeHistoryResolver maps JIDs through fixed tables and records skips.
type fakeHistoryResolver struct {
	canonical map[string]types.JID
	contacts  map[string]string
	groups    map[string]string
	skips     []string
	unknown   []string
}

func (f *fakeHistoryResolver) canonicalChatJID(chat types.JID) types.JID {
	if c, ok := f.canonical[chat.String()]; ok {
		return c
	}
	return chat.ToNonAD()
}

func (f *fakeHistoryResolver) contactName(jid types.JID) string { return f.contacts[jid.String()] }
func (f *fakeHistoryResolver) groupName(jid types.JID) string   { return f.groups[jid.String()] }
func (f *fakeHistoryResolver) skipped(id string, _ *waProto.Message) {
	f.skips = append(f.skips, id)
}
func (f *fakeHistoryResolver) unsupported(id string, _ *waProto.Message) {
	f.unknown = append(f.unknown, id)
}

type histMsg struct {
	id          string
	fromMe      bool
	participant string
	pushName    string
	ts          uint64
	msg         *waProto.Message
}

func historyConv(id string, msgs ...histMsg) *waHistorySync.Conversation {
	conv := &waHistorySync.Conversation{ID: proto.String(id)}
	for _, m := range msgs {
		key := &waCommon.MessageKey{
			ID:     proto.String(m.id),
			FromMe: proto.Bool(m.fromMe),
		}
		if m.participant != "" {
			key.Participant = proto.String(m.participant)
		}
		conv.Messages = append(conv.Messages, &waHistorySync.HistorySyncMsg{
			Message: &waWeb.WebMessageInfo{
				Key:              key,
				MessageTimestamp: proto.Uint64(m.ts),
				Message:          m.msg,
				PushName:         pushNamePtr(m.pushName),
			},
		})
	}
	return conv
}

func text(s string) *waProto.Message { return &waProto.Message{Conversation: proto.String(s)} }

func TestConvertHistoryConversationInvalidJID(t *testing.T) {
	for _, id := range []string{"", "1.2.3:x@s.whatsapp.net"} {
		if _, _, ok := convertHistoryConversation(historyConv(id), &fakeHistoryResolver{}); ok {
			t.Errorf("convertHistoryConversation(%q) ok = true, want false", id)
		}
	}
}

func TestConvertHistoryConversationDirect(t *testing.T) {
	lid := "123456789@lid"
	pn := types.NewJID("5511999999999", types.DefaultUserServer)
	r := &fakeHistoryResolver{
		canonical: map[string]types.JID{lid: pn},
		contacts:  map[string]string{pn.String(): "Ana"},
	}

	conv := historyConv(lid,
		histMsg{id: "m1", ts: 100, msg: text("oi")},
		histMsg{id: "m2", ts: 300, fromMe: true, msg: text("tudo bem?")},
		histMsg{id: "m3", ts: 200, msg: &waProto.Message{
			ImageMessage: &waProto.ImageMessage{Caption: proto.String("foto"), Mimetype: proto.String("image/jpeg")},
		}},
		histMsg{id: "r1", ts: 400, msg: &waProto.Message{
			ReactionMessage: &waProto.ReactionMessage{Text: proto.String("👍")},
		}},
		histMsg{id: "nil-msg", ts: 500},
	)
	conv.UnreadCount = proto.Uint32(2)
	conv.Pinned = proto.Uint32(1)

	got, msgs, ok := convertHistoryConversation(conv, r)
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.JID != pn.String() || got.Name != "Ana" || got.IsGroup {
		t.Errorf("conversation = %+v, want JID %s named Ana, not a group", got, pn)
	}
	if got.UnreadCount != 2 || !got.IsPinned {
		t.Errorf("unread/pinned = %d/%v, want 2/true", got.UnreadCount, got.IsPinned)
	}
	if got.LastMessage != "tudo bem?" || !got.LastMsgTime.Equal(time.Unix(300, 0)) {
		t.Errorf("last = %q at %v, want newest message", got.LastMessage, got.LastMsgTime)
	}

	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	if len(r.skips) != 1 || r.skips[0] != "r1" {
		t.Errorf("skipped = %v, want [r1]", r.skips)
	}

	in, out, img := msgs[0], msgs[1], msgs[2]
	if in.ChatJID != pn.String() || in.Content != "oi" || in.Status != "received" || in.IsFromMe {
		t.Errorf("incoming = %+v", in)
	}
	if out.Status != "read" || !out.IsFromMe || out.SenderJID != pn.String() {
		t.Errorf("outgoing = %+v, want read, from me, sender = chat", out)
	}
	if img.MediaType != "image" || img.Content != "foto" || img.MimeType != "image/jpeg" {
		t.Errorf("image = %+v, want image with bare caption", img)
	}
	if !in.Timestamp.Equal(time.Unix(100, 0)) {
		t.Errorf("timestamp = %v, want unix 100", in.Timestamp)
	}
}

func TestConvertHistoryConversationGroupName(t *testing.T) {
	group := "120363000000000000@g.us"
	r := &fakeHistoryResolver{groups: map[string]string{group: "Família"}}

	got, _, _ := convertHistoryConversation(historyConv(group), r)
	if !got.IsGroup || got.Name != "Família" {
		t.Errorf("conversation = %+v, want group named Família", got)
	}

	// A display name from the sync wins over the resolver.
	conv := historyConv(group)
	conv.DisplayName = proto.String("Synced")
	got, _, _ = convertHistoryConversation(conv, r)
	if got.Name != "Synced" {
		t.Errorf("name = %q, want Synced", got.Name)
	}
}

func TestConvertHistoryConversationUnknownKindIsPlaceholder(t *testing.T) {
	conv := historyConv("5511999999999@s.whatsapp.net",
		histMsg{id: "p1", ts: 1, msg: &waProto.Message{ScheduledCallCreationMessage: &waProto.ScheduledCallCreationMessage{}}},
	)
	_, msgs, _ := convertHistoryConversation(conv, &fakeHistoryResolver{})
	if len(msgs) != 1 || msgs[0].Content != unsupportedPlaceholder {
		t.Errorf("messages = %+v, want one unsupported placeholder", msgs)
	}
}

func TestUnwrapMessage(t *testing.T) {
	inner := text("segredo")
	fp := func(m *waProto.Message) *waProto.FutureProofMessage { return &waProto.FutureProofMessage{Message: m} }
	img := &waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String("uma vez")}}
	doc := &waProto.Message{DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("a.pdf"), Caption: proto.String("veja")}}

	tests := []struct {
		name string
		msg  *waProto.Message
		want *waProto.Message
	}{
		{"nil", nil, nil},
		{"plain", inner, inner},
		{"ephemeral", &waProto.Message{EphemeralMessage: fp(inner)}, inner},
		{"view once", &waProto.Message{ViewOnceMessage: fp(img)}, img},
		{"view once v2", &waProto.Message{ViewOnceMessageV2: fp(img)}, img},
		{"view once v2 extension", &waProto.Message{ViewOnceMessageV2Extension: fp(img)}, img},
		{"document with caption", &waProto.Message{DocumentWithCaptionMessage: fp(doc)}, doc},
		{"device sent", &waProto.Message{DeviceSentMessage: &waProto.DeviceSentMessage{Message: inner}}, inner},
		{"bot invoke", &waProto.Message{BotInvokeMessage: fp(inner)}, inner},
		{"lottie sticker", &waProto.Message{LottieStickerMessage: fp(inner)}, inner},
		{
			"ephemeral view once",
			&waProto.Message{EphemeralMessage: fp(&waProto.Message{ViewOnceMessageV2: fp(img)})},
			img,
		},
		{"empty wrapper stays", &waProto.Message{EphemeralMessage: fp(nil)}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unwrapMessage(tt.msg)
			if tt.name == "empty wrapper stays" {
				if got != tt.msg {
					t.Errorf("unwrapMessage() = %v, want wrapper unchanged", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("unwrapMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Edits stay wrapped: they are dropped until edit handling lands, instead of
// showing up as a duplicate bubble.
func TestUnwrapMessageKeepsEdits(t *testing.T) {
	edit := &waProto.Message{EditedMessage: &waProto.FutureProofMessage{Message: text("novo")}}
	if got := unwrapMessage(edit); got != edit {
		t.Errorf("unwrapMessage(edit) = %v, want unchanged", got)
	}
	if isDisplayable(unwrapMessage(edit)) {
		t.Error("edit became displayable after unwrap")
	}
}

func TestConvertHistoryConversationUnwrapsDisappearing(t *testing.T) {
	fp := func(m *waProto.Message) *waProto.FutureProofMessage { return &waProto.FutureProofMessage{Message: m} }
	conv := historyConv("5511999999999@s.whatsapp.net",
		histMsg{id: "e1", ts: 1, msg: &waProto.Message{EphemeralMessage: fp(text("some em 7 dias"))}},
		histMsg{id: "v1", ts: 2, msg: &waProto.Message{ViewOnceMessageV2: fp(&waProto.Message{
			ImageMessage: &waProto.ImageMessage{Caption: proto.String("uma vez"), Mimetype: proto.String("image/jpeg")},
		})}},
		histMsg{id: "d1", ts: 3, msg: &waProto.Message{DocumentWithCaptionMessage: fp(&waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("a.pdf"), Caption: proto.String("veja")},
		})}},
		histMsg{id: "r1", ts: 4, msg: &waProto.Message{EphemeralMessage: fp(&waProto.Message{
			ReactionMessage: &waProto.ReactionMessage{Text: proto.String("👍")},
		})}},
	)
	r := &fakeHistoryResolver{}
	got, msgs, _ := convertHistoryConversation(conv, r)

	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "some em 7 dias" {
		t.Errorf("ephemeral content = %q", msgs[0].Content)
	}
	if msgs[1].MediaType != "image" || msgs[1].Content != "uma vez" {
		t.Errorf("view-once = %+v, want image with caption", msgs[1])
	}
	if msgs[2].MediaType != "document" || msgs[2].FileName != "a.pdf" || msgs[2].Content != "veja" {
		t.Errorf("document with caption = %+v", msgs[2])
	}
	if len(r.skips) != 1 || r.skips[0] != "r1" {
		t.Errorf("skipped = %v, want wrapped reaction r1", r.skips)
	}
	if got.LastMessage == "[media]" || got.LastMessage == "" {
		t.Errorf("last message preview = %q", got.LastMessage)
	}
}

// Senders are keyed like live messages: group participants lose their device
// suffix, and 1:1 incoming messages are attributed to the canonical chat.
func TestConvertHistoryConversationCanonicalSender(t *testing.T) {
	pn := types.NewJID("5511999999999", types.DefaultUserServer)
	lidChat := "123456789@lid"
	r := &fakeHistoryResolver{canonical: map[string]types.JID{
		lidChat:           pn,
		"123456789:7@lid": pn,
	}}

	group := historyConv("120363000000000000@g.us",
		histMsg{id: "g1", ts: 1, participant: "5511777777777:12@s.whatsapp.net", msg: text("a")},
		histMsg{id: "g2", ts: 2, participant: "111111111@lid", msg: text("b")},
	)
	// WebMessageInfo.participant takes precedence over the key's, as in
	// whatsmeow's ParseWebMessage.
	group.Messages[1].Message.Participant = proto.String("987654321:4@lid")

	_, msgs, _ := convertHistoryConversation(group, r)
	if got := msgs[0].SenderJID; got != "5511777777777@s.whatsapp.net" {
		t.Errorf("group sender = %q, want device suffix dropped", got)
	}
	if got := msgs[1].SenderJID; got != "987654321@lid" {
		t.Errorf("group sender = %q, want WebMessageInfo participant without device", got)
	}

	direct := historyConv(lidChat,
		histMsg{id: "d1", ts: 1, msg: text("sem participant")},
		histMsg{id: "d2", ts: 2, participant: "123456789:7@lid", msg: text("com participant")},
	)
	_, msgs, _ = convertHistoryConversation(direct, r)
	for _, m := range msgs {
		if m.SenderJID != pn.String() {
			t.Errorf("%s: 1:1 sender = %q, want canonical chat %s", m.ID, m.SenderJID, pn)
		}
	}
}

func pushNamePtr(name string) *string {
	if name == "" {
		return nil
	}
	return proto.String(name)
}

func TestConvertHistoryConversationUsesPushNames(t *testing.T) {
	text := func(s string) *waProto.Message { return &waProto.Message{Conversation: proto.String(s)} }
	conv := historyConv("5511999999999@s.whatsapp.net",
		histMsg{id: "a", ts: 1, pushName: "Loja Antiga", msg: text("oi")},
		histMsg{id: "b", ts: 2, fromMe: true, pushName: "Eu", msg: text("oi")},
		histMsg{id: "c", ts: 3, pushName: "Loja Nova", msg: text("promo")},
	)
	got, msgs, _ := convertHistoryConversation(conv, &fakeHistoryResolver{})
	if got.Name != "Loja Nova" {
		t.Errorf("unnamed 1:1 name = %q, want newest incoming push name", got.Name)
	}
	if msgs[0].SenderName != "Loja Antiga" || msgs[1].SenderName != "" {
		t.Errorf("sender names = %q, %q; want push name on incoming only", msgs[0].SenderName, msgs[1].SenderName)
	}

	named, _, _ := convertHistoryConversation(conv, &fakeHistoryResolver{
		contacts: map[string]string{"5511999999999@s.whatsapp.net": "Agenda"},
	})
	if named.Name != "Agenda" {
		t.Errorf("name = %q, contact name must win over push name", named.Name)
	}
}

func TestConvertHistoryConversationGroupNameNotFromPushName(t *testing.T) {
	conv := historyConv("123@g.us",
		histMsg{id: "g", ts: 1, participant: "55119@s.whatsapp.net", pushName: "Membro",
			msg: &waProto.Message{Conversation: proto.String("oi")}},
	)
	got, msgs, _ := convertHistoryConversation(conv, &fakeHistoryResolver{})
	if got.Name != "" {
		t.Errorf("group name = %q, must not come from a member's push name", got.Name)
	}
	if msgs[0].SenderName != "Membro" {
		t.Errorf("group sender name = %q, want Membro", msgs[0].SenderName)
	}
}

func TestConvertHistoryConversationReportsUnsupported(t *testing.T) {
	conv := historyConv("5511999999999@s.whatsapp.net",
		histMsg{id: "u", ts: 1, msg: &waProto.Message{ScheduledCallCreationMessage: &waProto.ScheduledCallCreationMessage{}}},
		histMsg{id: "t", ts: 2, msg: &waProto.Message{Conversation: proto.String("ok")}},
	)
	r := &fakeHistoryResolver{}
	convertHistoryConversation(conv, r)
	if len(r.unknown) != 1 || r.unknown[0] != "u" {
		t.Errorf("unsupported = %v, want [u]", r.unknown)
	}
}
