# WATUI

### Context

Construir do zero um cliente TUI para WhatsApp com todas as features do WhatsApp Web, começando pelo MVP de core messaging.

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
| QR Code | skip2/go-qrcode (half-block + sextant block chars) |
| Config | TOML (BurntSushi/toml) |
| Terminal | Ghostty (suporta Kitty graphics protocol para futuro media) |
| File picker | zenity / kdialog / qarma / yad (GUI dialog, detectado automaticamente) |

---

### Estrutura do Projeto

```
watui/
├── cmd/watui/main.go              # Entry point, wiring
├── internal/
│   ├── app/
│   │   ├── app.go                 # Root Bubble Tea model (orchestrator)
│   │   ├── messages.go            # (reservado, vazio — tipos estão em theme/)
│   │   ├── keymap.go              # (reservado, vazio — keymaps estão em theme/)
│   │   └── styles.go              # (reservado, vazio — estilos estão em theme/)
│   ├── theme/                     # Pacote central: modelos, tea.Msg, estilos, keymaps
│   │   ├── models.go              # Conversation, Message structs + todos tea.Msg types
│   │   ├── styles.go              # Lipgloss styles compartilhados
│   │   └── keymap.go              # Keybindings compartilhados
│   ├── whatsapp/
│   │   ├── client.go              # whatsmeow wrapper: connect, send, LID resolution
│   │   └── events.go              # whatsmeow events → theme.*Msg bridge
│   ├── ui/
│   │   ├── auth/
│   │   │   ├── qr.go              # QR code auth screen (half-block + sextant rendering)
│   │   │   └── qr_test.go
│   │   ├── chatlist/
│   │   │   ├── chatlist.go        # Chat list panel (bubbles/list)
│   │   │   └── item.go            # list.Item para conversas
│   │   ├── chatview/
│   │   │   ├── chatview.go        # Message viewport (bubbles/viewport) + lazy-load
│   │   │   └── message.go         # Renderização de mensagens
│   │   ├── input/
│   │   │   ├── input.go           # Text input (bubbles/textarea) + path input
│   │   │   ├── filepicker.go      # GUI file picker (zenity/kdialog/qarma/yad)
│   │   │   ├── input_test.go
│   │   │   └── filepicker_test.go
│   │   ├── statusbar/statusbar.go # Conexão, versão, JID
│   │   └── titlebar/titlebar.go   # Nome do chat ativo, typing indicator
│   ├── store/
│   │   ├── store.go               # App-level SQLite (conversations + messages)
│   │   └── migrations.go          # Schema + migrações
│   ├── config/
│   │   ├── config.go              # TOML config loading
│   │   └── config_test.go
│   └── debug/
│       ├── logger.go              # Debug logger (zerolog → arquivo)
│       └── logger_test.go
├── data/                          # gitignored: whatsmeow.db, watui.db, debug.log
├── mise.toml
├── go.mod
├── Makefile
└── .gitignore
```

---

### Arquitetura Core: Bridge whatsmeow → Bubble Tea

O desafio central é conectar o modelo event-driven do whatsmeow com o loop Model-View-Update do Bubble Tea.

Solução: `p.Send()` como bridge.

```
whatsmeow WebSocket → events.go handler → c.sendMsg(theme.Msg) → p.Send() → tea.Program loop → app.Update()
```

- `whatsapp.Client` recebe `p.Send` como callback após criação do programa.
- Cada evento whatsmeow é traduzido para um `tea.Msg` tipado em `theme/`.
- O `internal/theme/` não importa nada do restante do projeto (evita import cycles).
- O root `app.Model` roteia mensagens para os child models.

#### Sequência de startup

1. Parse flags → Load config → Ensure data dirs
2. Open app SQLite store + run migrations
3. Open whatsmeow sqlstore container
4. Create `whatsapp.Client` (sendMsg = nil temporariamente)
5. Create `app.Model` → Create `tea.Program`
6. Set `waClient.sendMsg = p.Send`
7. `p.Run()` → `Init()` chama `waClient.Connect()` (QR flow ou reconexão)

---

### Layout da UI

