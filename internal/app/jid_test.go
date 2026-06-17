package app

import (
	"context"
	"testing"
	"time"

	"github.com/watui/watui/internal/theme"
)

type fakeWAAlt struct {
	fakeWA
	alts map[string]string
}

func (f fakeWAAlt) AltChatJID(jid string) string {
	if f.alts == nil {
		return ""
	}
	return f.alts[jid]
}

func TestResolveConversationJIDUsesAlt(t *testing.T) {
	pn := "1234567890@s.whatsapp.net"
	lid := "998877665544@lid"

	m, _ := newTestModel(t)
	m.wa = fakeWAAlt{alts: map[string]string{lid: pn, pn: lid}}
	m.conversations[pn] = theme.Conversation{JID: pn, Name: "Alice"}

	if got := m.resolveConversationJID(lid); got != pn {
		t.Fatalf("resolveConversationJID(%q) = %q, want %q", lid, got, pn)
	}
}

func TestHandleNewMessageResolvesLIDToExistingConversation(t *testing.T) {
	pn := "1234567890@s.whatsapp.net"
	lid := "998877665544@lid"

	m, s := newTestModel(t)
	m.wa = fakeWAAlt{alts: map[string]string{lid: pn, pn: lid}}
	_ = s.UpsertConversation(context.Background(), theme.Conversation{JID: pn, Name: "Alice"})
	m.conversations[pn] = theme.Conversation{JID: pn, Name: "Alice"}
	m.chatView.SetChat(pn, false, nil)

	m, _ = m.handleNewMessage(theme.Message{
		ID:        "live1",
		ChatJID:   lid,
		Content:   "hello via lid",
		Timestamp: time.Unix(500, 0),
	})

	if got := m.chatMessages[pn]; len(got) != 1 || got[0].ID != "live1" {
		t.Fatalf("chatMessages[pn] = %v, want live1 under phone JID", msgIDs(got))
	}
	if _, ok := m.chatMessages[lid]; ok {
		t.Fatalf("message should not remain cached under LID key")
	}
	conv := m.conversations[pn]
	if conv.LastMessage != "hello via lid" {
		t.Fatalf("preview = %q, want hello via lid", conv.LastMessage)
	}
}

func TestMergeAliasChatCache(t *testing.T) {
	pn := "1234567890@s.whatsapp.net"
	lid := "998877665544@lid"

	m, _ := newTestModel(t)
	m.wa = fakeWAAlt{alts: map[string]string{lid: pn, pn: lid}}
	m.chatMessages[lid] = []theme.Message{{ID: "m1", ChatJID: lid, Timestamp: time.Unix(1, 0)}}
	m.chatMessages[pn] = []theme.Message{{ID: "m2", ChatJID: pn, Timestamp: time.Unix(2, 0)}}

	m.mergeAliasChatCache(pn)

	if len(m.chatMessages[lid]) != 0 {
		t.Fatalf("alias cache should be cleared, still have %d msgs", len(m.chatMessages[lid]))
	}
	got := msgIDs(m.chatMessages[pn])
	if len(got) != 2 || got[0] != "m1" || got[1] != "m2" {
		t.Fatalf("merged = %v, want [m1 m2]", got)
	}
}