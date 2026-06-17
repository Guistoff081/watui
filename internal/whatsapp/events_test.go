package whatsapp

import (
	"testing"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"google.golang.org/protobuf/proto"
)

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name string
		msg  *waProto.Message
		want string
	}{
		{"nil", nil, ""},
		{"empty", &waProto.Message{}, ""},
		{"conversation", &waProto.Message{Conversation: proto.String("hello")}, "hello"},
		{
			"extended text",
			&waProto.Message{ExtendedTextMessage: &waProto.ExtendedTextMessage{Text: proto.String("ext")}},
			"ext",
		},
		{
			"image with caption",
			&waProto.Message{ImageMessage: &waProto.ImageMessage{Caption: proto.String("cap")}},
			"[image] cap",
		},
		{"image no caption", &waProto.Message{ImageMessage: &waProto.ImageMessage{}}, "[image]"},
		{
			"voice (ptt)",
			&waProto.Message{AudioMessage: &waProto.AudioMessage{PTT: proto.Bool(true)}},
			"[voice message]",
		},
		{"audio", &waProto.Message{AudioMessage: &waProto.AudioMessage{}}, "[audio]"},
		{
			"document",
			&waProto.Message{DocumentMessage: &waProto.DocumentMessage{FileName: proto.String("a.pdf")}},
			"[file] a.pdf",
		},
		{"sticker", &waProto.Message{StickerMessage: &waProto.StickerMessage{}}, "[sticker]"},
		{"location", &waProto.Message{LocationMessage: &waProto.LocationMessage{}}, "[location]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextContent(tt.msg); got != tt.want {
				t.Errorf("extractTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectMIME(t *testing.T) {
	if got := detectMIME("photo.png", nil); got == "" {
		t.Error("detectMIME() for .png returned empty")
	}
	// Unknown extension falls back to content sniffing.
	if got := detectMIME("noext", []byte("%PDF-1.4\n")); got == "" {
		t.Error("detectMIME() sniff returned empty")
	}
}

func TestBestContactName(t *testing.T) {
	cases := []struct {
		full, push, business, want string
	}{
		{"Full", "Push", "Biz", "Full"},
		{"", "Push", "Biz", "Push"},
		{"", "", "Biz", "Biz"},
		{"", "", "", ""},
	}
	for _, c := range cases {
		if got := bestContactName(c.full, c.push, c.business); got != c.want {
			t.Errorf("bestContactName(%q,%q,%q) = %q, want %q", c.full, c.push, c.business, got, c.want)
		}
	}
}
