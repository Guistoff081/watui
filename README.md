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

```bash
git clone https://github.com/elissonguimel/watui
cd watui
go build -o watui ./cmd/watui/
```

## Usage

```bash
./watui
# or with a custom data directory:
./watui --data-dir ~/.local/share/watui
```

On first run a QR code appears — scan it with WhatsApp on your phone (Linked Devices → Link a device). Session is saved locally so subsequent runs connect directly.

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

## Stack

| Layer | Technology |
|---|---|
| Language | Go |
| WhatsApp | [whatsmeow](https://github.com/tulir/whatsmeow) |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [Bubbles](https://github.com/charmbracelet/bubbles) + [Lipgloss](https://github.com/charmbracelet/lipgloss) |
| Storage | SQLite via [go-sqlite3](https://github.com/mattn/go-sqlite3) |

## License

MIT
