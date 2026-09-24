package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/core"
)

// persistEffects queues eff's store writes as one ordered unit: conversations
// first, since messages.chat_jid references them, then messages, then unread
// resets.
func (m *Model) persistEffects(eff core.Effects) tea.Cmd {
	var ops []storeOp
	for _, conv := range eff.Conversations {
		ops = append(ops, storeOp{"save conversation", func(ctx context.Context, s Store) error {
			return s.UpsertConversation(ctx, conv)
		}})
	}
	if len(eff.Messages) > 0 {
		msgs := append([]core.Message(nil), eff.Messages...)
		ops = append(ops, storeOp{"save messages", func(ctx context.Context, s Store) error {
			return s.InsertMessages(ctx, msgs)
		}})
	}
	for _, jid := range eff.ClearUnread {
		ops = append(ops, storeOp{"clear unread", func(ctx context.Context, s Store) error {
			return s.ClearUnread(ctx, jid)
		}})
	}
	return m.writes.enqueue(ops...)
}

// reportStoreErr logs a persistence error and shows it briefly in the status
// bar. It never changes the app state.
func (m *Model) reportStoreErr(err error) tea.Cmd {
	m.log.Error(err, "store")
	m.statusBar.SetMessage("Storage error: " + err.Error())
	return m.clearStatusAfter(statusTimeout)
}

// storeOp is one queued store write; name prefixes its error.
type storeOp struct {
	name string
	run  func(ctx context.Context, s Store) error
}

// writeQueue runs store writes off the Update goroutine while keeping the
// order Update produced them in. Bubble Tea runs commands concurrently and in
// no particular order, so commands do not carry their own writes: Update
// appends to the queue and returns a flush command, and any flush drains
// everything queued so far, in order, one flush at a time. Writes to the same
// row therefore never overtake each other (e.g. a status update cannot land
// before the insert of the message it refers to).
type writeQueue struct {
	store Store

	mu      sync.Mutex // guards pending
	pending []storeOp

	flushMu sync.Mutex // serializes flushes
}

// enqueue appends ops and returns the command that flushes them, or nil when
// there is nothing to write.
func (q *writeQueue) enqueue(ops ...storeOp) tea.Cmd {
	if len(ops) == 0 {
		return nil
	}
	q.mu.Lock()
	q.pending = append(q.pending, ops...)
	q.mu.Unlock()
	return q.flushCmd
}

// flush runs every pending op and joins their errors. A failed op does not
// stop the ones after it.
func (q *writeQueue) flush() error {
	q.flushMu.Lock()
	defer q.flushMu.Unlock()

	q.mu.Lock()
	ops := q.pending
	q.pending = nil
	q.mu.Unlock()

	var errs []error
	for _, op := range ops {
		if err := op.run(context.Background(), q.store); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", op.name, err))
		}
	}
	return errors.Join(errs...)
}

func (q *writeQueue) flushCmd() tea.Msg {
	if err := q.flush(); err != nil {
		return persistErrMsg{Err: err}
	}
	return nil
}
