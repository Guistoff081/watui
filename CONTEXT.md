# WATUI

### Context

Construir do zero um cliente TUI para WhatsApp com todas as features do WhatsApp Web, começando pelo MVP de core messaging.

O repositório está no inicio, cosntruindo as fundações do projeto

Stack: Go + Bubble Tea (Charm) + whatsmeow.

### Riscos

- APIs não-oficiais do WhatsApp violam ToS. Contas podem ser banidas.
- Projeto para uso pessoal/hobby.

---

### Stack

| Camada | Tecnologia |
| --- | --- |
| Linguagem | Go |
| WhatsApp | [go.mau.fi/whatsmeow](http://go.mau.fi/whatsmeow) (multi-device, WebSocket, Signal protocol) |
| TUI Framework | bubbletea + bubbles + lipgloss (Charm ecosystem) |
| Persistência | SQLite (mattn/go-sqlite3) |
| QR Code | skip2/go-qrcode (bitmap → half-block chars) |
| Config | TOML (BurntSushi/toml) |
| Terminal | Ghostty (suporta Kitty graphics protocol p/ futuro media) |

---

### Estrutura do Projeto

```
watui/
├── cmd/watui/main.go              # Entry point, wiring
├── internal/
│   ├── app/
│   │   ├── app.go                 # Root Bubble Tea model (orchestrator)
│   │   ├── messages.go            # Custom tea.Msg types
│   │   ├── keymap.go              # Key bindings
│   │   └── styles.go              # Lipgloss theme/styles
│   ├── whatsapp/
│   │   ├── client.go              # whatsmeow wrapper, connect/send/events
│   │   ├── events.go              # whatsmeow events → tea.Msg bridge
│   │   └── history.go             # History sync processor
│   ├── ui/
│   │   ├── auth/qr.go             # QR code auth screen
│   │   ├── chatlist/
│   │   │   ├── chatlist.go        # Chat list panel (bubbles/list)
│   │   │   └── item.go            # list.Item for conversations
│   │   ├── chatview/
│   │   │   ├── chatview.go        # Message viewport (bubbles/viewport)
│   │   │   └── message.go         # Message rendering helpers
│   │   ├── input/input.go         # Text input (bubbles/textarea)
│   │   ├── statusbar/statusbar.go # Connection status, info
│   │   └── titlebar/titlebar.go   # Active chat info
│   ├── config/config.go           # TOML config loading
│   └── store/
│       ├── store.go               # App-level SQLite (conversations, messages)
│       ├── models.go              # Conversation, Message structs
│       └── migrations.go          # DB schema
├── go.mod
├── Makefile
└── .gitignore
```

---

### Arquitetura Core: Bridge whatsmeow → Bubble Tea

O desafio central é conectar o modelo event-driven do whatsmeow com o loop Model-View-Update do Bubble Tea.

Solução: `p.Send()` como bridge.

```
whatsmeow WebSocket → events.go handler → c.sendMsg(TypedMsg) → p.Send() → tea.Program loop → app.Update()
```

- `whatsapp.Client` recebe `p.Send` como callback na inicialização.
- Cada evento whatsmeow é traduzido para um `tea.Msg` tipado.
- O root `app.Model` roteia mensagens para os child models apropriados.

#### Sequência de startup

1. Parse flags → Load config → Ensure data dirs
2. Open app SQLite store + run migrations
3. Open whatsmeow sqlstore container
4. Create `whatsapp.Client` (sendMsg = nil temporariamente)
5. Create `app.Model` → Create `tea.Program`
6. Set `waClient.sendMsg = p.Send`
7. [`p.Run](http://p.Run)()` → `Init()` chama `waClient.Connect()` (QR flow começa)

---

### Layout da UI

- **Title bar** (1 linha): nome do contato/grupo + info
- **Corpo**: painel esquerdo (Chat list ~30%) + painel direito (Message view ~70%)
- **Input** (3 linhas): textarea + hint de envio
- **Status bar** (1 linha): conexão + versão + jid

```
┌─────────────────────────────────────────────────────────────┐
│ TITLE BAR: Nome do contato/grupo | Info                      │
├──────────────┬──────────────────────────────────────────────┤
│  CHAT LIST    │  MESSAGE VIEW (viewport, scrollable)         │
│  30% width    │  70% width                                   │
│   > Alice [2] │  Alice                         10:30 AM      │
│     Bob       │  Oi, tudo bem?                               │
│     Grupo [5] │                         Você  10:31 AM       │
│              ├──────────────────────────────────────────────┤
│              │ INPUT: Digite uma mensagem... [Enter: send]   │
├──────────────┴──────────────────────────────────────────────┤
│ STATUS: ● Conectado | watui v0.1 | user@s.whatsapp.net       │
└─────────────────────────────────────────────────────────────┘
```

QR Auth Screen: tela centralizada com QR em half-block chars + spinner.

---

### Key Bindings

| Tecla | Chat List | Message View | Input |
| --- | --- | --- | --- |
| Tab | → Messages | → Input | → Chat List |
| j/↓ | Próximo chat | Scroll down | (texto) |
| k/↑ | Chat anterior | Scroll up | (texto) |
| Enter | Abrir chat | — | Enviar msg |
| i | Focar input | Focar input | — |
| Esc | — | Focar chat list | Limpar/defocar |
| Ctrl+C | Sair | Sair | Sair |
| / | Buscar chats | — | — |
| Ctrl+U/D | — | Page up/down | — |
| g/G | Primeiro/último | Topo/fim | — |

---

### Data Models

```go
type Conversation struct {
	JID, Name    string
	IsGroup      bool
	LastMessage  string
	LastMsgTime  time.Time
	UnreadCount  int
	IsPinned     bool
}

type Message struct {
	ID, ChatJID, SenderJID, SenderName string
	Content    string
	Timestamp  time.Time
	IsFromMe   bool
	Status     string // sending/sent/delivered/read/received/failed
}
```

SQLite schema: tabela `conversations` (PK: jid) + tabela `messages` (PK: id, FK: chat_jid, index por chat+timestamp).

---

### Fases de Implementação

#### Fase 1: Setup + Conexão WhatsApp + QR Auth

Arquivos: `go.mod`, `cmd/watui/main.go`, `internal/whatsapp/client.go`, `internal/whatsapp/events.go`, `internal/app/app.go`, `internal/app/messages.go`, `internal/ui/auth/qr.go`, `.gitignore`

- Inicializar módulo Go com dependências
- whatsmeow wrapper com Connect/Disconnect e QR flow
- Bridge de eventos via `p.Send()`
- Tela de QR code com half-block rendering + spinner
- Root model com estados Auth → Chat (placeholder)

Verificação: rodar app, ver QR no terminal, escanear com WhatsApp, ver "Conectado!".

#### Fase 2: Shell TUI (Layout + Painéis)

Arquivos: `internal/app/styles.go`, `internal/app/keymap.go`, `internal/ui/chatlist/chatlist.go`, `internal/ui/chatlist/item.go`, `internal/ui/chatview/chatview.go`, `internal/ui/input/input.go`, `internal/ui/statusbar/statusbar.go`, `internal/ui/titlebar/titlebar.go`

- Layout split-pane com lipgloss (`JoinHorizontal`/`JoinVertical`)
- Todos os painéis com conteúdo placeholder
- Focus management (Tab cycling, border highlight)
- Resize handling (`WindowSizeMsg` → recalcular dimensões)

Verificação: após auth, ver layout split-pane. Tab entre painéis. Resize funciona.

#### Fase 3: Chat List com Dados Reais

Arquivos: `internal/store/store.go`, `internal/store/models.go`, `internal/store/migrations.go`, `internal/whatsapp/history.go`

- App-level SQLite store (separado do whatsmeow store)
- History sync: processar eventos → popular conversations + messages
- Chat list mostra conversas reais, ordenadas por última mensagem
- Incoming messages atualizam a lista (bump to top, preview, unread)

Verificação: Auth → history sync → conversas reais aparecem. Receber msg → lista atualiza.

#### Fase 4: Exibição de Mensagens

Arquivos: `internal/ui/chatview/message.go`, updates em `chatview.go`, `app.go`, `titlebar.go`

- Selecionar chat → carregar últimas 50 msgs do store
- Rendering: msgs próprias alinhadas à direita, recebidas à esquerda
- Nomes de remetentes em cores distintas (grupos)
- Separadores de data, timestamps
- Scroll com viewport, auto-scroll no fundo
- Novas msgs aparecem em real-time

Verificação: selecionar chat, ver histórico. Scroll up/down. Receber msg → aparece no fundo.

#### Fase 5: Envio de Mensagens

Arquivos: updates em `input.go`, `app.go`, `whatsapp/client.go`, `whatsapp/events.go`

- Enter envia, Shift+Enter nova linha
- UI otimista: msg aparece imediatamente com status "sending"
- `waClient.SendTextMessage()` via `tea.Cmd` assíncrono
- `MessageSentMsg` atualiza status para "sent" ou "failed"
- Receipts (delivered/read) via `*events.Receipt`

Verificação: enviar msg → aparece na hora. Checar outro dispositivo. Receipts atualizam.

#### Fase 6: Polish

Arquivos: `internal/config/config.go`, `Makefile`, updates em statusbar, chatlist, chatview, whatsapp/client

- Config TOML (`~/.config/watui/config.toml`): theme, keybindings, UI options
- Unread badges com clear ao abrir chat + MarkRead()
- Typing indicators (send + receive)
- Reconexão automática (whatsmeow built-in) + status visual
- Error handling: status bar msgs, failed message indicators
- Makefile (build, run, clean, install)
- Lazy-load mensagens antigas ao scrollar para cima

Verificação: uso completo end-to-end. Desconectar WiFi → reconecta. Resize. Unread counts. Typing.

---

### Verificação Final (End-to-End)

1. `make build && ./watui`
2. QR aparece → escanear → conectado
3. Lista de chats carrega com conversas reais
4. Selecionar chat → mensagens aparecem com formatação
5. Enviar mensagem → entrega confirmada no outro dispositivo
6. Receber mensagem → aparece em real-time
7. Tab entre painéis, scroll, resize terminal
8. Ctrl+C → exit limpo