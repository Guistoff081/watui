package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/watui/watui/internal/core"
)

const (
	testPN  = "1234567890@s.whatsapp.net"
	testLID = "998877665544@lid"
)

func pnLIDAlts() map[string]string { return map[string]string{testLID: testPN, testPN: testLID} }

// The engine resolves aliases through the WAClient it was built with.
func TestHandleNewMessageResolvesLIDToExistingConversation(t *testing.T) {
	m, s, _ := newRecordingModel(t, pnLIDAlts())
	seedConv(t, &m, core.Conversation{JID: testPN, Name: "Alice"})
	m.chatView.SetChat(testPN, false, nil)

	m = send(t, m, core.NewMessage{Message: core.Message{
		ID:        "live1",
		ChatJID:   testLID,
		Content:   "hello via lid",
		Timestamp: time.Unix(500, 0),
	}})

	if got := msgIDs(m.chats.Messages(testPN)); !reflect.DeepEqual(got, []string{"live1"}) {
		t.Fatalf("Messages(pn) = %v, want live1 under phone JID", got)
	}
	if got := conv(m, testPN).LastMessage; got != "hello via lid" {
		t.Fatalf("preview = %q, want hello via lid", got)
	}
	// Persisted under the canonical JID so it is found when the chat reopens.
	stored, err := s.GetMessagesForChats(context.Background(), []string{testPN}, 10)
	if err != nil || len(stored) != 1 || stored[0].ID != "live1" {
		t.Fatalf("stored = %v, %v; want live1 under pn", msgIDs(stored), err)
	}
}

func TestSelectChatLoadsStoredAliasHistory(t *testing.T) {
	m, _, _ := newRecordingModel(t, pnLIDAlts())
	seedConv(t, &m, core.Conversation{JID: testPN})
	// A stale store row keyed by the LID (history synced before the mapping was known).
	if err := m.store.UpsertConversation(context.Background(), core.Conversation{JID: testLID}); err != nil {
		t.Fatal(err)
	}
	seedStored(t, &m, []core.Message{
		{ID: "p1", ChatJID: testPN, Timestamp: time.Unix(100, 0)},
		{ID: "l1", ChatJID: testLID, Timestamp: time.Unix(200, 0)},
	})

	m = open(t, m, testPN)

	if got, want := msgIDs(m.chats.Messages(testPN)), []string{"p1", "l1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Messages(pn) = %v, want %v (history under both JIDs)", got, want)
	}
}
