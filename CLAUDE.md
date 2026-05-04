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

The central design challenge is connecting whatsmeow's event-driven WebSocket model to Bubble Tea's MVU loop. The bridge is `program.Send` passed as a callback:

```
whatsmeow WebSocket → internal/whatsapp/events.go → client.send(tea.Msg) → program.Send() → app.Update()
```

`whatsapp.Client` holds a `sendMsg func(tea.Msg)` that is nil until `SetSendMsg` is called in `main.go` after `tea.NewProgram` is created but before `program.Run()`.

### Package layout

- **`internal/theme/`** — shared data models (`Conversation`, `Message`) and all `tea.Msg` types. Every package imports this; it imports nothing from the rest of the codebase. This avoids import cycles.
- **`internal/whatsapp/`** — wraps `go.mau.fi/whatsmeow`. `client.go` exposes the `WAClient` interface. `events.go` translates whatsmeow events to `theme.*Msg` types and calls `client.send()`.
- **`internal/store/`** — app-level SQLite (not whatsmeow's own store). Stores `conversations` and `messages` tables. Migrations are in `migrations.go`.
- **`internal/app/`** — root Bubble Tea model (`app.Model`). Routes all `tea.Msg` to sub-models, manages focus, handles layout. Holds in-memory caches (`chatMessages map[string][]theme.Message`, `conversations map[string]theme.Conversation`).
- **`internal/ui/`** — sub-models for each UI panel: `auth/qr.go`, `chatlist/`, `chatview/`, `input/`, `statusbar/`, `titlebar/`.
- **`internal/theme/`** — also holds lipgloss styles (`styles.go`) and keymap (`keymap.go`) shared across UI packages.

### App states and focus

`app.Model` has three states: `StateAuth` (QR screen), `StateChat` (main layout), `StateError`. Focus cycles between three panels via Tab: `PanelChatList` → `PanelMessages` → `PanelInput`.

### Layout

Title bar (1 line) + horizontal body (chat list 30% | message view 70%) + input (3 lines) + status bar (1 line). Layout recalculates on every `tea.WindowSizeMsg`.

### Data flow for a new message

1. whatsmeow fires an event → `events.go` converts it to `theme.NewMessageMsg`
2. `client.send()` calls `program.Send()` → enters `app.Update()`
3. `app.Model.handleNewMessage()` appends to in-memory cache, persists to `store`, updates chat list, and if the chat is open, calls `chatView.AppendMessage()`

### Startup sequence

1. Parse flags → open both SQLite databases
2. Create `whatsapp.Client` (sendMsg = nil)
3. Create `app.Model` → `tea.NewProgram`
4. `waClient.SetSendMsg(program.Send)`
5. `program.Run()` → `Init()` calls `waClient.Connect()` (starts QR flow if not logged in)

## Notes

- WhatsApp's unofficial API violates ToS. This is a personal/hobby project.
- whatsmeow handles reconnection automatically; `DisconnectedMsg` in `StateChat` shows "Reconnecting..." without exiting.
- Message timestamps are stored as Unix int64 in SQLite; `time.Time` is reconstructed on read.
- The `WAClient` interface in `app.go` allows the whatsapp package to be mocked in tests.
