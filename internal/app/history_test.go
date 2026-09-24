package app

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/watui/watui/internal/core"
)

// When the local store has nothing older, the phone is asked (under every
// alias of the chat: LID-migrated chats are keyed by LID on the phone). A
// repeat while the request is pending only reports that it is still waiting;
// after the wait window it is sent again. An on-demand page lands on top.
func TestOlderHistoryFallsBackToPhone(t *testing.T) {
	jid, lid := "558185724594@s.whatsapp.net", "75553390510081@lid"
	m, s, wa := newRecordingModel(t, map[string]string{jid: lid, lid: jid})
	clock := time.Unix(10_000, 0)
	m.now = func() time.Time { return clock }
	m.statusBar.SetWidth(200)
	_ = s.UpsertConversation(context.Background(), core.Conversation{JID: jid})
	_ = s.InsertMessages(context.Background(), []core.Message{
		{ID: "a", ChatJID: jid, Content: "oiii", Timestamp: time.Unix(1000, 0)},
		{ID: "b", ChatJID: jid, Content: "tudo bem?", Timestamp: time.Unix(1100, 0)},
	})
	m.chats.Load([]core.Conversation{{JID: jid}})
	m = open(t, m, jid)

	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid}) // local store exhausted
	if want := []string{"a@" + jid, "a@" + lid}; !reflect.DeepEqual(wa.historyReqs, want) {
		t.Fatalf("history requests = %v, want %v", wa.historyReqs, want)
	}
	if !strings.Contains(m.statusBar.View(), "phone") {
		t.Errorf("status = %q, want a note that the phone was asked", m.statusBar.View())
	}

	// Pending: no new request and not the end of history.
	clock = clock.Add(10 * time.Second)
	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid})
	if len(wa.historyReqs) != 2 || m.chatView.NoMoreMessages() {
		t.Fatalf("pending repeat: requests = %v, noMore = %v; want no resend and not ended",
			wa.historyReqs, m.chatView.NoMoreMessages())
	}
	if !strings.Contains(m.statusBar.View(), "waiting") {
		t.Errorf("status = %q, want still waiting", m.statusBar.View())
	}

	// After the wait window the request is sent again.
	clock = clock.Add(historyRetryAfter)
	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid})
	if len(wa.historyReqs) != 4 {
		t.Fatalf("history requests = %v, want a resend after the wait window", wa.historyReqs)
	}

	m = send(t, m, core.MessagesLoaded{ChatJID: mustJID(t, jid), Messages: []core.Message{
		{ID: "o1", ChatJID: jid, Content: "velha", Timestamp: time.Unix(500, 0)},
	}})
	if got := m.chatView.Messages(); len(got) != 3 || got[0].ID != "o1" {
		t.Fatalf("view messages = %v, want the on-demand page prepended", msgIDs(got))
	}

	// The next exhaustion anchors on the new oldest message right away.
	m = send(t, m, olderMessagesLoadedMsg{ChatJID: jid})
	if n := len(wa.historyReqs); n != 6 || wa.historyReqs[4] != "o1@"+jid {
		t.Fatalf("history requests = %v, want new requests anchored on o1", wa.historyReqs)
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

// Unnamed 1:1 chats the address book can't name are looked up as businesses;
// chats named by contacts are not queried, and verified names are applied.
func TestContactNamesQueryVerifiedBusinessNames(t *testing.T) {
	wa := &recordingWA{
		contactNames:  map[string]string{"c@s.whatsapp.net": "Carla"},
		verifiedNames: map[string]string{"b@s.whatsapp.net": "Jeitto"},
	}
	m := testModel(wa, newTestStore(t))
	m.chats.Load([]core.Conversation{{JID: "b@s.whatsapp.net"}, {JID: "c@s.whatsapp.net"}, {JID: "g@g.us"}})

	m = send(t, m, core.HistorySyncComplete{})

	if want := [][]string{{"b@s.whatsapp.net"}}; !reflect.DeepEqual(wa.verifiedQueries, want) {
		t.Errorf("verified-name queries = %v, want %v", wa.verifiedQueries, want)
	}
	if conv, _ := m.chats.Conversation("b@s.whatsapp.net"); conv.Name != "Jeitto" {
		t.Errorf("business chat name = %q, want Jeitto", conv.Name)
	}
	if conv, _ := m.chats.Conversation("c@s.whatsapp.net"); conv.Name != "Carla" {
		t.Errorf("contact chat name = %q, want Carla", conv.Name)
	}
}

// On a normal start (no history sync) names must be resolved after the
// stored conversations are loaded, or the unnamed list is still empty.
func TestStartupLooksUpBusinessNamesAfterConversationsLoad(t *testing.T) {
	s := newTestStore(t)
	_ = s.UpsertConversation(context.Background(), core.Conversation{JID: "b@s.whatsapp.net"})
	wa := &recordingWA{verifiedNames: map[string]string{"b@s.whatsapp.net": "Jeitto"}}
	m := testModel(wa, s)

	m = send(t, m, core.Connected{})

	if conv, _ := m.chats.Conversation("b@s.whatsapp.net"); conv.Name != "Jeitto" {
		t.Errorf("after startup name = %q, want Jeitto (queries: %v)", conv.Name, wa.verifiedQueries)
	}
}
