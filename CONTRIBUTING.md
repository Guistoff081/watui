# Contributing to watui

## Prerequisites

- Go 1.25+
- GCC (required by `go-sqlite3` for CGo compilation)
- A terminal you can test TUI output in

```bash
git clone https://github.com/Guistoff081/watui
cd watui
go build ./...   # verify it compiles
```

## Project layout

```
cmd/watui/main.go          entry point, wiring
internal/theme/            shared tea.Msg types and data models — no internal imports
internal/whatsapp/         whatsmeow wrapper; bridges events → tea.Msg via p.Send()
internal/store/            app-level SQLite (conversations + messages)
internal/app/              root Bubble Tea model, routes messages, manages focus/layout
internal/ui/               one sub-model per panel (auth, chatlist, chatview, input, statusbar, titlebar)
```

The `internal/theme` package is the hub that all others import. It must not import any other internal package to prevent import cycles.

## Making changes

- Keep sub-models self-contained: each `internal/ui/*` package owns its own state and renders itself. The root `app.Model` only coordinates sizing and focus.
- New event types go in `internal/theme/models.go`; new whatsmeow-to-tea conversions go in `internal/whatsapp/events.go`.
- The `WAClient` interface in `internal/app/app.go` is the boundary between the TUI and WhatsApp. Keep it minimal and add methods there only when a UI feature needs them.
- Run `go vet ./...` before submitting. There are no automated tests yet — manual verification against a real WhatsApp account is required.

## Pull requests

1. Fork → branch off `main` → open a PR against `main`.
2. One feature or fix per PR. Keep the diff small.
3. Describe *what* and *why* in the PR body; reference any related issue.
4. Mark the PR as a draft while it is still a work-in-progress.

## Reporting issues

Open a GitHub issue with:
- What you did
- What you expected
- What actually happened
- Your OS, terminal emulator, and Go version (`go version`)

## Note on WhatsApp ToS

whatsmeow uses WhatsApp's unofficial multi-device protocol, which violates WhatsApp's Terms of Service. Contributions that automate bulk messaging, scrape contacts, or facilitate spam will not be accepted.
