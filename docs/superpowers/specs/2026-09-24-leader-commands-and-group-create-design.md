# Leader-key commands and group creation — design

Date: 2026-09-24 · Status: approved in conversation, pending spec review

## Intent

**What the user asked for:** a way to create a new WhatsApp group from watui (so group
features such as sender names can be tested without the phone), and a keybinding model
like nvim/LazyVim — a trigger key, then a command, then the action — instead of more
scattered `ctrl+<letter>` shortcuts.

**Decisions made in conversation:**
- Leader key is **Space**, active in "normal mode" (chat list and message view). The input
  panel is "insert mode": Space types a space; `Esc` returns to normal mode (existing
  behaviour).
- Existing shortcuts stay: direct navigation (`j k g G Tab Shift+Tab i Esc Enter`) and the
  input's `ctrl+f` / `ctrl+p` / `ctrl+o`. Actions additionally get leader entries.
- Group participants are picked with a fuzzy search over known contacts/chats, toggled
  with `Space`, and a phone number not in the address book can be typed.

**Assumptions (correct me):**
- No timeout on the leader popup; it stays open until a key resolves or cancels it.
- After a group is created, its chat opens.
- Configurable bindings (`[keys]` in config.toml) are out of scope; the registry is designed
  so Phase 11 can add them.

**Success criteria:**
1. `Space` in normal mode shows a which-key popup listing available keys; every existing
   action is reachable through it; nothing typed in the input is affected.
2. `Space g c` opens a form; picking participants (search, toggle, typed number) and a name
   creates the group on WhatsApp; the new chat appears and opens.
3. The frame stays exactly the terminal height with any overlay open.
4. All new logic is unit-tested without a terminal or network.

## Scope

Two stacked PRs:
1. **Command layer** — registry, leader state machine, which-key overlay, help, migration of
   existing actions.
2. **Group creation** — `Space g c` form and the WhatsApp call.

Out of scope: configurable keymaps, a `:` command palette, chat search (`/` is in the
keymap but unimplemented), group admin actions (add/remove members, rename).

## Part 1 — Command layer

### Units

**`internal/ui/commands` (new, pure)**
- `type Command struct { ID string; Keys string /* "a f" */; Desc string }`
- `type Group struct { Key string; Name string }` — labels for prefixes (`a` → "attach",
  `g` → "group").
- `type Registry` built from `[]Command` + `[]Group`; construction validates: no empty
  keys, no duplicate sequence, no command whose keys are a prefix of another command
  (e.g. `a` and `a f`), every prefix has a group label. Construction errors are
  programming errors → returned as `error`, checked by a test over the default registry.
- `func (r *Registry) Step(prefix []string, key string) (next []string, cmd *Command, ok bool)`:
  `ok=false` → unknown key (cancel); `cmd!=nil` → resolved; otherwise `next` is the longer
  prefix.
- `func (r *Registry) Options(prefix []string) []Option` — sorted entries for the popup:
  `{Key, Label, IsGroup}`.
- `DefaultRegistry()` holds the watui commands:

| Keys | ID | Description |
|---|---|---|
| `a f` | `attach.file` | Attach file (path prompt) |
| `a a` | `attach.audio` | Send audio as voice note (path prompt) |
| `a o` | `attach.picker` | Pick a file (GUI dialog) |
| `g c` | `group.create` | New group |
| `?` | `help` | All keybindings |

- `commands.InvokeMsg{ID string}` — emitted when a sequence resolves.

**`internal/ui/whichkey` (new)** — view-only model: `SetOptions(title string, opts []Option)`,
`View(width int) string` renders a bordered box (title = prefix path, e.g. `␣ a — attach`),
one row per option (`f  Attach file`, groups shown as `+attach`). No key handling.

**`internal/app` changes**
- Model gains `leader []string` (nil = inactive), `registry *commands.Registry`, and
  `overlay` (see below).
- Key routing order in `handleKey`, after `ctrl+c` and the non-chat states:
  1. overlay active → overlay gets the key (`Esc` closes it);
  2. leader active → `Step`; unknown key or `Esc` cancels (key is swallowed, not passed to
     the panel); a resolved command closes the popup and returns
     `commands.InvokeMsg{ID}` as a cmd;
  3. focus is input → existing insert-mode handling (unchanged, Space types);
  4. `" "` (Space) in normal mode → leader starts, popup shows the root options;
  5. existing normal-mode keys.
- `Update` handles `commands.InvokeMsg` by ID:
  - `attach.file` / `attach.audio` → focus input and start its path prompt;
  - `attach.picker` → open the GUI picker;
  - `group.create` → open the group form overlay (Part 2; until then hidden from the
    registry);
  - `help` → open a help overlay listing every registry command and the direct keys.
  The attach commands require an open chat; without one, the status bar says
  "Open a chat first".
