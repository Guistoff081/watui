package store

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/watui/watui/internal/theme"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedConversation(t *testing.T, s *Store, jid string) {
	t.Helper()
	if err := s.UpsertConversation(context.Background(), theme.Conversation{JID: jid, Name: "Test"}); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
}

func TestGetMessagesReturnsRecentInAscendingOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"
	seedConversation(t, s, jid)

	base := time.Unix(1_000_000, 0)
	var msgs []theme.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, theme.Message{
			ID:        fmt.Sprintf("m%d", i),
			ChatJID:   jid,
			Content:   fmt.Sprintf("c%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Status:    "received",
		})
	}
	if err := s.InsertMessages(ctx, msgs); err != nil {
		t.Fatalf("InsertMessages() error = %v", err)
	}

	got, err := s.GetMessages(ctx, jid, 5)
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}
	// Want the 5 most recent (m5..m9), oldest-first.
	want := []string{"m5", "m6", "m7", "m8", "m9"}
	if len(got) != len(want) {
		t.Fatalf("GetMessages() len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("got[%d].ID = %s, want %s", i, got[i].ID, w)
		}
	}
}

func TestInsertMessageIgnoresDuplicates(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "g@g.us"
	seedConversation(t, s, jid)

	m := theme.Message{ID: "x", ChatJID: jid, Content: "first", Timestamp: time.Unix(100, 0)}
	if err := s.InsertMessage(ctx, m); err != nil {
		t.Fatalf("InsertMessage() error = %v", err)
	}
	dup := m
	dup.Content = "second"
	if err := s.InsertMessage(ctx, dup); err != nil {
		t.Fatalf("InsertMessage() dup error = %v", err)
	}

	got, err := s.GetMessages(ctx, jid, 10)
	if err != nil {
		t.Fatalf("GetMessages() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetMessages() len = %d, want 1 (dup ignored)", len(got))
	}
	if got[0].Content != "first" {
		t.Errorf("duplicate overwrote content = %q, want %q", got[0].Content, "first")
	}
}

func TestUpdateMessageStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"
	seedConversation(t, s, jid)

	m := theme.Message{ID: "m1", ChatJID: jid, Content: "hi", Timestamp: time.Unix(1, 0), Status: "sending"}
	if err := s.InsertMessage(ctx, m); err != nil {
		t.Fatalf("InsertMessage() error = %v", err)
	}
	if err := s.UpdateMessageStatus(ctx, "m1", "read"); err != nil {
		t.Fatalf("UpdateMessageStatus() error = %v", err)
	}

	got, _ := s.GetMessages(ctx, jid, 10)
	if len(got) != 1 || got[0].Status != "read" {
		t.Fatalf("status = %q, want read", got[0].Status)
	}
}

func TestGetMessagesBeforeReturnsAscending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"
	seedConversation(t, s, jid)

	base := time.Unix(1_000_000, 0)
	var msgs []theme.Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, theme.Message{
			ID:        fmt.Sprintf("m%d", i),
			ChatJID:   jid,
			Timestamp: base.Add(time.Duration(i) * time.Minute),
		})
	}
	if err := s.InsertMessages(ctx, msgs); err != nil {
		t.Fatalf("InsertMessages() error = %v", err)
	}

	before := base.Add(3 * time.Minute) // strictly before m3
	got, err := s.GetMessagesBefore(ctx, jid, before, 10)
	if err != nil {
		t.Fatalf("GetMessagesBefore() error = %v", err)
	}
	want := []string{"m0", "m1", "m2"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("got[%d].ID = %s, want %s", i, got[i].ID, w)
		}
	}
}

func TestUpsertConversationKeepsNameAndAdvancesPreview(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"

	first := theme.Conversation{JID: jid, Name: "Alice", LastMessage: "old", LastMsgTime: time.Unix(100, 0)}
	if err := s.UpsertConversation(ctx, first); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	// Empty name + newer message should keep the name but advance the preview.
	update := theme.Conversation{JID: jid, Name: "", LastMessage: "new", LastMsgTime: time.Unix(200, 0)}
	if err := s.UpsertConversation(ctx, update); err != nil {
		t.Fatalf("UpsertConversation() update error = %v", err)
	}

	convs, err := s.GetAllConversations(ctx)
	if err != nil {
		t.Fatalf("GetAllConversations() error = %v", err)
	}
	if len(convs) != 1 {
		t.Fatalf("len = %d, want 1", len(convs))
	}
	if convs[0].Name != "Alice" {
		t.Errorf("name = %q, want Alice (empty update must not clobber)", convs[0].Name)
	}
	if convs[0].LastMessage != "new" {
		t.Errorf("last message = %q, want new", convs[0].LastMessage)
	}
}

func TestUpsertConversationDoesNotRegressPreviewWithOlder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"

	if err := s.UpsertConversation(ctx, theme.Conversation{JID: jid, LastMessage: "new", LastMsgTime: time.Unix(200, 0)}); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	if err := s.UpsertConversation(ctx, theme.Conversation{JID: jid, LastMessage: "old", LastMsgTime: time.Unix(100, 0)}); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	convs, _ := s.GetAllConversations(ctx)
	if convs[0].LastMessage != "new" {
		t.Errorf("older update regressed preview to %q", convs[0].LastMessage)
	}
}

func TestClearUnread(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	jid := "123@s.whatsapp.net"

	if err := s.UpsertConversation(ctx, theme.Conversation{JID: jid, UnreadCount: 5, LastMsgTime: time.Unix(1, 0)}); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	if err := s.ClearUnread(ctx, jid); err != nil {
		t.Fatalf("ClearUnread() error = %v", err)
	}
	convs, _ := s.GetAllConversations(ctx)
	if convs[0].UnreadCount != 0 {
		t.Errorf("unread = %d, want 0", convs[0].UnreadCount)
	}
}

func TestGetAllConversationsOrdersPinnedThenRecent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.UpsertConversation(ctx, theme.Conversation{JID: "a", LastMsgTime: time.Unix(300, 0)})
	_ = s.UpsertConversation(ctx, theme.Conversation{JID: "b", LastMsgTime: time.Unix(100, 0), IsPinned: true})
	_ = s.UpsertConversation(ctx, theme.Conversation{JID: "c", LastMsgTime: time.Unix(200, 0)})

	convs, err := s.GetAllConversations(ctx)
	if err != nil {
		t.Fatalf("GetAllConversations() error = %v", err)
	}
	// Pinned "b" first, then by recency: a (300) then c (200).
	want := []string{"b", "a", "c"}
	for i, w := range want {
		if convs[i].JID != w {
			t.Errorf("convs[%d].JID = %s, want %s", i, convs[i].JID, w)
		}
	}
}
