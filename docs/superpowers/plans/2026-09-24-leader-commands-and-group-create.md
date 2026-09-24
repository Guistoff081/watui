# Leader-key commands and group creation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Space-leader commands with a which-key popup (Part 1) and creating WhatsApp groups from a `Space g c` form (Part 2).

**Architecture:** A pure `internal/ui/commands` registry resolves key sequences; the app keeps the leader prefix and an `overlay` slot that receives keys first and is drawn over the body without changing frame height. `internal/ui/whichkey` and `internal/ui/groupform` are overlay views; `whatsapp.Client.CreateGroup` wraps whatsmeow's `CreateGroup`.

**Tech Stack:** Go 1.26, Bubble Tea v1 (`github.com/charmbracelet/bubbletea`), lipgloss v1.1, `github.com/charmbracelet/x/ansi`, whatsmeow.

**Spec:** `docs/superpowers/specs/2026-09-24-leader-commands-and-group-create-design.md`

## Global Constraints

- Leader key is Space (`msg.String() == " "`), only in `StateChat` with focus on `PanelChatList` or `PanelMessages`. In `PanelInput` Space types a space.
- Keep direct keys: `j k g G Tab Shift+Tab i Esc Enter`, and input `ctrl+f` / `ctrl+p` / `ctrl+o`.
- `ctrl+c` quits from anywhere, including with the leader or an overlay open.
- Group names: max **25 characters** (runes), non-empty after trim.
- Typed participant numbers: `+`, digits, spaces, dashes, parentheses; **8–15 digits**; become `<digits>@s.whatsapp.net`.
- Frame height must stay exactly the terminal height with any overlay open (existing `TestViewFillsExactlyTerminalHeight` pattern).
- `internal/core` must not import UI packages; `internal/whatsapp` must not import bubbletea.
- CI enforces `test -z "$(gofmt -l ./cmd ./internal)"`, `go vet ./...`, `go test -race ./...`.
- Commits: conventional style, body explains why, trailer:
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`
  `Claude-Session: https://claude.ai/code/session_018s26HtL4ovWLNKhxdfb5kd`

## Review Focus

1. `ctrl+c` while the leader popup or the group form is open must still quit (test in Task 4 and Task 7).
2. A very narrow/short terminal with an overlay open: overlay is clipped, frame height stays exact (test in Task 4 with 40×12).
3. Group name with accents/emoji: the 25 limit counts runes, not bytes (test in Task 6: `"Família Ção 🎉"`).
4. A typed number that equals a known contact's JID must not be added twice (dedupe by JID; test in Task 6).
5. A key after Space that is not a command (e.g. `j`) must not reach the panel underneath (test in Task 4: list cursor unchanged).

## Execution waves (worktrees + stacked PRs)

