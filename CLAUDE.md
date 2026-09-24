# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build
go build ./cmd/watui/

# Run
go run ./cmd/watui/ --data-dir ./data

# Run with custom data directory
go run ./cmd/watui/ --data-dir /path/to/data

# Test
go test ./...

# Single package test
go test ./internal/store/...

# Lint
go vet ./...
```

The binary uses `--data-dir` (default: `./data`) to store two SQLite databases: `whatsmeow.db` (whatsmeow session/keys) and `watui.db` (conversations and messages). These are gitignored.

## Architecture

This is a WhatsApp TUI client built with Go + Bubble Tea + whatsmeow. It follows the Model-View-Update pattern from Bubble Tea throughout.

### Event bridge: whatsmeow → Bubble Tea

The central design challenge is connecting whatsmeow's event-driven WebSocket model to Bubble Tea's MVU loop. `internal/whatsapp` knows nothing about Bubble Tea; the bridge has two halves:

```
events:   whatsmeow WebSocket → internal/whatsapp/events.go → client.send(core.Event) → event handler → program.Send() → app.Update()
commands: app.Update() → WAClient (internal/app/waadapter.go, tea.Cmd) → whatsapp.Client sync call (ctx, returns result/error) → core event as tea.Msg
```

- **Events**: `whatsapp.Client` holds an `onEvent func(core.Event)` that is nil until `SetEventHandler` is called in `main.go` (after `tea.NewProgram`, before `program.Run()`) with `func(e core.Event) { program.Send(e) }`. Every `core.Event` is also a valid `tea.Msg`.
- **Commands**: the client's operations are synchronous and take a `context.Context` (`Connect(ctx) error`, `SendText/SendFile/SendAudio(ctx, jid, id, …) (core.MessageSent, error)`, `DownloadMedia(ctx, msg) (path, error)`, `OpenMedia(path, type) error`, `MarkRead`, `SendChatPresence`, `GetAllContactNames`, `GetGroupNames`, `AltChatJID`). `app.NewWAClient` wraps them in `tea.Cmd`s implementing the app's `WAClient` interface and maps results to events: send error → `core.MessageSendFailed`, download → `core.MediaDownloaded`/`core.MediaDownloadFailed`, connect → `nil`, the private `connectFailedMsg` for `*whatsapp.ConnectError` (socket never came up before QR; the only error that enters `StateError`), or `core.LoginFailed`. The adapter depends on a small unexported interface, so it is tested with a fake.
- During the QR flow `Connect` blocks while emitting `core.QRCode`/`core.QRTimeout` through the event handler.

### Package layout

- **`internal/core/`** — domain models (`Conversation`, `Message`), domain events (`core.Event`: `NewMessage`, `Connected`, `MessageSent`, …) and `core.Chats` (`chats.go`), the pure conversation/message state engine: LID↔PN alias folding, dedupe and ordering, unread counts, previews, read-receipt selection. Its operations (`AddMessage`, `Open`, `AddHistory`, `PrependOlder`, …) mutate in-memory state and return `core.Effects` (conversations/messages to persist, unread to clear, receipts to send, downloads, view changes); `Chats` never does I/O. No UI dependency; imports nothing from the rest of the codebase, which avoids import cycles. Events are plain structs, so the app receives them directly as `tea.Msg`.
- **`internal/whatsapp/`** — wraps `go.mau.fi/whatsmeow` behind a synchronous, context-aware `Client` (no Bubble Tea import). `events.go` translates whatsmeow events to `core` events and calls `client.send()`.
- **`internal/store/`** — app-level SQLite (not whatsmeow's own store). Stores `conversations` and `messages` tables (`messages.chat_jid` is a foreign key). Migrations are in `migrations.go`.
- **`internal/app/`** — root Bubble Tea model (`app.Model`), split by domain: `model.go` (types, `WAClient` and `Store` interfaces, private messages, `NewModel`), `update.go` (the `Update` switch), `keys.go` (keys, focus), `chats.go` (selection, effects, receipts), `media.go`, `send.go`, `commands.go` (loaders, timers), `persist.go` (write queue), `view.go`, `waadapter.go` (`WAClient` over `whatsapp.Client`). Conversation state lives in a `*core.Chats`; the model applies its `Effects`. `Store` is the subset of `*store.Store` the app uses, so tests pass a fake.
  - **No I/O in `Update`**: in-memory state, chat list and view update synchronously; SQLite reads/writes and `MarkRead` run in `tea.Cmd`s. Writes go through `writeQueue`: `Update` enqueues them in program order and returns a flush cmd, and flushes run one at a time, so writes never overtake each other; one `Effects` persists conversations, then messages, then unread resets. Opening a chat switches title/view/focus at once and loads stored history in a cmd (`chatLoadedMsg`); a load for a selection the user already left is dropped. Store errors come back as `persistErrMsg` (logged, shown in the status bar, never `StateError`).
- **`internal/ui/`** — sub-models for each UI panel: `auth/qr.go`, `chatlist/`, `chatview/`, `input/`, `statusbar/`, `titlebar/`.
- **`internal/theme/`** — lipgloss styles (`styles.go`) and keymap (`keymap.go`) shared across UI packages.

### App states and focus

`app.Model` has three states: `StateAuth` (QR screen), `StateChat` (main layout), `StateError`. Focus cycles between three panels via Tab: `PanelChatList` → `PanelMessages` → `PanelInput`.

### Layout

Title bar (1 line) + horizontal body (chat list 30% | message view 70%) + input (3 lines) + status bar (1 line). Layout recalculates on every `tea.WindowSizeMsg`.

### Data flow for a new message

1. whatsmeow fires an event → `events.go` converts it to `core.NewMessage`
2. `client.send()` calls the event handler → `program.Send()` → enters `app.Update()`
3. `app.Model.handleNewMessage()` calls `chats.AddMessage()` (alias resolution, dedupe, unread/preview, receipt if the chat is open), updates the chat list and, if the chat is open, calls `chatView.AppendMessage()`
4. It returns cmds that persist the effects via the write queue (conversation upsert, then message insert) and send the read receipt; errors come back as `persistErrMsg`

### Startup sequence

1. Parse flags → open both SQLite databases
2. Create `whatsapp.Client` (no event handler yet)
3. Create `app.Model` with `app.NewWAClient(waClient)` → `tea.NewProgram`
4. `waClient.SetEventHandler(func(e core.Event) { program.Send(e) })`
5. `program.Run()` → `Init()` runs the adapter's `Connect()` cmd, which calls `waClient.Connect(ctx)` (starts QR flow if not logged in)

## Notes

- WhatsApp's unofficial API violates ToS. This is a personal/hobby project.
- whatsmeow handles reconnection automatically; `core.Disconnected` in `StateChat` shows "Reconnecting..." without exiting.
- Message timestamps are stored as Unix int64 in SQLite; `time.Time` is reconstructed on read.
- The `WAClient` and `Store` interfaces in `internal/app/model.go` let tests replace WhatsApp and SQLite with fakes (`helpers_test.go`, `fakestore_test.go`); `driver_test.go` runs returned cmds and feeds their messages back through `Update`. Timers go through `Model.delay` so tests never sleep.