- **Title bar** (1 linha): nome do contato/grupo + typing indicator
- **Corpo**: painel esquerdo (Chat list ~30%) + painel direito (Message view ~70%)
- **Input** (3 linhas): textarea + hints de atalhos
- **Status bar** (1 linha): conexão + versão + JID

```
┌─────────────────────────────────────────────────────────────┐
│ TITLE BAR: Nome do contato/grupo | Info                      │
├──────────────┬──────────────────────────────────────────────┤
│  CHAT LIST    │  MESSAGE VIEW (viewport, scrollable)         │
│  30% width    │  70% width                                   │
│   > Alice [2] │  Alice                         10:30 AM      │
│     Bob       │  Oi, tudo bem?                               │
│     Grupo [5] │                         Você  10:31 AM ✓✓    │
│              ├──────────────────────────────────────────────┤
│              │ INPUT: Digite uma mensagem...                 │
│              │ ctrl+f attach  ctrl+p audio  ctrl+o browse    │
├──────────────┴──────────────────────────────────────────────┤
│ STATUS: ● Conectado | watui v0.1 | user@s.whatsapp.net       │
└─────────────────────────────────────────────────────────────┘
```

QR Auth Screen: tela centralizada com QR em half-block chars (ou sextant blocks quando necessário) + spinner.

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
| PgUp/PgDn | — | Meia tela up/down | — |
| g/G | — | Topo/fim | — |
| Ctrl+F | — | — | Modo attach (path) |
| Ctrl+P | — | — | Modo audio (path) |
| Ctrl+O | — | — | Abrir GUI file picker |

---

### Data Models