- `internal/ui/input` gains exported entry points used above (`StartFilePrompt`,
  `StartAudioPrompt`, `PickFile`) reusing the code paths behind `ctrl+f`/`ctrl+p`/`ctrl+o`.

**Overlay rendering.** `renderChat` composes the body, then places the overlay box over its
bottom-right corner (which-key) or centre (forms, help) with lipgloss placement, clipped to
the body's row budget, so frame height never changes. The overlay is a small interface
inside app:

```go
type overlay interface {
	Update(tea.KeyMsg) (overlay, tea.Cmd) // nil overlay = close
	View(maxW, maxH int) string
}
```

### Error handling

Registry validation runs in a test; at runtime an unknown key simply cancels the leader.
Commands that need context (an open chat) report through the status bar and leave state
unchanged.

### Tests (Part 1)

- `commands`: `Step` over prefixes, resolution, unknown key; `Options` ordering; validation
  rejects duplicates / prefix conflicts / missing group labels; the default registry is valid.
- `whichkey`: rendering lists options and groups; width is respected.
- `app`: Space in chat list/messages opens the leader; Space in input types a space; `Esc`
  and unknown keys cancel without reaching the panel (e.g. `j` after Space does not move the
  list); `␣ a f` focuses input in file-prompt mode; attach without a chat reports; `␣ ?`
  opens help; frame height exact with the popup open.

## Part 2 — Group creation

### Units

**`internal/ui/groupform` (new)** — overlay model with three steps:
1. **Participants.** Text query + list. Candidates = known people: 1:1 conversations and
   address-book contacts (name + JID), deduplicated by JID, sorted by name. The query
   filters with a case/diacritic-insensitive subsequence match over name and number.
   Keys: typing edits the query; `↑/↓` (and `ctrl+p/ctrl+n`) move; `Space` toggles;
   `Enter` on a query that parses as a phone number (`+`, digits, spaces, dashes;
   8–15 digits) adds it as a typed participant; otherwise `Enter` goes to step 2 when ≥1
   is selected. The header shows the selected count.
2. **Name.** Single-line input, max 25 characters (server limit; longer → 406), counter
   shown; `Enter` requires a non-empty trimmed name.
3. **Confirm.** Summary (name + participants); `Enter` emits
   `groupform.SubmitMsg{Name string, Participants []string /* JIDs */}` and shows
   "Creating…"; `Esc` goes back a step (from step 1, closes).
- `SetError(err)` shows a failure in the confirm step and allows retry/back.
- Typed numbers become `<digits>@s.whatsapp.net`.

**Candidate source.** app builds the candidate list when opening the form, on the Update
goroutine: `core.Chats` 1:1 conversations with `core.DisplayName`, plus contacts from
`WAClient.GetAllContactNames()` fetched by a cmd (list refreshes when it arrives).

**`internal/whatsapp`** — `CreateGroup(ctx, name string, participants []string) (core.Conversation, error)`:
parses JIDs (invalid → error before any network call), calls `wm.CreateGroup` with
`ReqCreateGroup{Name, Participants}`, and returns the new conversation
(`JID`, `Name`, `IsGroup: true`).

**Adapter / app**
- `WAClient.CreateGroup(name string, participants []string) tea.Cmd` → `groupCreatedMsg{Conv}`
  or `groupCreateFailedMsg{Err}`.
- On `groupform.SubmitMsg` → call `CreateGroup`.
- On `groupCreatedMsg` → close the form, `chats` upsert + persist (`UpdateConversation`
  effects), select the new chat, status "Group created".
- On `groupCreateFailedMsg` → `form.SetError(err)`; the form stays open.
- Registry gains `g c` → `group.create`.

### Error handling

- Invalid typed number: not added; inline hint "not a phone number".
- Name empty / too long: inline, `Enter` disabled.
- Network/server errors (e.g. participant not on WhatsApp, 406, offline): shown in the
  confirm step; user can go back and edit or retry.

### Tests (Part 2)

- `groupform`: filtering (diacritics, number match), toggle, typed number parsing and
  rejection, step transitions and back, name limits, `SubmitMsg` contents, error display.
- `whatsapp`: participant JID parsing/validation; offline `CreateGroup` returns an error;
  result mapping from `types.GroupInfo` (pure helper).
- adapter: success/failure mapping.
- `app`: `␣ g c` opens the form with candidates from chats (+ contacts when they arrive);
  submit calls `CreateGroup` with the selected JIDs; success opens the new chat and persists
  it; failure keeps the form open with the error; frame height exact with the form open.

## Docs

README key bindings and CONTEXT.md key-binding table + Phase list gain the leader section.
