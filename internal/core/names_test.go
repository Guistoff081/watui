package core

import "testing"

func TestDisplayName(t *testing.T) {
	tests := []struct {
		conv Conversation
		want string
	}{
		{Conversation{JID: "5511999998888@s.whatsapp.net", Name: "Ana"}, "Ana"},
		{Conversation{JID: "5511999998888@s.whatsapp.net"}, "+55 11 99999-8888"},
		{Conversation{JID: "5511999998888@s.whatsapp.net", Name: "5511999998888@s.whatsapp.net"}, "+55 11 99999-8888"},
		{Conversation{JID: "0@s.whatsapp.net"}, "WhatsApp"},
		{Conversation{JID: "12036302@g.us"}, "12036302@g.us"},
		{Conversation{JID: "998877@lid"}, "998877@lid"},
	}
	for _, tt := range tests {
		if got := DisplayName(tt.conv); got != tt.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", tt.conv, got, tt.want)
		}
	}
}

func TestDisplayNameFormatsBrazilianNumbers(t *testing.T) {
	tests := map[string]string{
		"5581986072503@s.whatsapp.net": "+55 81 98607-2503", // mobile, 9 digits
		"551151948658@s.whatsapp.net":  "+55 11 5194-8658",  // landline, 8 digits
		"14155550100@s.whatsapp.net":   "+14155550100",      // other countries: digits only
	}
	for jid, want := range tests {
		if got := DisplayName(Conversation{JID: jid}); got != want {
			t.Errorf("DisplayName(%s) = %q, want %q", jid, got, want)
		}
	}
}

func TestNeedsPoster(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{"animated sticker", Message{MediaType: "sticker", IsAnimated: true}, true},
		{"static sticker", Message{MediaType: "sticker"}, false},
		{"gif without thumbnail", Message{MediaType: "gif"}, true},
		{"gif with thumbnail", Message{MediaType: "gif", Thumbnail: []byte{1}}, false},
		{"video without thumbnail", Message{MediaType: "video"}, true},
		{"image", Message{MediaType: "image"}, false},
		{"text", Message{}, false},
	}
	for _, tt := range tests {
		if got := tt.msg.NeedsPoster(); got != tt.want {
			t.Errorf("%s: NeedsPoster() = %v, want %v", tt.name, got, tt.want)
		}
	}
	if got := PosterPath("/cache/a.webp"); got != "/cache/a.webp.poster.png" {
		t.Errorf("PosterPath() = %q", got)
	}
}