```go
// em internal/theme/models.go

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

SQLite schema: tabela `conversations` (PK: jid) + tabela `messages` (PK: id+chat_jid, index por chat+timestamp).

---

### Fases de Implementação

#### ✅ Fase 1: Setup + Conexão WhatsApp + QR Auth

- Inicializar módulo Go com dependências
- whatsmeow wrapper com Connect/Disconnect e QR flow
- Bridge de eventos via `p.Send()`
- Tela de QR code com half-block rendering + spinner
- Root model com estados Auth → Chat

#### ✅ Fase 2: Shell TUI (Layout + Painéis)

- Layout split-pane com lipgloss (`JoinHorizontal`/`JoinVertical`)
- Focus management (Tab cycling, border highlight)
- Resize handling (`WindowSizeMsg` → recalcular dimensões)
- Status bar, title bar, placeholders

#### ✅ Fase 3: Chat List com Dados Reais

- App-level SQLite store (separado do whatsmeow store)
- History sync: processar `*events.HistorySync` → conversations + messages
- Chat list mostra conversas reais, ordenadas por última mensagem
- Incoming messages atualizam a lista (bump, preview, unread badge)

#### ✅ Fase 4: Exibição de Mensagens

- Selecionar chat → carregar mensagens mais recentes do store
- Rendering: msgs próprias alinhadas à direita, recebidas à esquerda
- Nomes de remetentes em cores distintas (grupos)
- Separadores de data, timestamps, status icons (◷ ✓ ✓✓)
- Scroll com viewport, auto-scroll ao fundo
- Novas mensagens aparecem em real-time
- Lazy-load de mensagens antigas ao scrollar para o topo

#### ✅ Fase 5: Envio de Mensagens

- Enter envia, textarea suporta multi-linha
- ID gerado no cliente (mesmo ID para placeholder e envio real)
- UI otimista: msg aparece imediatamente com status "sending"
- `waClient.SendTextMessage()` via `tea.Cmd` assíncrono
- Status atualizado via `MessageSentMsg` / receipts (`delivered`/`read`)
- Envio de arquivos (Ctrl+F) e áudio/voz (Ctrl+P) como documentos
- GUI file picker integrado (Ctrl+O) via zenity/kdialog/qarma/yad
- Path normalization: shell quoting, backslash escape, tilde expansion

#### ✅ Fase 6: Polish & Resiliência

- Config TOML (`~/.config/watui/config.toml`)
- Unread badges com clear ao abrir chat + `MarkRead()` para WhatsApp
- Typing indicators (envio e recebimento)
- Reconexão automática (whatsmeow built-in) + status visual
- Identificação do device: `Os: "watui"`, `PlatformType: DESKTOP`
- Resolução de nomes LID-aware (`@lid` → telefone → contato)
- Deduplicação de mensagens (group pkmsg+skmsg double-dispatch)
- Merge de history-sync com cache live (sem overwrite de msgs novas)
- Ordenação correta: GetMessages carrega as mais recentes (DESC+reverse)
- Debug logger: zerolog → arquivo (`--debug` flag)
- QR responsivo: half-blocks (1×2) com fallback sextant blocks (2×3)

---

### 🔜 Fase 7: Renderização de Media

**Objetivo:** exibir imagens, figurinhas, GIFs e representar áudios na UI.

- Download de media on-demand via `wm.DownloadAny()` com cache local em `data/media/`
- Imagens e GIFs: renderizar via **Kitty graphics protocol** (Ghostty suporta nativamente); fallback para sixel; fallback textual `[imagem]` + dimensões
- Figurinhas (sticker): mesmo pipeline que imagem (WebP → display)
- GIF animado: exibir primeiro frame estático com indicador `[GIF]`
- Áudio/voz: exibir duração, waveform em ASCII blocks (amplitude aproximada)
- Reprodução de áudio via subprocess (`mpv --no-video` / `ffplay -nodisp` / `aplay`) com tecla de atalho (e.g. `Enter` em mensagem de áudio selecionada)
- Atualizar `theme.Message` para carregar `MediaType`, `MediaURL`, `MediaKey`, `MediaPath` (caminho local do cache)
- Migração do schema: adicionar colunas de media na tabela `messages`

**Verificação:** receber imagem → thumbnail aparece na conversa. Pressionar tecla em áudio → toca no terminal.

---

### 🔜 Fase 8: Gravação e Envio de Áudio

**Objetivo:** gravar áudios PTT diretamente no app, sem depender de arquivo externo.

- Captura de áudio via `arecord` ou `ffmpeg -f alsa` como subprocess (pipe stdout → buffer)
- Encode automático para OGG Opus (padrão WhatsApp): `ffmpeg -i - -c:a libopus`
- Keybinding push-to-talk: `Ctrl+R` inicia gravação, `Enter` confirma e envia, `Esc` cancela
- Feedback visual durante gravação: timer + VU meter em ASCII (amplitude do buffer)
- Arquivo temporário em `data/recordings/` com cleanup após envio
- Integrar com `SendAudioMessage()` existente (já suporta PTT)
- Adicionar `WATUI_AUDIO_DEVICE` env para configurar dispositivo de captura

**Verificação:** pressionar Ctrl+R → gravar → Enter → voz enviada como PTT no WhatsApp.

---

### 🔜 Fase 9: Temas e Esquemas de Cores

**Objetivo:** temas nomeados configuráveis e suporte a esquemas de cores customizados.

- Struct `Theme` com todos os tokens de cor (primary, background, surface, text, dim, error, sent, received, group colors…)
- Temas built-in: `default` (verde WhatsApp), `dark`, `light`, `solarized-dark`, `catppuccin-mocha`, `dracula`
- Carregar tema via `config.toml` → `[theme] name = "catppuccin-mocha"`
- Suporte a override por token: `[theme.colors] primary = "#FF6600"`
- Live reload de tema sem reiniciar (reprocessar estilos lipgloss)
- Cores de remetentes em grupos geradas dinamicamente a partir do JID (hash → cor da paleta do tema)
- Exportar paleta atual como arquivo TOML (comando `watui --export-theme`)

**Verificação:** trocar tema no config → reiniciar → UI em novo esquema de cores. Override de cor individual funciona.

---

### 🔜 Fase 10: Multi-tenant (Múltiplas Contas)

**Objetivo:** suporte a múltiplas contas WhatsApp simultâneas (pessoal + business, etc.).

- Cada conta tem seu próprio diretório: `data/accounts/<account-id>/` com `whatsmeow.db` e `watui.db` separados
- Arquivo de contas: `~/.config/watui/accounts.toml` com lista de contas configuradas
- Startup: inicializar todos os `whatsapp.Client` em paralelo; cada um tem seu `sendMsg` com prefixo de conta
- UI: indicador de conta ativa na title bar / status bar
- Atalho para trocar conta ativa (e.g. `Ctrl+A` abre account switcher overlay)
- Chat list mostra conversas da conta ativa (ou view unificada com badge de conta)
- Notificações de mensagens de contas em background (status bar badge)
- Adicionar `AccountID` em `Conversation` e `Message`; atualizar schema SQLite
- `WAClient` interface permanece a mesma; `app.Model` gerencia slice de clients

**Verificação:** duas contas logadas → trocar entre elas → conversas e mensagens isoladas por conta.

---

### 🔜 Fase 11: Melhorias e Otimizações

**Objetivo:** qualidade de vida, performance e features avançadas.

#### Search
- Full-text search de mensagens via SQLite FTS5 (`CREATE VIRTUAL TABLE messages_fts`)
- Atalho `/` na message view (além do chat list) abre busca global
- Highlight de termos na mensagem

#### Mensagens Avançadas
- Quoted messages / respostas: exibir trecho da mensagem citada acima
- Reações (emoji): exibir agregado de reações abaixo da mensagem
- Edição de mensagem: atualizar conteúdo no store ao receber `*events.Message` com edit flag
- Deleção: remover/ocultar mensagem ao receber evento de delete

#### Notificações
- Integrar `notify-send` / `libnotify` para notificações de desktop
- Configurável: `[notifications] enabled = true`, `sound = true`
- Não notificar chats silenciados

#### Performance
- Virtualização do viewport: renderizar apenas mensagens visíveis (relevante em chats muito longos)
- Cache de rendered lines para evitar re-renderização desnecessária no `rebuildContent()`
- Pool de goroutines para history sync paralelo

#### Contatos e Grupos
- Exibir info do grupo (participantes, subject, foto) em overlay
- Atualizar nomes de contato via `*events.PushName` (já capturado, mas não aplicado ao store)
- Avatar de contato/grupo via Kitty graphics protocol

#### QoL
- Emoji picker básico (categorias + busca) ativado por `:` no input
- Link preview inline (fetch OG tags em background)
- Exportar chat como texto/markdown (`watui --export-chat <jid>`)
- Marcar mensagem como favorita / starred

---

### Verificação Final (End-to-End)

1. `make build && ./watui --data-dir ./data`
2. QR aparece → escanear com WhatsApp → conectado
3. Lista de chats carrega com conversas reais e nomes resolvidos
4. Selecionar chat → mensagens mais recentes aparecem no fundo
5. Enviar mensagem → aparece imediatamente como "◷" → vira "✓" → "✓✓"
6. Receber mensagem → aparece em real-time; painel lateral atualiza
7. Ctrl+F → path do arquivo → Enter → envia documento
8. Ctrl+O → abre zenity → seleciona arquivo → envia
9. Tab entre painéis, scroll, resize terminal
10. Desconectar WiFi → status bar mostra "Reconectando..." → reconecta sozinho
11. Ctrl+C → exit limpo

---

### Notas de Implementação

**LID (`@lid`):** WhatsApp está migrando endereçamento de contatos de `@s.whatsapp.net` para LIDs opacos. A resolução `LID → phone → contact name` é feita via `Store.LIDs.GetPNForLID()`. O mapa inverso é registrado no `GetAllContactNames()`.

**Deduplicação:** mensagens de grupo com `pkmsg` + `skmsg` são despachadas duas vezes pelo whatsmeow. Deduplicadas por `msg.ID` no `handleNewMessage()`.

**IDs de mensagem no cliente:** o ID é gerado localmente via `GenerateMessageID()` e passado como `SendRequestExtra{ID}` para que o placeholder da UI e o eco do servidor usem o mesmo ID.

**QR rendering:** half-blocks (1 módulo/col × 2 módulos/linha) são tentados primeiro. Se o terminal for muito curto, sextant blocks (2×3) reduzem a altura ~40%. O quiet-zone border é preservado (necessário para leitura). Nenhum downsampling é feito (corromperia o QR).
