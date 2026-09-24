package core

import "testing"

func TestDisplayName(t *testing.T) {
	tests := []struct {
		conv Conversation
		want string
	}{
		{Conversation{JID: "5511999998888@s.whatsapp.net", Name: "Ana"}, "Ana"},
		{Conversation{JID: "5511999998888@s.whatsapp.net"}, "+5511999998888"},
		{Conversation{JID: "5511999998888@s.whatsapp.net", Name: "5511999998888@s.whatsapp.net"}, "+5511999998888"},
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
