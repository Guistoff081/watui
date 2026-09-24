package whatsapp

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/watui/watui/internal/core"
)

func TestIsDisplayable(t *testing.T) {
	ctxInfo := &waProto.MessageContextInfo{MessageSecret: []byte("secret")}
	skdm := &waProto.SenderKeyDistributionMessage{GroupID: proto.String("123@g.us")}

	tests := []struct {
		name string
		msg  *waProto.Message
		want bool
	}{
		{"nil", nil, false},
		{"empty", &waProto.Message{}, false},
		{"conversation", &waProto.Message{Conversation: proto.String("hi")}, true},
		{"empty conversation string", &waProto.Message{Conversation: proto.String("")}, true},
		{
			"extended text",
			&waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String("hi")}},
			true,
		},
		{"image", &waProto.Message{ImageMessage: &waProto.ImageMessage{}}, true},
		{"video", &waProto.Message{VideoMessage: &waProto.VideoMessage{}}, true},
		{"audio", &waProto.Message{AudioMessage: &waProto.AudioMessage{}}, true},
		{"voice", &waProto.Message{AudioMessage: &waProto.AudioMessage{PTT: proto.Bool(true)}}, true},
		{"document", &waProto.Message{DocumentMessage: &waProto.DocumentMessage{}}, true},
		{"sticker", &waProto.Message{StickerMessage: &waProto.StickerMessage{}}, true},
		{"location", &waProto.Message{LocationMessage: &waProto.LocationMessage{}}, true},
		{"contact", &waProto.Message{ContactMessage: &waProto.ContactMessage{}}, true},
		{
			// Unknown-but-content-bearing kinds stay visible (rendered as [media]).
			"poll creation",
			&waProto.Message{PollCreationMessage: &waProto.PollCreationMessage{Name: proto.String("q")}},
			true,
		},
		{
			"text with sender key and context info",
			&waProto.Message{
				Conversation:                 proto.String("hi"),
				SenderKeyDistributionMessage: skdm,
				MessageContextInfo:           ctxInfo,
			},
			true,
		},
		{
			"reaction",
			&waProto.Message{ReactionMessage: &waProto.ReactionMessage{Text: proto.String("👍")}},
			false,
		},
		{"encrypted reaction", &waProto.Message{EncReactionMessage: &waProto.EncReactionMessage{}}, false},
		{
			"protocol revoke",
			&waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
				Type: waProto.ProtocolMessage_REVOKE.Enum(),
			}},
			false,
		},
		{
			"protocol edit",
			&waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
				Type:          waProto.ProtocolMessage_MESSAGE_EDIT.Enum(),
				EditedMessage: &waProto.Message{Conversation: proto.String("edited")},
			}},
			false,
		},
		{
			"edited message wrapper",
			&waProto.Message{EditedMessage: &waProto.FutureProofMessage{
				Message: &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
					Type: waProto.ProtocolMessage_MESSAGE_EDIT.Enum(),
				}},
			}},
			false,
		},
		{
			"ephemeral setting",
			&waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
				Type:                waProto.ProtocolMessage_EPHEMERAL_SETTING.Enum(),
				EphemeralExpiration: proto.Uint32(86400),
			}},
			false,
		},
		{"sender key distribution only", &waProto.Message{SenderKeyDistributionMessage: skdm}, false},
		{
			"fast ratchet sender key only",
			&waProto.Message{FastRatchetKeySenderKeyDistributionMessage: skdm},
			false,
		},
		{"context info only", &waProto.Message{MessageContextInfo: ctxInfo}, false},
		{
			"sender key and context info only",
			&waProto.Message{SenderKeyDistributionMessage: skdm, MessageContextInfo: ctxInfo},
			false,
		},
		{"poll update", &waProto.Message{PollUpdateMessage: &waProto.PollUpdateMessage{}}, false},
		{"keep in chat", &waProto.Message{KeepInChatMessage: &waProto.KeepInChatMessage{}}, false},
		{"pin in chat", &waProto.Message{PinInChatMessage: &waProto.PinInChatMessage{}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDisplayable(tt.msg); got != tt.want {
				t.Errorf("isDisplayable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNonDisplayableFieldNames guards against typos in the skip set: every
// name must be a real field of waE2E.Message.
func TestNonDisplayableFieldNames(t *testing.T) {
	fields := (&waProto.Message{}).ProtoReflect().Descriptor().Fields()
	for name := range nonDisplayableFields {
		if fields.ByName(protoreflect.Name(name)) == nil {
			t.Errorf("nonDisplayableFields: %q is not a field of waE2E.Message", name)
		}
	}
}

func TestExtractTextContentKinds(t *testing.T) {
	tests := []struct {
		name string
		msg  *waProto.Message
		want string
	}{
		{
			"video with caption",
			&waProto.Message{VideoMessage: &waProto.VideoMessage{Caption: proto.String("clip")}},
			"[video] clip",
		},
		{"video no caption", &waProto.Message{VideoMessage: &waProto.VideoMessage{}}, "[video]"},
		{"document no name", &waProto.Message{DocumentMessage: &waProto.DocumentMessage{}}, "[file]"},
		{
			"contact",
			&waProto.Message{ContactMessage: &waProto.ContactMessage{DisplayName: proto.String("Ana")}},
			"[contact] Ana",
		},
		{
			"extended text without text",
			&waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{}},
			"",
		},
		{"reaction", &waProto.Message{ReactionMessage: &waProto.ReactionMessage{Text: proto.String("👍")}}, ""},
		{"protocol", &waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{}}, ""},
		{
			"conversation wins over media",
			&waProto.Message{
				Conversation: proto.String("text"),
				ImageMessage: &waProto.ImageMessage{Caption: proto.String("cap")},
			},
			"text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextContent(tt.msg); got != tt.want {
				t.Errorf("extractTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractMedia(t *testing.T) {
	key := []byte("key")
	sha := []byte("sha")
	encSHA := []byte("encsha")
	thumb := []byte{0xff, 0xd8}

	tests := []struct {
		name string
		msg  *waProto.Message
		want mediaMeta
	}{
		{"nil", nil, mediaMeta{}},
		{"empty", &waProto.Message{}, mediaMeta{}},
		{"text", &waProto.Message{Conversation: proto.String("hi")}, mediaMeta{}},
		{"reaction", &waProto.Message{ReactionMessage: &waProto.ReactionMessage{}}, mediaMeta{}},
		{
			"image",
			&waProto.Message{ImageMessage: &waProto.ImageMessage{
				DirectPath:    proto.String("/img"),
				MediaKey:      key,
				FileSHA256:    sha,
				FileEncSHA256: encSHA,
				Mimetype:      proto.String("image/jpeg"),
				JPEGThumbnail: thumb,
				Width:         proto.Uint32(640),
				Height:        proto.Uint32(480),
				Caption:       proto.String("cap"),
			}},
			mediaMeta{
				mediaType:     "image",
				directPath:    "/img",
				mediaKey:      key,
				fileSHA256:    sha,
				fileEncSHA256: encSHA,
				mimeType:      "image/jpeg",
				thumbnail:     thumb,
				width:         640,
				height:        480,
				caption:       "cap",
			},
		},
		{
			"video",
			&waProto.Message{VideoMessage: &waProto.VideoMessage{
				Mimetype: proto.String("video/mp4"),
				Seconds:  proto.Uint32(12),
				Width:    proto.Uint32(1280),
				Height:   proto.Uint32(720),
				Caption:  proto.String("clip"),
			}},
			mediaMeta{
				mediaType: "video",
				mimeType:  "video/mp4",
				duration:  12,
				width:     1280,
				height:    720,
				caption:   "clip",
			},
		},
		{
			"gif",
			&waProto.Message{VideoMessage: &waProto.VideoMessage{
				Mimetype:    proto.String("video/mp4"),
				GifPlayback: proto.Bool(true),
				Seconds:     proto.Uint32(3),
			}},
			mediaMeta{mediaType: "gif", mimeType: "video/mp4", duration: 3, isAnimated: true},
		},
		{
			"audio",
			&waProto.Message{AudioMessage: &waProto.AudioMessage{
				Mimetype: proto.String("audio/mpeg"),
				Seconds:  proto.Uint32(200),
			}},
			mediaMeta{mediaType: "audio", mimeType: "audio/mpeg", duration: 200},
		},
		{
			"voice (ptt)",
			&waProto.Message{AudioMessage: &waProto.AudioMessage{
				Mimetype: proto.String("audio/ogg; codecs=opus"),
				Seconds:  proto.Uint32(7),
				PTT:      proto.Bool(true),
			}},
			mediaMeta{mediaType: "voice", mimeType: "audio/ogg; codecs=opus", duration: 7},
		},
		{
			"document",
			&waProto.Message{DocumentMessage: &waProto.DocumentMessage{
				Mimetype: proto.String("application/pdf"),
				FileName: proto.String("a.pdf"),
				Caption:  proto.String("report"),
			}},
			mediaMeta{mediaType: "document", mimeType: "application/pdf", fileName: "a.pdf", caption: "report"},
		},
		{
			"sticker",
			&waProto.Message{StickerMessage: &waProto.StickerMessage{
				Mimetype: proto.String("image/webp"),
				Width:    proto.Uint32(512),
				Height:   proto.Uint32(512),
			}},
			mediaMeta{mediaType: "sticker", mimeType: "image/webp", width: 512, height: 512},
		},
		{
			"animated sticker",
			&waProto.Message{StickerMessage: &waProto.StickerMessage{
				Mimetype:   proto.String("image/webp"),
				IsAnimated: proto.Bool(true),
			}},
			mediaMeta{mediaType: "sticker", mimeType: "image/webp", isAnimated: true},
		},
		{"location is not media", &waProto.Message{LocationMessage: &waProto.LocationMessage{}}, mediaMeta{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractMedia(tt.msg)
			if !mediaMetaEqual(got, tt.want) {
				t.Errorf("extractMedia() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func mediaMetaEqual(a, b mediaMeta) bool {
	return a.mediaType == b.mediaType &&
		a.directPath == b.directPath &&
		string(a.mediaKey) == string(b.mediaKey) &&
		string(a.fileSHA256) == string(b.fileSHA256) &&
		string(a.fileEncSHA256) == string(b.fileEncSHA256) &&
		a.mimeType == b.mimeType &&
		string(a.thumbnail) == string(b.thumbnail) &&
		a.width == b.width &&
		a.height == b.height &&
		a.duration == b.duration &&
		a.isAnimated == b.isAnimated &&
		a.fileName == b.fileName &&
		a.caption == b.caption
}

// TestHandleMessageSkipsNonDisplayable drives handleMessage with a group chat
// (group JIDs need no whatsmeow store lookups) and checks that only content
// messages reach the Bubble Tea program.
func TestHandleMessageSkipsNonDisplayable(t *testing.T) {
	group := types.NewJID("123456", types.GroupServer)
	sender := types.NewJID("5511999999999", types.DefaultUserServer)

	newEvt := func(id string, m *waProto.Message) *events.Message {
		return &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true},
				ID:            id,
				Timestamp:     time.Unix(1700000000, 0),
			},
			Message: m,
		}
	}

	tests := []struct {
		name    string
		msg     *waProto.Message
		wantMsg bool
	}{
		{"text", &waProto.Message{Conversation: proto.String("hi")}, true},
		{"reaction", &waProto.Message{ReactionMessage: &waProto.ReactionMessage{Text: proto.String("👍")}}, false},
		{
			"revoke",
			&waProto.Message{ProtocolMessage: &waProto.ProtocolMessage{
				Type: waProto.ProtocolMessage_REVOKE.Enum(),
			}},
			false,
		},
		{
			"sender key only",
			&waProto.Message{SenderKeyDistributionMessage: &waProto.SenderKeyDistributionMessage{}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sent []tea.Msg
			c := &Client{sendMsg: func(m tea.Msg) { sent = append(sent, m) }}

			c.handleMessage(newEvt("ID1", tt.msg))

			if !tt.wantMsg {
				if len(sent) != 0 {
					t.Fatalf("handleMessage() sent %d msgs, want 0: %+v", len(sent), sent)
				}
				return
			}
			if len(sent) != 1 {
				t.Fatalf("handleMessage() sent %d msgs, want 1", len(sent))
			}
			nm, ok := sent[0].(core.NewMessage)
			if !ok {
				t.Fatalf("handleMessage() sent %T, want core.NewMessage", sent[0])
			}
			if nm.Message.Content != "hi" || nm.Message.ChatJID != group.String() {
				t.Errorf("handleMessage() message = %+v", nm.Message)
			}
		})
	}
}
