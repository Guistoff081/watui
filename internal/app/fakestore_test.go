package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/watui/watui/internal/core"
)

// fakeStore is an in-memory Store that records calls in order and can be told
// to fail specific operations.
type fakeStore struct {
	mu    sync.Mutex
	calls []string
	errs  map[string]error // keyed by operation name, e.g. "InsertMessages"

	convs    []core.Conversation
	messages []core.Message
}

func (f *fakeStore) record(op, arg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+" "+arg)
	return f.errs[op]
}

func (f *fakeStore) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeStore) GetAllConversations(context.Context) ([]core.Conversation, error) {
	if err := f.record("GetAllConversations", ""); err != nil {
		return nil, err
	}
	return f.convs, nil
}

func (f *fakeStore) UpsertConversation(_ context.Context, c core.Conversation) error {
	return f.record("UpsertConversation", c.JID)
}

func (f *fakeStore) ClearUnread(_ context.Context, jid string) error {
	return f.record("ClearUnread", jid)
}

func (f *fakeStore) InsertMessages(_ context.Context, msgs []core.Message) error {
	return f.record("InsertMessages", strings.Join(msgIDs(msgs), ","))
}

func (f *fakeStore) GetMessagesForChats(_ context.Context, jids []string, limit int) ([]core.Message, error) {
	if err := f.record("GetMessagesForChats", strings.Join(jids, ",")); err != nil {
		return nil, err
	}
	return f.messages, nil
}

func (f *fakeStore) GetMessagesBefore(_ context.Context, jid string, before time.Time, limit int) ([]core.Message, error) {
	if err := f.record("GetMessagesBefore", jid); err != nil {
		return nil, err
	}
	return nil, nil
}

func (f *fakeStore) UpdateMessageStatus(_ context.Context, id, status string) error {
	return f.record("UpdateMessageStatus", fmt.Sprintf("%s=%s", id, status))
}

func (f *fakeStore) UpdateMessageMediaPath(_ context.Context, jid, id, path string) error {
	return f.record("UpdateMessageMediaPath", id)
}
