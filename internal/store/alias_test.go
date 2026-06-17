package store

import (
	"context"
	"testing"
	"time"

	"github.com/watui/watui/internal/theme"
)

func TestGetMessagesForChatsMergesAliases(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	pn := "123@s.whatsapp.net"
	lid := "999@lid"
	// Legacy DB rows may exist under both PN and LID conversation keys.
	seedConversation(t, s, pn)
	seedConversation(t, s, lid)

	if err := s.InsertMessages(ctx, []theme.Message{
		{ID: "pn1", ChatJID: pn, Content: "from phone", Timestamp: time.Unix(100, 0)},
		{ID: "lid1", ChatJID: lid, Content: "from lid", Timestamp: time.Unix(200, 0)},
	}); err != nil {
		t.Fatalf("InsertMessages() error = %v", err)
	}

	msgs, err := s.GetMessagesForChats(ctx, []string{pn, lid}, 10)
	if err != nil {
		t.Fatalf("GetMessagesForChats() error = %v", err)
	}
	if len(msgs) != 2 || msgs[0].ID != "pn1" || msgs[1].ID != "lid1" {
		t.Fatalf("msgs = %+v, want ascending pn1 then lid1", msgs)
	}
}