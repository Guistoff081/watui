package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// testModel builds a Model whose timers yield delayedMsg immediately instead
// of sleeping, so run() can settle a model without waiting on them.
func testModel(wa WAClient, s Store) Model {
	m := NewModel(wa, s, "test", nil)
	m.delay = func(d time.Duration, msg tea.Msg) tea.Cmd {
		return func() tea.Msg { return delayedMsg{d: d, msg: msg} }
	}
	return m
}

// delayedMsg stands in for a timer's message in tests; run() never delivers it.
type delayedMsg struct {
	d   time.Duration
	msg tea.Msg
}

// slowCmd bounds how long collect waits for a command. The app's own commands
// return at once in tests (fakes, temp SQLite, delayedMsg timers); only UI
// timers from bubbles components (e.g. the textarea cursor blink) sleep, and
// those are dropped.
const slowCmd = 250 * time.Millisecond

// collect executes cmd (expanding batches) and returns the messages it
// yields, without feeding them back to a model. Commands of one batch run
// concurrently, as in Bubble Tea, but messages are returned in batch order.
func collect(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	frontier := []tea.Cmd{cmd}
	for len(frontier) > 0 {
		results := make([]chan tea.Msg, len(frontier))
		for i, c := range frontier {
			results[i] = make(chan tea.Msg, 1)
			if c == nil {
				results[i] <- nil
				continue
			}
			go func(c tea.Cmd, ch chan<- tea.Msg) { ch <- c() }(c, results[i])
		}
		deadline := time.After(slowCmd)
		expired := false
		var next []tea.Cmd
		for _, ch := range results {
			var msg tea.Msg
			if expired {
				select {
				case msg = <-ch:
				default:
					continue // a UI timer; never delivered
				}
			} else {
				select {
				case msg = <-ch:
				case <-deadline:
					expired = true
					select {
					case msg = <-ch:
					default:
						continue
					}
				}
			}
			switch msg := msg.(type) {
			case nil:
			case tea.BatchMsg:
				next = append(next, msg...)
			default:
				out = append(out, msg)
			}
		}
		frontier = next
	}
	return out
}

// run executes cmd and every command it leads to, feeding each message back
// through Update, and returns the settled model. Timers (delayedMsg) are not
// delivered.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	pending := []tea.Cmd{cmd}
	for i := 0; len(pending) > 0; i++ {
		if i > 1000 {
			t.Fatal("run: commands did not settle")
		}
		var next []tea.Cmd
		for _, msg := range collect(t, tea.Batch(pending...)) {
			if _, ok := msg.(delayedMsg); ok {
				continue
			}
			var c tea.Cmd
			m, c = update(t, m, msg)
			next = append(next, c)
		}
		pending = next
	}
	return m
}

// send runs msg through Update and settles the commands it returns.
func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	m, cmd := update(t, m, msg)
	return run(t, m, cmd)
}

// open selects jid and settles the resulting load.
func open(t *testing.T, m Model, jid string) Model {
	t.Helper()
	m, cmd := m.selectChat(jid)
	return run(t, m, cmd)
}
