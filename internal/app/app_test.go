package app

import (
	"testing"
	"time"

	"github.com/watui/watui/internal/theme"
)

func ids(msgs []theme.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.ID
	}
	return out
}

func TestMergeMessagesDedupesAndSorts(t *testing.T) {
	t1 := time.Unix(100, 0)
	t2 := time.Unix(200, 0)
	t3 := time.Unix(300, 0)

	cache := []theme.Message{
		{ID: "c", Timestamp: t3},
		{ID: "b", Timestamp: t2, Content: "cache-b"},
	}
	stored := []theme.Message{
		{ID: "a", Timestamp: t1},
		{ID: "b", Timestamp: t2, Content: "stored-b"},
	}

	out := mergeMessages(cache, stored)
	if got := ids(out); len(got) != 3 {
		t.Fatalf("merged len = %d (%v), want 3", len(got), got)
	}
	if out[0].ID != "a" || out[1].ID != "b" || out[2].ID != "c" {
		t.Errorf("order = %v, want [a b c]", ids(out))
	}
	if out[1].Content != "cache-b" {
		t.Errorf("dedup kept %q, want cache-b (earlier list wins)", out[1].Content)
	}
}

func TestMergeMessagesEmpty(t *testing.T) {
	if out := mergeMessages(nil, nil); out != nil {
		t.Errorf("mergeMessages(nil, nil) = %v, want nil", out)
	}
}

func TestInsertMessageSorted(t *testing.T) {
	msgs := []theme.Message{
		{ID: "a", Timestamp: time.Unix(100, 0)},
		{ID: "c", Timestamp: time.Unix(300, 0)},
	}

	msgs = insertMessageSorted(msgs, theme.Message{ID: "b", Timestamp: time.Unix(200, 0)})
	if got := ids(msgs); got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("after middle insert = %v, want [a b c]", got)
	}

	msgs = insertMessageSorted(msgs, theme.Message{ID: "z", Timestamp: time.Unix(50, 0)})
	if msgs[0].ID != "z" {
		t.Errorf("oldest insert: first = %s, want z", msgs[0].ID)
	}

	msgs = insertMessageSorted(msgs, theme.Message{ID: "n", Timestamp: time.Unix(400, 0)})
	if msgs[len(msgs)-1].ID != "n" {
		t.Errorf("newest insert: last = %s, want n", msgs[len(msgs)-1].ID)
	}
}

func TestInsertMessageSortedIntoEmpty(t *testing.T) {
	msgs := insertMessageSorted(nil, theme.Message{ID: "a", Timestamp: time.Unix(1, 0)})
	if len(msgs) != 1 || msgs[0].ID != "a" {
		t.Errorf("insert into empty = %v, want [a]", ids(msgs))
	}
}
