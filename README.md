# watui

A terminal UI client for WhatsApp, built with Go + [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [whatsmeow](https://github.com/tulir/whatsmeow).

> **Warning:** Uses WhatsApp's unofficial multi-device API, which violates WhatsApp's Terms of Service. Use at your own risk. Personal/hobby project only.

## Features

- QR code login (scan with WhatsApp mobile)
- Conversation list with unread counts
- Real-time message receive and send
- Message history with scroll
- Typing indicators
- Delivery/read receipts
- Auto-reconnect on disconnect

## Requirements

- Go 1.25+
- GCC (for `go-sqlite3` CGo build)
- A terminal with good Unicode support (e.g. [Ghostty](https://ghostty.org/))

## Install

### Option 1 — go install (quickest)

```bash
go install github.com/watui/watui/cmd/watui@latest
```

Installs the `watui` binary to `$(go env GOPATH)/bin` (usually `~/go/bin`). Requires GCC for the SQLite CGo dependency.

### Option 2 — build from source

```bash
git clone https://github.com/Guistoff081/watui
cd watui
make install        # installs to ~/go/bin
# or just build locally:
make build          # produces ./watui
```

### Option 3 — release archive

Download a pre-built archive from the [Releases](https://github.com/Guistoff081/watui/releases) page, extract, and place the binary on your `$PATH`:

```bash
tar xzf watui-<version>-linux-amd64.tar.gz
sudo mv watui /usr/local/bin/
```

### Build your own archive

```bash
make dist                        # current platform
GOOS=linux GOARCH=arm64 make dist   # cross-compile (requires matching C cross-compiler)
```

Archives are placed in `dist/`.

## Usage

```bash
./watui
# or with a custom data directory:
./watui --data-dir ~/.local/share/watui
```

On first run a QR code appears — scan it with WhatsApp on your phone (Linked Devices → Link a device). Session is saved locally so subsequent runs connect directly.

## Debug logging (development)

Enable file-based debug logging with the `--debug` flag or `WATUI_DEBUG=1`. Logs are written to `<data-dir>/watui-debug.log` (override with `--log-file`).

Run in two terminals:

```bash
# Terminal 1 — start watui with debug logging
make run-debug
# or: WATUI_DEBUG=1 ./watui --data-dir ./data

# Terminal 2 — watch the debug console
tail -f ./data/watui-debug.log
```

The log captures Bubble Tea message flow, whatsmeow events at DEBUG level, errors with stack traces, and panics. Message content is not logged — only metadata (chat JID, message ID, etc.).

## Key Bindings

| Key | Context | Action |
|---|---|---|
| `Tab` / `Shift+Tab` | Global | Cycle focus between panels |
| `j` / `↓` | Chat list | Next conversation |
| `k` / `↑` | Chat list | Previous conversation |
| `Enter` | Chat list | Open conversation |
| `j` / `↓` | Message view | Scroll down |
| `k` / `↑` | Message view | Scroll up |
| `g` / `G` | Message view | Top / bottom |
| `Ctrl+U` / `Ctrl+D` | Message view | Page up / down |
| `i` | Chat / messages | Focus input |
| `Enter` | Input | Send message |
| `Shift+Enter` | Input | New line |
| `Esc` | Input / messages | Back to chat list |
| `Ctrl+C` | Global | Quit |

## Data

All data is stored in `./data/` by default (override with `--data-dir`):

- `whatsmeow.db` — WhatsApp session keys (whatsmeow-managed)
- `watui.db` — Conversations and messages (app-managed)
- `watui-debug.log` — Debug log (only when `--debug` or `WATUI_DEBUG=1` is set)

## Stack

| Layer | Technology |
|---|---|
| Language | Go |
| WhatsApp | [whatsmeow](https://github.com/tulir/whatsmeow) |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Bubbles](https://github.com/charmbracelet/bubbles) + [Lipgloss](https://github.com/charmbracelet/lipgloss) |
| Storage | SQLite via [go-sqlite3](https://github.com/mattn/go-sqlite3) |

## License

MIT
