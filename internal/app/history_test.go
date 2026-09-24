package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
)

// When the local store has nothing older, the phone is asked once per anchor;
// asking again with the same oldest message means the phone had nothing, so
// the view stops trying. An on-demand page then lands on top of the open chat.
func TestOlderHistoryFallsBackToPhone(t *testing.T) {
	m, s, wa := newRecordingModel(t)
	m.statusBar.SetWidth(200)
	jid := "558185724594@s.whatsapp.net"
	_ = s.UpsertConversation(context.Background(), core.Conversation{JID: jid})
	_ = s.InsertMessages(context.Background(), []core.Message{
		{ID: "a", ChatJID: jid, Content: "oiii", Timestamp: time.Unix(1000, 0)},
		{ID: "b", ChatJID: jid, Content: "tudo bem?", Timestamp: time.Unix(1100, 0)},
	})
	m.chats.Load([]core.Conversation{{JID: jid}})
	m = open(t, m, jid)

	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid}) // local store exhausted
	if len(wa.historyReqs) != 1 || wa.historyReqs[0] != "a" {
		t.Fatalf("history requests = %v, want one anchored on the oldest message a", wa.historyReqs)
	}
	if m.chatView.NoMoreMessages() {
		t.Error("view marked as end of history while the phone was asked")
	}
	if !strings.Contains(m.statusBar.View(), "phone") {
		t.Errorf("status = %q, want a note that the phone was asked", m.statusBar.View())
	}

	parsed := mustJID(t, jid)
	m = send(t, m, core.MessagesLoaded{ChatJID: parsed, Messages: []core.Message{
		{ID: "o1", ChatJID: jid, Content: "velha", Timestamp: time.Unix(500, 0)},
	}})
	if got := m.chatView.Messages(); len(got) != 3 || got[0].ID != "o1" {
		t.Fatalf("view messages = %v, want the on-demand page prepended", msgIDs(got))
	}

	// Phone answered; the next exhaustion anchors on the new oldest message.
	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid})
	if len(wa.historyReqs) != 2 || wa.historyReqs[1] != "o1" {
		t.Fatalf("history requests = %v, want a second one anchored on o1", wa.historyReqs)
	}
	// Nothing came back: same anchor again means the end.
	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid})
	if len(wa.historyReqs) != 2 {
		t.Errorf("history requests = %v, want no repeat for the same anchor", wa.historyReqs)
	}
	if !m.chatView.NoMoreMessages() {
		t.Error("view should stop loading after the phone had nothing older")
	}
}

func TestHistoryRequestFailedShowsStatus(t *testing.T) {
	m, _ := newTestModel(t)
	m.statusBar.SetWidth(200)
	m = send(t, m, historyRequestFailedMsg{Err: context.DeadlineExceeded})
	if !strings.Contains(m.statusBar.View(), "older messages") {
		t.Errorf("status = %q, want the request failure", m.statusBar.View())
	}
}

func mustJID(t *testing.T, s string) types.JID {
	t.Helper()
	j, err := types.ParseJID(s)
	if err != nil {
		t.Fatal(err)
	}
	return j
}