- **Wave 1 (parallel, each its own worktree off `main`):** agent A = Task 1 then Task 2 (Task 2 imports Task 1's `commands.Option`), agent B = Task 3, agent C = Task 5, agent D = Task 6.
- **Wave 2:** branch `feat/leader-commands` = merge of the A and B branches, then Task 4 on it → **PR A** (base `main`).
- **Wave 3:** branch `feat/group-create` on top of `feat/leader-commands`, merge Tasks 5–6 branches, then Task 7 → **PR B** (base `feat/leader-commands`).

File ownership is disjoint inside each wave, so merges are conflict-free.

---

### Task 1: Command registry (`internal/ui/commands`)

**Files:**
- Create: `internal/ui/commands/commands.go`
- Test: `internal/ui/commands/commands_test.go`

**Interfaces:**
- Produces:
  - `type Command struct { ID, Keys, Desc string }` (`Keys` space-separated, e.g. `"a f"`)
  - `type Group struct { Key, Name string }` (`Key` is a prefix path, e.g. `"a"`)
  - `type Option struct { Key, Label string; IsGroup bool }`
  - `func NewRegistry(cmds []Command, groups []Group) (*Registry, error)`
  - `func (r *Registry) Step(prefix []string, key string) (next []string, cmd *Command, ok bool)`
  - `func (r *Registry) Options(prefix []string) []Option`
  - `func (r *Registry) Title(prefix []string) string` — `"␣"` for root, `"␣ a — attach"` for `["a"]`
  - `func (r *Registry) Commands() []Command` — all, sorted by Keys
  - `type InvokeMsg struct{ ID string }`
  - `func Default() *Registry` (panics if invalid; guarded by a test)
  - IDs: `attach.file`, `attach.audio`, `attach.picker`, `help` (Task 7 adds `group.create`)

- [ ] **Step 1: Write the failing test**

```go
package commands

import (
	"reflect"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := NewRegistry(
		[]Command{
			{ID: "attach.file", Keys: "a f", Desc: "Attach file"},
			{ID: "attach.audio", Keys: "a a", Desc: "Send audio"},
			{ID: "help", Keys: "?", Desc: "Keybindings"},
		},
		[]Group{{Key: "a", Name: "attach"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestStep(t *testing.T) {
	r := testRegistry(t)

	next, cmd, ok := r.Step(nil, "a")
	if !ok || cmd != nil || !reflect.DeepEqual(next, []string{"a"}) {
		t.Fatalf("Step(root, a) = %v, %v, %v; want prefix [a]", next, cmd, ok)
	}
	_, cmd, ok = r.Step(next, "f")
	if !ok || cmd == nil || cmd.ID != "attach.file" {
		t.Fatalf("Step([a], f) = %v, %v; want attach.file", cmd, ok)
	}
	_, cmd, ok = r.Step(nil, "?")
	if !ok || cmd == nil || cmd.ID != "help" {
		t.Fatalf("Step(root, ?) = %v, %v; want help", cmd, ok)
	}
	if _, _, ok := r.Step(nil, "j"); ok {
		t.Error("Step(root, j) ok, want unknown")
	}
	if _, _, ok := r.Step([]string{"a"}, "z"); ok {
		t.Error("Step([a], z) ok, want unknown")
	}
}

func TestOptionsAndTitle(t *testing.T) {
	r := testRegistry(t)
	want := []Option{{Key: "?", Label: "Keybindings"}, {Key: "a", Label: "attach", IsGroup: true}}
	if got := r.Options(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("Options(root) = %+v, want %+v", got, want)
	}
	want = []Option{{Key: "a", Label: "Send audio"}, {Key: "f", Label: "Attach file"}}
	if got := r.Options([]string{"a"}); !reflect.DeepEqual(got, want) {
		t.Errorf("Options([a]) = %+v, want %+v", got, want)
	}
	if got := r.Title(nil); got != "␣" {
		t.Errorf("Title(root) = %q", got)
	}
	if got := r.Title([]string{"a"}); got != "␣ a — attach" {
		t.Errorf("Title([a]) = %q", got)
	}
}

func TestNewRegistryRejectsInvalid(t *testing.T) {
	cases := map[string]struct {
		cmds   []Command
		groups []Group
	}{
		"empty keys":      {[]Command{{ID: "x", Keys: " "}}, nil},
		"duplicate":       {[]Command{{ID: "x", Keys: "?"}, {ID: "y", Keys: "?"}}, nil},
		"prefix conflict": {[]Command{{ID: "x", Keys: "a"}, {ID: "y", Keys: "a f"}}, []Group{{Key: "a", Name: "a"}}},
		"missing group":   {[]Command{{ID: "x", Keys: "a f"}}, nil},
		"duplicate id":    {[]Command{{ID: "x", Keys: "?"}, {ID: "x", Keys: "h"}}, nil},
	}
	for name, c := range cases {
		if _, err := NewRegistry(c.cmds, c.groups); err == nil {
			t.Errorf("%s: NewRegistry() = nil error", name)
		}
	}
}

func TestDefaultIsValid(t *testing.T) {
	r := Default()
	for _, id := range []string{"attach.file", "attach.audio", "attach.picker", "help"} {
		found := false
		for _, c := range r.Commands() {
			found = found || c.ID == id
		}
		if !found {
			t.Errorf("default registry lacks %s", id)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/commands/`
Expected: FAIL (build: `undefined: NewRegistry`).

- [ ] **Step 3: Write minimal implementation**

```go
// Package commands holds the leader-key command registry: key sequences
// typed after Space, resolved one key at a time for the which-key popup.
package commands

import (
	"fmt"
	"sort"
	"strings"
)

// Command is an action reachable as Space followed by Keys ("a f").
type Command struct {
	ID   string
	Keys string
	Desc string
}

// Group labels a key prefix shared by several commands ("a" → "attach").
type Group struct {
	Key  string
	Name string
}

// Option is one row of the which-key popup.
type Option struct {
	Key     string
	Label   string
	IsGroup bool
}

// InvokeMsg is emitted when a key sequence resolves to a command.
type InvokeMsg struct{ ID string }

// Registry resolves leader key sequences.
type Registry struct {
	cmds   map[string]Command // by Keys
	groups map[string]string  // prefix path → name
}

// NewRegistry validates and indexes cmds: no empty or duplicate sequences or
// IDs, no command that is a prefix of another, and a group label for every
// prefix.
func NewRegistry(cmds []Command, groups []Group) (*Registry, error) {
	r := &Registry{cmds: map[string]Command{}, groups: map[string]string{}}
	for _, g := range groups {
		r.groups[g.Key] = g.Name
	}
	ids := map[string]bool{}
	for _, c := range cmds {
		keys := strings.Fields(c.Keys)
		if len(keys) == 0 {
			return nil, fmt.Errorf("command %q has no keys", c.ID)
		}
		c.Keys = strings.Join(keys, " ")
		if _, dup := r.cmds[c.Keys]; dup {
			return nil, fmt.Errorf("duplicate key sequence %q", c.Keys)
		}
		if ids[c.ID] {
			return nil, fmt.Errorf("duplicate command id %q", c.ID)
		}
		ids[c.ID] = true
		for i := 1; i < len(keys); i++ {
			prefix := strings.Join(keys[:i], " ")
			if _, ok := r.groups[prefix]; !ok {
				return nil, fmt.Errorf("prefix %q of %q has no group label", prefix, c.Keys)
			}
		}
		r.cmds[c.Keys] = c
	}
	for seq := range r.cmds {
		for other := range r.cmds {
			if other != seq && strings.HasPrefix(other, seq+" ") {
				return nil, fmt.Errorf("command %q is a prefix of %q", seq, other)
			}
		}
	}
	return r, nil
}

// Step advances prefix by key: cmd is set when the sequence resolves, next is
// the longer prefix otherwise; ok is false for a key that leads nowhere.
func (r *Registry) Step(prefix []string, key string) (next []string, cmd *Command, ok bool) {
	seq := append(append([]string(nil), prefix...), key)
	joined := strings.Join(seq, " ")
	if c, found := r.cmds[joined]; found {
		return nil, &c, true
	}
	if _, isGroup := r.groups[joined]; isGroup {
		return seq, nil, true
	}
	return nil, nil, false
}

// Options lists the keys available after prefix, sorted by key.
func (r *Registry) Options(prefix []string) []Option {
	base := strings.Join(prefix, " ")
	seen := map[string]Option{}
	for seq, c := range r.cmds {
		rest := seq
		if base != "" {
			if !strings.HasPrefix(seq, base+" ") {
				continue
			}
			rest = strings.TrimPrefix(seq, base+" ")
		}
		keys := strings.Fields(rest)
		if len(keys) == 1 {
			seen[keys[0]] = Option{Key: keys[0], Label: c.Desc}
			continue
		}
		groupKey := strings.TrimSpace(base + " " + keys[0])
		seen[keys[0]] = Option{Key: keys[0], Label: r.groups[groupKey], IsGroup: true}
	}
	out := make([]Option, 0, len(seen))
	for _, o := range seen {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Title is the popup heading for prefix: "␣" at the root, "␣ a — attach" below.
func (r *Registry) Title(prefix []string) string {
	if len(prefix) == 0 {
		return "␣"
	}
	return "␣ " + strings.Join(prefix, " ") + " — " + r.groups[strings.Join(prefix, " ")]
}

// Commands returns every command sorted by key sequence.
func (r *Registry) Commands() []Command {
	out := make([]Command, 0, len(r.cmds))
	for _, c := range r.cmds {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Keys < out[j].Keys })
	return out
}

// DefaultCommands and DefaultGroups define watui's leader bindings.
var (
	DefaultCommands = []Command{
		{ID: "attach.file", Keys: "a f", Desc: "Attach file (type a path)"},
		{ID: "attach.audio", Keys: "a a", Desc: "Send audio as voice note (type a path)"},
		{ID: "attach.picker", Keys: "a o", Desc: "Pick a file (GUI dialog)"},
		{ID: "help", Keys: "?", Desc: "All keybindings"},
	}
	DefaultGroups = []Group{{Key: "a", Name: "attach"}}
)

// Default returns the registry for DefaultCommands; it panics on an invalid
// definition, which TestDefaultIsValid catches.
func Default() *Registry {
	r, err := NewRegistry(DefaultCommands, DefaultGroups)
	if err != nil {
		panic(err)
	}
	return r
}
```

- [ ] **Step 4: Run tests** — `go test ./internal/ui/commands/` → PASS; `gofmt -l internal/ui/commands` empty; `go vet ./internal/ui/commands`.

- [ ] **Step 5: Commit** — `feat(commands): leader-key command registry`.

---

### Task 2: Which-key popup view (`internal/ui/whichkey`)

**Files:**
- Create: `internal/ui/whichkey/whichkey.go`
- Test: `internal/ui/whichkey/whichkey_test.go`

**Interfaces:**
- Consumes: `commands.Option` from Task 1 (same worktree, done first).
- Produces: `func View(title string, opts []commands.Option, maxW int) string` — bordered box, one row per option: `key  label` (groups as `key  +label`), width ≤ maxW.

- [ ] **Step 1: Write the failing test**

```go
package whichkey

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/ui/commands"
)

func TestViewListsOptions(t *testing.T) {
	out := ansi.Strip(View("␣", []commands.Option{
		{Key: "?", Label: "All keybindings"},
		{Key: "a", Label: "attach", IsGroup: true},
	}, 60))
	for _, want := range []string{"␣", "?  All keybindings", "a  +attach"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q:\n%s", want, out)
		}
	}
}

func TestViewRespectsWidth(t *testing.T) {
	out := View("␣ a — attach", []commands.Option{{Key: "f", Label: strings.Repeat("very long label ", 10)}}, 30)
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 30 {
			t.Errorf("line width %d > 30: %q", w, ansi.Strip(l))
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/ui/whichkey/` → FAIL (`undefined: View`).

- [ ] **Step 3: Implement**

```go
// Package whichkey renders the leader-key popup listing the keys available
// after the current prefix.
package whichkey

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/theme"
	"github.com/watui/watui/internal/ui/commands"
)

var (
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorFocused).
			Background(theme.ColorBgPanel).
			Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary)
	keyStyle   = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText)
	groupStyle = lipgloss.NewStyle().Foreground(theme.ColorPrimary)
	descStyle  = lipgloss.NewStyle().Foreground(theme.ColorTextDim)
)

// View renders title and opts in a box no wider than maxW columns.
func View(title string, opts []commands.Option, maxW int) string {
	inner := max(4, maxW-boxStyle.GetHorizontalFrameSize())
	rows := []string{titleStyle.Render(ansi.Truncate(title, inner, "…"))}
	for _, o := range opts {
		label := o.Label
		style := descStyle
		if o.IsGroup {
			label, style = "+"+label, groupStyle
		}
		row := keyStyle.Render(o.Key) + "  " + style.Render(label)
		rows = append(rows, ansi.Truncate(row, inner, "…"))
	}
	return boxStyle.Render(strings.Join(rows, "\n"))
}
```

- [ ] **Step 4: Run** `go test ./internal/ui/whichkey/` → PASS; gofmt/vet clean.
- [ ] **Step 5: Commit** — `feat(whichkey): leader popup view`.

---

### Task 3: Input prompt entry points (`internal/ui/input`)

**Files:**
- Modify: `internal/ui/input/input.go` (the `modeText` branch of `Update`, lines ~186–203)
- Test: `internal/ui/input/input_test.go`

**Interfaces:**
- Produces on `*input.Model`:
  - `func (m *Model) StartFilePrompt() tea.Cmd` — same effect as `ctrl+f` in text mode
  - `func (m *Model) StartAudioPrompt() tea.Cmd` — same as `ctrl+p`
  - `func (m *Model) PickFile() tea.Cmd` — same as `ctrl+o` (non-audio)
  - `func (m Model) InPathPrompt() bool` — true in file/audio mode
- Callers must focus the input first (`SetFocused(true)`); `SetFocused(false)` resets to text mode.

- [ ] **Step 1: Write the failing test** (append to `input_test.go`; add imports `tea "github.com/charmbracelet/bubbletea"` if missing)

```go
func TestStartPromptsMatchShortcuts(t *testing.T) {
	m := New()
	m.SetSize(80, 4)
	m.SetFocused(true)

	_ = m.StartFilePrompt()
	if !m.InPathPrompt() || m.mode != modeFile {
		t.Fatalf("StartFilePrompt: mode = %v", m.mode)
	}
	// Enter with a path emits SendFileMsg, as with ctrl+f.
	m.pathInput.SetValue("/tmp/a.png")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !containsMsg(cmd, SendFileMsg{Path: "/tmp/a.png"}) {
		t.Errorf("Enter after StartFilePrompt did not emit SendFileMsg")
	}

	_ = m.StartAudioPrompt()
	if m.mode != modeAudio {
		t.Fatalf("StartAudioPrompt: mode = %v", m.mode)
	}
	m.SetFocused(false)
	if m.InPathPrompt() {
		t.Error("blur must reset the prompt")
	}
	if m.PickFile() == nil {
		t.Error("PickFile returned nil cmd")
	}
}

// containsMsg runs cmd (and nested batches) and reports whether want was produced.
func containsMsg(cmd tea.Cmd, want tea.Msg) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if containsMsg(c, want) {
				return true
			}
		}
		return false
	}
	return msg == want
}
```

- [ ] **Step 2: Run** `go test ./internal/ui/input/ -run TestStartPrompts` → FAIL (`undefined: StartFilePrompt`).

- [ ] **Step 3: Implement** — extract the three branches into methods and call them from `Update`:

```go
// StartFilePrompt switches to the file-path prompt (what ctrl+f does). The
// input must be focused.
func (m *Model) StartFilePrompt() tea.Cmd { return m.startPrompt(modeFile, "File path...") }

// StartAudioPrompt switches to the audio-path prompt (what ctrl+p does).
func (m *Model) StartAudioPrompt() tea.Cmd { return m.startPrompt(modeAudio, "Audio file path...") }

// PickFile opens the GUI file picker (what ctrl+o does in text mode).
func (m *Model) PickFile() tea.Cmd { return pickFileCmd(false) }

// InPathPrompt reports whether a file/audio path prompt is active.
func (m Model) InPathPrompt() bool { return m.mode == modeFile || m.mode == modeAudio }

func (m *Model) startPrompt(mode inputMode, placeholder string) tea.Cmd {
	m.mode = mode
	m.pathInput.Placeholder = placeholder
	m.textarea.Blur()
	m.pathInput.Reset()
	return m.pathInput.Focus()
}
```

and in `Update`'s `case modeText:` replace the bodies:

```go
			case "ctrl+f":
				return m, m.StartFilePrompt()

			case "ctrl+p":
				return m, m.StartAudioPrompt()

			case "ctrl+o":
				return m, m.PickFile()
```

- [ ] **Step 4: Run** `go test ./internal/ui/input/` → PASS (existing tests too); gofmt/vet.
- [ ] **Step 5: Commit** — `refactor(input): expose path prompts and picker for leader commands`.

---

### Task 4: Leader state machine, overlays and help in `app` (PR A)

Runs on `feat/leader-commands` after Tasks 1–3 are merged into it.

**Files:**
- Create: `internal/app/overlay.go` (overlay interface, help overlay, placement)
- Create: `internal/app/leader.go` (leader handling, InvokeMsg dispatch)
- Modify: `internal/app/model.go` (fields + NewModel), `internal/app/keys.go` (routing), `internal/app/update.go` (`case commands.InvokeMsg`), `internal/app/view.go` (`renderChat` overlay placement)
- Test: `internal/app/leader_test.go`
- Docs: `README.md` key table, `CONTEXT.md` key-binding table (+ note in Phase list)

**Interfaces:**
- Consumes: `commands.Default()`, `(*Registry).Step/Options/Title/Commands`, `commands.InvokeMsg`; `whichkey.View`; `input.StartFilePrompt/StartAudioPrompt/PickFile`.
- Produces (used by Task 7):
  ```go
  type overlay interface {
      Update(tea.KeyMsg) (overlay, tea.Cmd) // returning nil closes the overlay
      View(maxW, maxH int) string
      Centered() bool                       // false = bottom-right (which-key)
  }
  func (m *Model) openOverlay(o overlay)
  func (m *Model) dispatchCommand(id string) tea.Cmd // switch on command IDs
  ```
  Model fields: `registry *commands.Registry`, `leader []string` (nil = inactive), `overlay overlay`.

- [ ] **Step 1: Write the failing tests** (`internal/app/leader_test.go`)

```go
package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/core"
)

func space() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}} }
func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func chatModel(t *testing.T) Model {
	t.Helper()
	m, _ := newTestModel(t)
	m = send(t, m, core.Connected{})
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	convs := []core.Conversation{
		{JID: "a@s.whatsapp.net", Name: "Ana", LastMsgTime: time.Unix(2, 0)},
		{JID: "b@s.whatsapp.net", Name: "Bia", LastMsgTime: time.Unix(1, 0)},
	}
	return send(t, m, conversationsLoadedMsg{Conversations: convs})
}

func TestSpaceOpensWhichKeyInNormalMode(t *testing.T) {
	m := chatModel(t)
	m = send(t, m, space())
	if m.leader == nil {
		t.Fatal("Space in chat list did not start the leader")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "+attach") || !strings.Contains(v, "All keybindings") {
		t.Errorf("which-key popup not shown:\n%s", v)
	}
	if got := strings.Count(m.View(), "\n") + 1; got != 30 {
		t.Errorf("frame is %d rows with popup, want 30", got)
	}
}

func TestSpaceTypesInInput(t *testing.T) {
	m := chatModel(t)
	m = open(t, m, "a@s.whatsapp.net")
	m = send(t, m, runeKey('i'))
	m = send(t, m, runeKey('o'))
	m = send(t, m, space())
	m = send(t, m, runeKey('i'))
	if m.leader != nil || m.input.Value() != "o i" {
		t.Errorf("input = %q, leader = %v; want Space typed", m.input.Value(), m.leader)
	}
}

func TestUnknownLeaderKeyIsSwallowed(t *testing.T) {
	m := chatModel(t)
	before := m.chatList.SelectedJID()
	m = send(t, m, space())
	m = send(t, m, runeKey('j'))
	if m.leader != nil {
		t.Error("unknown key did not cancel the leader")
	}
	if got := m.chatList.SelectedJID(); got != before {
		t.Errorf("j after Space moved the list (%s → %s)", before, got)
	}
	m = send(t, m, space())
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.leader != nil {
		t.Error("Esc did not cancel the leader")
	}
}

func TestLeaderAttachFileFocusesInputPrompt(t *testing.T) {
	m := chatModel(t)
	m = open(t, m, "a@s.whatsapp.net")
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc}) // back to chat list (normal mode)
	for _, k := range []tea.KeyMsg{space(), runeKey('a'), runeKey('f')} {
		m = send(t, m, k)
	}
	if m.focus != PanelInput || !m.input.InPathPrompt() {
		t.Errorf("focus = %v, prompt = %v; want input in file prompt", m.focus, m.input.InPathPrompt())
	}
}

func TestLeaderAttachWithoutChatReports(t *testing.T) {
	m := chatModel(t)
	m.statusBar.SetWidth(200)
	for _, k := range []tea.KeyMsg{space(), runeKey('a'), runeKey('f')} {
		m = send(t, m, k)
	}
	if !strings.Contains(m.statusBar.View(), "Open a chat first") || m.focus == PanelInput {
		t.Errorf("status = %q focus = %v", m.statusBar.View(), m.focus)
	}
}

func TestLeaderHelpOverlay(t *testing.T) {
	m := chatModel(t)
	m = send(t, m, space())
	m = send(t, m, runeKey('?'))
	v := ansi.Strip(m.View())
	if m.overlay == nil || !strings.Contains(v, "a f") || !strings.Contains(v, "Tab") {
		t.Fatalf("help overlay missing:\n%s", v)
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != nil {
		t.Error("Esc did not close help")
	}
}

func TestCtrlCQuitsWithLeaderOrOverlayOpen(t *testing.T) {
	m := chatModel(t)
	m = send(t, m, space())
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c with leader open did not quit")
	}
	m = send(t, m, runeKey('?'))
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c with help open did not quit")
	}
}

func TestOverlayOnTinyTerminalKeepsFrameHeight(t *testing.T) {
	m := chatModel(t)
	m = send(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})
	m = send(t, m, space())
	m = send(t, m, runeKey('?'))
	if got := strings.Count(m.View(), "\n") + 1; got != 12 {
		t.Errorf("frame %d rows on a 40x12 terminal with help open, want 12", got)
	}
}
```

Check `update(t, m, msg)` in `driver_test.go` returns `(Model, tea.Cmd)` and that `open` exists; adapt the helper names if they differ (they exist today: `update`, `send`, `open`, `run`). If `m.chatList.SelectedJID()` is not exported under that name, use the existing exported accessor (`SelectedJID` exists in `chatlist.go`).

- [ ] **Step 2: Run** `go test ./internal/app/ -run 'Space|Leader|CtrlC|Overlay'` → FAIL (build: `m.leader undefined`).

- [ ] **Step 3: Implement**

`internal/app/overlay.go`:

```go
package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/theme"
	"github.com/watui/watui/internal/ui/commands"
)

// overlay is a modal drawn over the body; while open it receives keys first.
type overlay interface {
	Update(tea.KeyMsg) (overlay, tea.Cmd) // nil closes the overlay
	View(maxW, maxH int) string
	Centered() bool // false: bottom-right corner (which-key)
}

func (m *Model) openOverlay(o overlay) { m.overlay = o; m.leader = nil }

// placeOverlay draws box over body (bodyW×bodyH), bottom-right or centred,
// without changing body's size.
func placeOverlay(body, box string, bodyW, bodyH int, centered bool) string {
	lines := strings.Split(body, "\n")
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	boxLines := strings.Split(box, "\n")
	if len(boxLines) > bodyH {
		boxLines = boxLines[:bodyH]
	}
	boxW := 0
	for _, l := range boxLines {
		boxW = max(boxW, lipgloss.Width(l))
	}
	boxW = min(boxW, bodyW)
	top, left := bodyH-len(boxLines), bodyW-boxW
	if centered {
		top, left = (bodyH-len(boxLines))/2, (bodyW-boxW)/2
	}
	for i, bl := range boxLines {
		row := top + i
		line := lines[row]
		pad := max(0, left-lipgloss.Width(line))
		lines[row] = ansi.Truncate(line, left, "") + strings.Repeat(" ", pad) +
			ansi.Truncate(bl, boxW, "") + ansi.TruncateLeft(line, left+boxW, "")
	}
	return strings.Join(lines[:bodyH], "\n")
}

// helpOverlay lists every leader command and the direct keys.
type helpOverlay struct{ reg *commands.Registry }

var directKeys = [][2]string{
	{"Tab / Shift+Tab", "cycle panels"}, {"j / k", "move"}, {"g / G", "top / bottom"},
	{"Enter", "open chat · send · open media"}, {"i", "focus input"}, {"Esc", "back / cancel"},
	{"ctrl+f / ctrl+p / ctrl+o", "attach · audio · picker (in input)"}, {"ctrl+c", "quit"},
}

func (h helpOverlay) Update(k tea.KeyMsg) (overlay, tea.Cmd) {
	if k.String() == "esc" || k.String() == "q" || k.String() == "?" {
		return nil, nil
	}
	return h, nil
}

func (h helpOverlay) Centered() bool { return true }

func (h helpOverlay) View(maxW, maxH int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary)
	key := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText)
	dim := lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	rows := []string{title.Render("Leader (Space)")}
	for _, c := range h.reg.Commands() {
		rows = append(rows, key.Render("␣ "+c.Keys)+"  "+dim.Render(c.Desc))
	}
	rows = append(rows, "", title.Render("Direct keys"))
	for _, d := range directKeys {
		rows = append(rows, key.Render(d[0])+"  "+dim.Render(d[1]))
	}
	rows = append(rows, "", dim.Render("esc to close"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.ColorFocused).
		Background(theme.ColorBgPanel).Padding(0, 1)
	inner := max(4, maxW-box.GetHorizontalFrameSize())
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, inner, "…")
	}
	if maxH > 2 && len(rows) > maxH-2 {
		rows = rows[:maxH-2]
	}
	return box.Render(strings.Join(rows, "\n"))
}
```

`internal/app/leader.go`:

```go
package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/ui/commands"
)

// handleLeaderKey advances the active leader sequence. Every key is consumed:
// unknown keys and Esc cancel without reaching the panel underneath.
func (m Model) handleLeaderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.leader = nil
		return m, nil
	}
	next, cmd, ok := m.registry.Step(m.leader, msg.String())
	switch {
	case !ok:
		m.leader = nil
		return m, nil
	case cmd != nil:
		m.leader = nil
		id := cmd.ID
		return m, func() tea.Msg { return commands.InvokeMsg{ID: id} }
	default:
		m.leader = next
		return m, nil
	}
}

// dispatchCommand runs the command with id.
func (m *Model) dispatchCommand(id string) tea.Cmd {
	switch id {
	case "attach.file", "attach.audio", "attach.picker":
		if m.chatView.ChatJID() == "" {
			m.statusBar.SetMessage("Open a chat first")
			return m.clearStatusAfter(statusTimeout)
		}
		focus := m.setFocus(PanelInput)
		switch id {
		case "attach.file":
			return tea.Batch(focus, m.input.StartFilePrompt())
		case "attach.audio":
			return tea.Batch(focus, m.input.StartAudioPrompt())
		default:
			return tea.Batch(focus, m.input.PickFile())
		}
	case "help":
		m.openOverlay(helpOverlay{reg: m.registry})
	}
	return nil
}
```

`model.go`: add fields next to `historyAsked`:

```go
	// registry holds the leader-key commands; leader is the prefix typed after
	// Space (nil when inactive); overlay is the open modal, if any.
	registry *commands.Registry
	leader   []string
	overlay  overlay
```

and in `NewModel`: `registry: commands.Default(),` (import `github.com/watui/watui/internal/ui/commands`).

`keys.go` — in `handleKey`, right after the `if m.state != StateChat { ... }` block, insert:

```go
	if m.overlay != nil {
		var cmd tea.Cmd
		m.overlay, cmd = m.overlay.Update(msg)
		return m, cmd
	}
	if m.leader != nil {
		return m.handleLeaderKey(msg)
	}
	if key == " " && m.focus != PanelInput {
		m.leader = []string{}
		return m, nil
	}
```

`update.go` — add a case:

```go
	case commands.InvokeMsg:
		cmds = append(cmds, m.dispatchCommand(msg.ID))
```

`view.go` — `renderChat`: after computing `body`, overlay it:

```go
	bodyW := m.width
	switch {
	case m.overlay != nil:
		box := m.overlay.View(bodyW-2, bodyH)
		body = placeOverlay(body, box, bodyW, bodyH, m.overlay.Centered())
	case m.leader != nil:
		box := whichkey.View(m.registry.Title(m.leader), m.registry.Options(m.leader), min(48, bodyW))
		body = placeOverlay(body, box, bodyW, bodyH, false)
	}
```

(imports `github.com/watui/watui/internal/ui/whichkey`). Keep the existing `MaxHeight(bodyH)` clip before this.

- [ ] **Step 4: Run** `go test -race ./internal/app/` → PASS, then the full suite `go test -race ./...`, gofmt, vet. Existing `TestViewFillsExactlyTerminalHeight` must still pass.

- [ ] **Step 5: Docs** — README "Key Bindings": add rows
  `| Space | Chat list / messages | Leader: opens the command popup |`,
  `| Space a f / a a / a o | … | Attach file / audio / GUI picker |`,
  `| Space ? | … | All keybindings |`. CONTEXT.md key-binding table: same rows; under "Fase 11" note that `[keys]` can later remap the registry.

- [ ] **Step 6: Commit** — `feat(app): Space leader with which-key popup and help overlay`.

---

### Task 5: `whatsapp.Client.CreateGroup`

**Files:**
- Modify: `internal/whatsapp/client.go` (new function near `GetGroupNames`)
- Test: `internal/whatsapp/client_test.go`

**Interfaces:**
- Produces:
  - `func (c *Client) CreateGroup(ctx context.Context, name string, participants []string) (core.Conversation, error)`
  - `func parseParticipants(jids []string) ([]types.JID, error)` (pure)
  - `func groupConversation(info *types.GroupInfo) core.Conversation` (pure)

- [ ] **Step 1: Write the failing test**

```go
func TestParseParticipants(t *testing.T) {
	got, err := parseParticipants([]string{"5511955556666@s.whatsapp.net", "998877@lid"})
	if err != nil || len(got) != 2 || got[0].User != "5511955556666" || got[1].Server != types.HiddenUserServer {
		t.Fatalf("parseParticipants() = %v, %v", got, err)
	}
	for _, bad := range [][]string{nil, {"no-user"}, {"123@g.us"}} {
		if _, err := parseParticipants(bad); err == nil {
			t.Errorf("parseParticipants(%v) = nil error", bad)
		}
	}
}

func TestGroupConversation(t *testing.T) {
	jid := types.NewJID("120363000000000001", types.GroupServer)
	conv := groupConversation(&types.GroupInfo{JID: jid, GroupName: types.GroupName{Name: "Teste"}})
	if conv.JID != jid.String() || conv.Name != "Teste" || !conv.IsGroup {
		t.Errorf("groupConversation() = %+v", conv)
	}
}

func TestCreateGroupValidatesBeforeNetwork(t *testing.T) {
	c, _ := newStoreClient(t)
	if _, err := c.CreateGroup(context.Background(), "  ", []string{testPN.String()}); err == nil {
		t.Error("empty name accepted")
	}
	if _, err := c.CreateGroup(context.Background(), strings.Repeat("é", 26), []string{testPN.String()}); err == nil {
		t.Error("26-rune name accepted")
	}
	if _, err := c.CreateGroup(context.Background(), "ok", nil); err == nil {
		t.Error("no participants accepted")
	}
	if _, err := c.CreateGroup(context.Background(), "ok", []string{testPN.String()}); err == nil {
		t.Error("offline create returned nil error")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/whatsapp/ -run 'Participants|GroupConversation|CreateGroup'` → FAIL.

- [ ] **Step 3: Implement**

```go
// maxGroupName is WhatsApp's group subject limit; longer names get a 406.
const maxGroupName = 25

// CreateGroup creates a group named name with participants (JID strings; the
// user is added implicitly by the server) and returns its conversation.
func (c *Client) CreateGroup(ctx context.Context, name string, participants []string) (core.Conversation, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxGroupName {
		return core.Conversation{}, fmt.Errorf("group name must be 1–%d characters", maxGroupName)
	}
	users, err := parseParticipants(participants)
	if err != nil {
		return core.Conversation{}, err
	}
	info, err := c.wm.CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: name, Participants: users})
	if err != nil {
		return core.Conversation{}, fmt.Errorf("create group: %w", err)
	}
	return groupConversation(info), nil
}

// parseParticipants parses user JIDs for a group, rejecting empty input,
// non-user JIDs and groups.
func parseParticipants(jids []string) ([]types.JID, error) {
	if len(jids) == 0 {
		return nil, errors.New("create group: no participants")
	}
	out := make([]types.JID, 0, len(jids))
	for _, s := range jids {
		jid, err := types.ParseJID(s)
		if err != nil || jid.User == "" || (jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer) {
			return nil, fmt.Errorf("create group: invalid participant %q", s)
		}
		out = append(out, jid.ToNonAD())
	}
	return out, nil
}

// groupConversation is the chat-list entry for a newly created group.
func groupConversation(info *types.GroupInfo) core.Conversation {
	return core.Conversation{JID: info.JID.String(), Name: info.GroupName.Name, IsGroup: true}
}
```

Add `"unicode/utf8"` to the imports. Confirm field names against the pinned whatsmeow (`types.GroupInfo.GroupName.Name`, `whatsmeow.ReqCreateGroup{Name, Participants}`) in `$(go env GOMODCACHE)/go.mau.fi/whatsmeow@*/types/group.go` and `group.go`.

- [ ] **Step 4: Run** `go test ./internal/whatsapp/` → PASS; gofmt/vet.
- [ ] **Step 5: Commit** — `feat(whatsapp): CreateGroup`.

---

### Task 6: Group form overlay model (`internal/ui/groupform`)

**Files:**
- Create: `internal/ui/groupform/groupform.go`, `internal/ui/groupform/match.go`
- Test: `internal/ui/groupform/groupform_test.go`

**Interfaces:**
- Produces:
  - `type Candidate struct { JID, Name string }`
  - `func New(candidates []Candidate) Model`
  - `func (m Model) Update(tea.KeyMsg) (Model, tea.Cmd, bool)` — third result `closed`
  - `func (m Model) View(maxW, maxH int) string`
  - `func (m *Model) SetCandidates([]Candidate)` (merge, dedupe by JID)
  - `func (m *Model) SetError(err error)`
  - `type SubmitMsg struct { Name string; Participants []string }`
  - `const MaxName = 25`
  - `func ParsePhone(s string) (jid string, ok bool)`

- [ ] **Step 1: Write the failing test**

```go
package groupform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}
func press(m Model, t tea.KeyType) (Model, tea.Cmd, bool) { return m.Update(tea.KeyMsg{Type: t}) }
func spaceKey(m Model) Model {
	m, _, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	return m
}

var people = []Candidate{
	{JID: "5511911112222@s.whatsapp.net", Name: "Ana Agenda"},
	{JID: "5511933334444@s.whatsapp.net", Name: "Anabela"},
	{JID: "5581986072503@s.whatsapp.net", Name: "Bruno Perini"},
}

func TestParsePhone(t *testing.T) {
	for in, want := range map[string]string{
		"+55 (11) 95555-6666": "5511955556666@s.whatsapp.net",
		"5511955556666":       "5511955556666@s.whatsapp.net",
	} {
		if got, ok := ParsePhone(in); !ok || got != want {
			t.Errorf("ParsePhone(%q) = %q, %v", in, got, ok)
		}
	}
	for _, bad := range []string{"ana", "1234567", "+55 11 9555 abc", strings.Repeat("9", 16)} {
		if _, ok := ParsePhone(bad); ok {
			t.Errorf("ParsePhone(%q) accepted", bad)
		}
	}
}

func TestFilterIsAccentInsensitiveAndMatchesNumbers(t *testing.T) {
	m := New([]Candidate{{JID: "5511900000000@s.whatsapp.net", Name: "José Ção"}, people[2]})
	if got := m.filtered(); len(got) != 2 {
		t.Fatalf("empty query shows %d, want all", len(got))
	}
	m = typeText(m, "jose cao")
	if got := m.filtered(); len(got) != 1 || got[0].Name != "José Ção" {
		t.Errorf("accent-insensitive filter = %+v", got)
	}
	m = New(people)
	m = typeText(m, "8607")
	if got := m.filtered(); len(got) != 1 || got[0].Name != "Bruno Perini" {
		t.Errorf("number filter = %+v", got)
	}
}

func TestSelectTypedNumberAndSubmit(t *testing.T) {
	m := New(people)
	m = typeText(m, "ana")
	m = spaceKey(m) // toggles the highlighted "Ana Agenda"
	m.query = ""
	m = typeText(m, "+55 11 95555-6666")
	m, _, _ = press(m, tea.KeyEnter) // adds the typed number
	m, _, _ = press(m, tea.KeyEnter) // → name step
	m = typeText(m, "Teste watui")
	m, _, _ = press(m, tea.KeyEnter) // → confirm
	m, cmd, closed := press(m, tea.KeyEnter)
	if closed || cmd == nil {
		t.Fatal("confirm did not emit a command")
	}
	want := SubmitMsg{Name: "Teste watui", Participants: []string{"5511911112222@s.whatsapp.net", "5511955556666@s.whatsapp.net"}}
	if got := cmd(); !reflect.DeepEqual(got, want) {
		t.Errorf("submit = %#v, want %#v", got, want)
	}
}

func TestTypedNumberOfKnownContactIsNotDuplicated(t *testing.T) {
	m := New(people)
	m = typeText(m, "ana")
	m = spaceKey(m) // Ana Agenda selected
	m.query = ""
	m = typeText(m, "+55 11 91111-2222") // same JID as Ana Agenda
	m, _, _ = press(m, tea.KeyEnter)
	if got := m.selectedJIDs(); len(got) != 1 {
		t.Errorf("selected = %v, want the contact once", got)
	}
}

func TestNameRulesCountRunes(t *testing.T) {
	m := New(people)
	m = spaceKey(m)
	m, _, _ = press(m, tea.KeyEnter) // → name
	m, _, _ = press(m, tea.KeyEnter) // empty name: stays
	if m.step != stepName {
		t.Fatal("empty name advanced")
	}
	m = typeText(m, "Família Ção 🎉 com nome grande demais") // > 25 runes: input stops at 25
	if n := len([]rune(m.name)); n != MaxName {
		t.Errorf("name has %d runes, want capped at %d", n, MaxName)
	}
}

func TestEscStepsBackAndCloses(t *testing.T) {
	m := New(people)
	m = spaceKey(m)
	m, _, _ = press(m, tea.KeyEnter)
	m, _, closed := press(m, tea.KeyEsc)
	if closed || m.step != stepParticipants {
		t.Fatal("Esc in name step should go back")
	}
	if _, _, closed = press(m, tea.KeyEsc); !closed {
		t.Error("Esc in participants step should close")
	}
}

func TestErrorShownInConfirm(t *testing.T) {
	m := New(people)
	m = spaceKey(m)
	m, _, _ = press(m, tea.KeyEnter)
	m = typeText(m, "x")
	m, _, _ = press(m, tea.KeyEnter)
	m.SetError(errors.New("participant not on WhatsApp"))
	if v := ansi.Strip(m.View(60, 20)); !strings.Contains(v, "participant not on WhatsApp") {
		t.Errorf("error not shown:\n%s", v)
	}
}

func TestSetCandidatesDedupes(t *testing.T) {
	m := New(people[:1])
	m.SetCandidates([]Candidate{people[0], people[1]})
	if n := len(m.filtered()); n != 2 {
		t.Errorf("candidates = %d, want 2 (deduped by JID)", n)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/ui/groupform/` → FAIL (package missing).

- [ ] **Step 3: Implement**

`match.go`:

```go
package groupform

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// fold lowercases s and strips diacritics ("José Ção" → "jose cao").
func fold(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		out = s
	}
	return strings.ToLower(out)
}

// matches reports whether every rune of query appears in order in text.
func matches(text, query string) bool {
	q := []rune(fold(query))
	i := 0
	for _, r := range fold(text) {
		if i < len(q) && r == q[i] {
			i++
		}
	}
	return i == len(q)
}

// ParsePhone turns a typed phone number into a user JID: "+", digits, spaces,
// dashes and parentheses allowed, 8–15 digits.
func ParsePhone(s string) (string, bool) {
	var digits strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' || r == ' ' || r == '-' || r == '(' || r == ')':
		default:
			return "", false
		}
	}
	d := digits.String()
	if len(d) < 8 || len(d) > 15 {
		return "", false
	}
	return d + "@s.whatsapp.net", true
}
```

If `golang.org/x/text` is not yet a direct dependency, run `go get golang.org/x/text` (it is already in the module graph via whatsmeow) and `go mod tidy`.

`groupform.go`:

```go
// Package groupform is the "new group" overlay: pick participants, name the
// group, confirm.
package groupform

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/theme"
)

// MaxName is WhatsApp's group subject limit in characters.
const MaxName = 25

// Candidate is a person that can be added.
type Candidate struct{ JID, Name string }

// SubmitMsg asks the app to create the group.
type SubmitMsg struct {
	Name         string
	Participants []string
}

type step int

const (
	stepParticipants step = iota
	stepName
	stepConfirm
)

// Model is the form state.
type Model struct {
	candidates []Candidate
	selected   map[string]bool
	order      []string // selection order of JIDs
	typed      map[string]bool
	query      string
	cursor     int
	step       step
	name       string
	hint       string
	err        error
	creating   bool
}

// New starts the form on the participants step.
func New(candidates []Candidate) Model {
	m := Model{selected: map[string]bool{}, typed: map[string]bool{}}
	m.SetCandidates(candidates)
	return m
}

// SetCandidates merges more candidates (e.g. address-book contacts that
// arrived later), deduplicated by JID and sorted by name.
func (m *Model) SetCandidates(cs []Candidate) {
	seen := map[string]bool{}
	var out []Candidate
	for _, c := range append(append([]Candidate(nil), m.candidates...), cs...) {
		if c.JID == "" || seen[c.JID] {
			continue
		}
		seen[c.JID] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return fold(out[i].Name) < fold(out[j].Name) })
	m.candidates = out
}

// SetError shows a create failure on the confirm step and allows retry.
func (m *Model) SetError(err error) { m.err, m.creating = err, false }

func (m Model) filtered() []Candidate {
	var out []Candidate
	for _, c := range m.candidates {
		if m.query == "" || matches(c.Name, m.query) || strings.Contains(c.JID, onlyDigits(m.query)) && onlyDigits(m.query) != "" {
			out = append(out, c)
		}
	}
	return out
}

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (m Model) selectedJIDs() []string { return append([]string(nil), m.order...) }

func (m *Model) toggle(jid string) {
	if m.selected[jid] {
		delete(m.selected, jid)
		for i, j := range m.order {
			if j == jid {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
		return
	}
	m.selected[jid] = true
	m.order = append(m.order, jid)
}

// Update handles a key; closed reports that the form was dismissed.
func (m Model) Update(k tea.KeyMsg) (Model, tea.Cmd, bool) {
	m.hint = ""
	switch m.step {
	case stepParticipants:
		switch k.Type {
		case tea.KeyEsc:
			return m, nil, true
		case tea.KeyUp, tea.KeyCtrlP:
			m.cursor = max(0, m.cursor-1)
		case tea.KeyDown, tea.KeyCtrlN:
			m.cursor = min(max(0, len(m.filtered())-1), m.cursor+1)
		case tea.KeySpace:
			if f := m.filtered(); m.cursor < len(f) {
				m.toggle(f[m.cursor].JID)
			}
		case tea.KeyBackspace:
			if r := []rune(m.query); len(r) > 0 {
				m.query, m.cursor = string(r[:len(r)-1]), 0
			}
		case tea.KeyEnter:
			if jid, ok := ParsePhone(m.query); ok {
				if !m.selected[jid] {
					m.toggle(jid)
					m.typed[jid] = true
				}
				m.query, m.cursor = "", 0
			} else if onlyDigits(m.query) != "" && len(m.filtered()) == 0 {
				m.hint = "not a phone number"
			} else if len(m.order) > 0 {
				m.step = stepName
			}
		case tea.KeyRunes:
			m.query, m.cursor = m.query+string(k.Runes), 0
		}
	case stepName:
		switch k.Type {
		case tea.KeyEsc:
			m.step = stepParticipants
		case tea.KeyBackspace:
			if r := []rune(m.name); len(r) > 0 {
				m.name = string(r[:len(r)-1])
			}
		case tea.KeyEnter:
			if strings.TrimSpace(m.name) != "" {
				m.step = stepConfirm
			}
		case tea.KeyRunes, tea.KeySpace:
			add := string(k.Runes)
			if k.Type == tea.KeySpace {
				add = " "
			}
			if r := []rune(m.name + add); len(r) <= MaxName {
				m.name = string(r)
			} else {
				m.name = string(r[:MaxName])
			}
		}
	case stepConfirm:
		switch k.Type {
		case tea.KeyEsc:
			m.step, m.err = stepName, nil
		case tea.KeyEnter:
			if m.creating {
				return m, nil, false
			}
			m.creating, m.err = true, nil
			msg := SubmitMsg{Name: strings.TrimSpace(m.name), Participants: m.selectedJIDs()}
			return m, func() tea.Msg { return msg }, false
		}
	}
	return m, nil, false
}

// View renders the form in a box of at most maxW×maxH.
func (m Model) View(maxW, maxH int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary)
	dim := lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	errStyle := lipgloss.NewStyle().Foreground(theme.ColorError)
	var rows []string
	switch m.step {
	case stepParticipants:
		rows = append(rows, title.Render(fmt.Sprintf("New group — participants (%d)", len(m.order))), "> "+m.query+"█")
		for i, c := range m.filtered() {
			mark := "[ ]"
			if m.selected[c.JID] {
				mark = "[x]"
			}
			cur := "  "
			if i == m.cursor {
				cur = "› "
			}
			rows = append(rows, cur+mark+" "+c.Name+"  "+dim.Render(core.DisplayName(core.Conversation{JID: c.JID})))
		}
		for _, jid := range m.order {
			if m.typed[jid] {
				rows = append(rows, "  [x] "+core.DisplayName(core.Conversation{JID: jid})+dim.Render(" (typed)"))
			}
		}
		if m.hint != "" {
			rows = append(rows, errStyle.Render(m.hint))
		}
		rows = append(rows, dim.Render("type to search · space select · enter add number / next · esc close"))
	case stepName:
		rows = append(rows, title.Render("New group — name"),
			"> "+m.name+"█", dim.Render(fmt.Sprintf("%d/%d · enter next · esc back", utf8.RuneCountInString(m.name), MaxName)))
	case stepConfirm:
		rows = append(rows, title.Render("New group — confirm"), "Name: "+m.name,
			fmt.Sprintf("Participants: %d", len(m.order)))
		if m.creating {
			rows = append(rows, dim.Render("Creating…"))
		}
		if m.err != nil {
			rows = append(rows, errStyle.Render("Failed: "+m.err.Error()))
		}
		rows = append(rows, dim.Render("enter create · esc back"))
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.ColorFocused).
		Background(theme.ColorBgPanel).Padding(0, 1)
	inner := max(4, min(60, maxW)-box.GetHorizontalFrameSize())
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, inner, "…")
	}
	if maxH > 2 && len(rows) > maxH-2 {
		rows = append(rows[:maxH-3], rows[len(rows)-1])
	}
	return box.Render(strings.Join(rows, "\n"))
}
```

- [ ] **Step 4: Run** `go test ./internal/ui/groupform/` → PASS; gofmt/vet. Fix any operator-precedence lint in `filtered` by parenthesising: `(onlyDigits(m.query) != "" && strings.Contains(c.JID, onlyDigits(m.query)))`.
- [ ] **Step 5: Commit** — `feat(groupform): new-group overlay`.

---

### Task 7: Wire group creation into the app (PR B)

Runs on `feat/group-create` (based on `feat/leader-commands`) after Tasks 5 and 6 are merged into it.

**Files:**
- Modify: `internal/ui/commands/commands.go` (add command + group), `internal/app/model.go` (WAClient + msgs), `internal/app/waadapter.go`, `internal/app/leader.go` (dispatch), `internal/app/update.go`
- Create: `internal/app/groupcreate.go` (overlay adapter + handlers)
- Test: `internal/app/groupcreate_test.go`, `internal/app/waadapter_test.go`, `internal/app/helpers_test.go` (fakes)
- Docs: README/CONTEXT key tables (`Space g c`)

**Interfaces:**
- Consumes: `whatsapp.Client.CreateGroup` (Task 5); `groupform.*` (Task 6); `overlay`, `openOverlay`, `dispatchCommand` (Task 4).
- Produces:
  - `WAClient.CreateGroup(name string, participants []string) tea.Cmd`
  - `syncWAClient.CreateGroup(ctx context.Context, name string, participants []string) (core.Conversation, error)`
  - msgs `groupCreatedMsg{Conv core.Conversation}`, `groupCreateFailedMsg{Err error}`, `groupCandidatesMsg{Candidates []groupform.Candidate}`

- [ ] **Step 1: Write the failing tests**

`internal/app/groupcreate_test.go`:

```go
package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/core"
)

func openGroupForm(t *testing.T, m Model) Model {
	t.Helper()
	for _, k := range []tea.KeyMsg{space(), runeKey('g'), runeKey('c')} {
		m = send(t, m, k)
	}
	if _, ok := m.overlay.(*groupOverlay); !ok {
		t.Fatalf("overlay = %T, want the group form", m.overlay)
	}
	return m
}

func TestGroupCreateFlow(t *testing.T) {
	wa := &recordingWA{contactNames: map[string]string{"c@s.whatsapp.net": "Carla"}}
	m := testModel(wa, newTestStore(t))
	m = send(t, m, core.Connected{})
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = send(t, m, conversationsLoadedMsg{Conversations: []core.Conversation{{JID: "a@s.whatsapp.net", Name: "Ana"}}})

	m = openGroupForm(t, m)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Ana") || !strings.Contains(v, "Carla") {
		t.Fatalf("candidates from chats and contacts missing:\n%s", v)
	}
	if got := strings.Count(m.View(), "\n") + 1; got != 30 {
		t.Errorf("frame %d rows with the form open, want 30", got)
	}

	for _, r := range "ana" {
		m = send(t, m, runeKey(r))
	}
	m = send(t, m, space())
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "Teste" {
		m = send(t, m, runeKey(r))
	}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	wa.groupConv = core.Conversation{JID: "120363@g.us", Name: "Teste", IsGroup: true}
	m = send(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if want := []groupCall{{Name: "Teste", Participants: []string{"a@s.whatsapp.net"}}}; !reflect.DeepEqual(wa.groupCalls, want) {
		t.Fatalf("CreateGroup calls = %+v, want %+v", wa.groupCalls, want)
	}
	if m.overlay != nil {
		t.Error("form still open after success")
	}
	if conv, ok := m.chats.Conversation("120363@g.us"); !ok || !conv.IsGroup {
		t.Errorf("new group not in chats: %+v", conv)
	}
	if m.chatView.ChatJID() != "120363@g.us" {
		t.Errorf("open chat = %q, want the new group", m.chatView.ChatJID())
	}
}

func TestGroupCreateFailureKeepsForm(t *testing.T) {
	m := chatModel(t)
	m = openGroupForm(t, m)
	m = send(t, m, groupCreateFailedMsg{Err: errors.New("406 not acceptable")})
	if _, ok := m.overlay.(*groupOverlay); !ok {
		t.Fatal("form closed on failure")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "406 not acceptable") {
		t.Errorf("error not shown:\n%s", v)
	}
}

func TestCtrlCQuitsWithGroupFormOpen(t *testing.T) {
	m := openGroupForm(t, chatModel(t))
	if _, cmd := update(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c with the group form open did not quit")
	}
}
```

In `helpers_test.go` add to `recordingWA`:

```go
	groupCalls []groupCall
	groupConv  core.Conversation
	groupErr   error
```

```go
type groupCall struct {
	Name         string
	Participants []string
}

func (r *recordingWA) CreateGroup(name string, participants []string) tea.Cmd {
	r.mu.Lock()
	r.groupCalls = append(r.groupCalls, groupCall{name, append([]string(nil), participants...)})
	conv, err := r.groupConv, r.groupErr
	r.mu.Unlock()
	return func() tea.Msg {
		if err != nil {
			return groupCreateFailedMsg{Err: err}
		}
		return groupCreatedMsg{Conv: conv}
	}
}
```

and `func (fakeWA) CreateGroup(string, []string) tea.Cmd { return nil }`.

In `waadapter_test.go`: add `groupConv core.Conversation; groupErr error; groupName string; groupParts []string` to `fakeSyncWA`, its method, and:

```go
func TestAdapterCreateGroup(t *testing.T) {
	f := &fakeSyncWA{groupConv: core.Conversation{JID: "g@g.us", Name: "T", IsGroup: true}}
	if got := newWAAdapter(f).CreateGroup("T", []string{"a@s.whatsapp.net"})(); got != (groupCreatedMsg{Conv: f.groupConv}) {
		t.Errorf("success = %#v", got)
	}
	fail := errors.New("offline")
	got := newWAAdapter(&fakeSyncWA{groupErr: fail}).CreateGroup("T", nil)()
	if msg, ok := got.(groupCreateFailedMsg); !ok || !errors.Is(msg.Err, fail) {
		t.Errorf("failure = %#v", got)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/app/ -run 'Group'` → FAIL (build).

- [ ] **Step 3: Implement**

`commands.go` — append to `DefaultCommands` and `DefaultGroups`:

```go
		{ID: "group.create", Keys: "g c", Desc: "New group"},
```
```go
	DefaultGroups = []Group{{Key: "a", Name: "attach"}, {Key: "g", Name: "group"}}
```

`model.go` — `WAClient` gains:

```go
	// CreateGroup creates a group; the result arrives as groupCreatedMsg or
	// groupCreateFailedMsg.
	CreateGroup(name string, participants []string) tea.Cmd
```

and messages:

```go
// groupCreatedMsg / groupCreateFailedMsg report a CreateGroup result;
// groupCandidatesMsg brings address-book people to an open group form.
type groupCreatedMsg struct{ Conv core.Conversation }
type groupCreateFailedMsg struct{ Err error }
type groupCandidatesMsg struct{ Candidates []groupform.Candidate }
```

`waadapter.go` — `syncWAClient` gains `CreateGroup(ctx context.Context, name string, participants []string) (core.Conversation, error)` and:

```go
// CreateGroup returns a command that creates the group.
func (a *waAdapter) CreateGroup(name string, participants []string) tea.Cmd {
	return func() tea.Msg {
		conv, err := a.c.CreateGroup(context.Background(), name, participants)
		if err != nil {
			return groupCreateFailedMsg{Err: err}
		}
		return groupCreatedMsg{Conv: conv}
	}
}
```

`groupcreate.go`:

```go
package app

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/ui/groupform"
)

// groupOverlay adapts groupform.Model to the overlay interface. It is a
// pointer so the app can push candidates and errors into the open form.
type groupOverlay struct{ form groupform.Model }

func (g *groupOverlay) Update(k tea.KeyMsg) (overlay, tea.Cmd) {
	form, cmd, closed := g.form.Update(k)
	if closed {
		return nil, nil
	}
	g.form = form
	return g, cmd
}

func (g *groupOverlay) View(maxW, maxH int) string { return g.form.View(maxW, maxH) }
func (g *groupOverlay) Centered() bool             { return true }

// openGroupForm opens the form with the known 1:1 chats and fetches
// address-book contacts in the background.
func (m *Model) openGroupForm() tea.Cmd {
	var cands []groupform.Candidate
	for _, conv := range m.chats.Conversations() {
		if !conv.IsGroup && !strings.HasSuffix(conv.JID, "@g.us") {
			cands = append(cands, groupform.Candidate{JID: conv.JID, Name: core.DisplayName(conv)})
		}
	}
	m.openOverlay(&groupOverlay{form: groupform.New(cands)})
	wa := m.wa
	return func() tea.Msg {
		var out []groupform.Candidate
		for jid, name := range wa.GetAllContactNames() {
			if strings.HasSuffix(jid, "@s.whatsapp.net") {
				out = append(out, groupform.Candidate{JID: jid, Name: name})
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].JID < out[j].JID })
		return groupCandidatesMsg{Candidates: out}
	}
}

// handleGroupCreated adds the new group to the list, persists it and opens it.
func (m *Model) handleGroupCreated(conv core.Conversation) (Model, tea.Cmd) {
	m.overlay = nil
	persist := m.applyEffects(m.chats.UpdateConversation(conv))
	m.statusBar.SetMessage("Group created")
	next, openCmd := m.selectChat(conv.JID)
	return next, tea.Batch(persist, openCmd, next.clearStatusAfter(statusTimeout))
}
```

`core.Chats` needs `func (c *Chats) Conversations() []Conversation` (sorted by JID) — add it in `internal/core/chats.go` with a test in `chats_test.go`:

```go
// Conversations returns every known conversation, sorted by JID.
func (c *Chats) Conversations() []Conversation {
	out := make([]Conversation, 0, len(c.convs))
	for _, conv := range c.convs {
		out = append(out, conv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JID < out[j].JID })
	return out
}
```

```go
func TestConversationsSorted(t *testing.T) {
	c := NewChats(nil)
	c.Load([]Conversation{{JID: "b@s.whatsapp.net"}, {JID: "a@s.whatsapp.net"}})
	if got := c.Conversations(); len(got) != 2 || got[0].JID != "a@s.whatsapp.net" {
		t.Errorf("Conversations() = %+v", got)
	}
}
```

`leader.go` `dispatchCommand` — add:

```go
	case "group.create":
		return m.openGroupForm()
```

`update.go` — add cases:

```go
	case groupform.SubmitMsg:
		cmds = append(cmds, m.wa.CreateGroup(msg.Name, msg.Participants))

	case groupCreatedMsg:
		return m.handleGroupCreated(msg.Conv)

	case groupCreateFailedMsg:
		m.log.Error(msg.Err, "create group")
		if g, ok := m.overlay.(*groupOverlay); ok {
			g.form.SetError(msg.Err)
		} else {
			m.statusBar.SetMessage("Could not create group: " + msg.Err.Error())
			cmds = append(cmds, m.clearStatusAfter(statusTimeout))
		}

	case groupCandidatesMsg:
		if g, ok := m.overlay.(*groupOverlay); ok {
			g.form.SetCandidates(msg.Candidates)
		}
```

Check `selectChat` signature (`func (m *Model) selectChat(jid string) (Model, tea.Cmd)`) and `applyEffects` returning `tea.Cmd`; they exist in `internal/app/chats.go`.

- [ ] **Step 4: Run** `go test -race ./...`, gofmt, vet → all green.
- [ ] **Step 5: Docs** — README/CONTEXT: `| Space g c | Chat list / messages | New group |`; CONTEXT "Estrutura" tree gains `ui/commands`, `ui/whichkey`, `ui/groupform`.
- [ ] **Step 6: Commit** — `feat(app): create groups from Space g c`.
- [ ] **Step 7: Manual check (user)** — `Space g c` → pick a contact and a typed number → name → Enter → group appears on the phone and opens in watui.
