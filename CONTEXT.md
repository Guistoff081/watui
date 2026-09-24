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
│   ├── app/                       # Root Bubble Tea model (orchestrator)
│   │   ├── model.go               # Model, NewModel, interfaces WAClient + Store, msgs privadas
│   │   ├── update.go              # Switch do Update()
│   │   ├── keys.go                # Teclas, foco, presença "digitando"
│   │   ├── chats.go               # Seleção/abertura de chat, Effects → UI + cmds, recibos
│   │   ├── persist.go             # writeQueue: escritas SQLite ordenadas fora do Update
│   │   ├── media.go / send.go     # Download/abrir mídia; envio texto/arquivo/áudio
│   │   ├── commands.go            # Cmds de carga (store, nomes), timers
│   │   ├── view.go                # Layout + View
│   │   ├── waadapter.go           # WAClient (tea.Cmd) sobre whatsapp.Client síncrono
│   │   └── *_test.go              # fakes (WA, store), driver de cmds, fluxos, unread, mídia
│   ├── core/                      # Domínio sem UI: modelos + eventos + regras
│   │   ├── models.go              # Conversation, Message, PreviewText()
│   │   ├── events.go              # core.Event (NewMessage, Connected, MessageSent…)
│   │   ├── chats.go               # core.Chats: estado de conversas puro → core.Effects
│   │   └── chats_test.go
│   ├── theme/                     # Estilos e keymaps da UI
│   │   ├── styles.go              # Lipgloss styles compartilhados
│   │   └── keymap.go              # Keybindings compartilhados
│   ├── whatsapp/
│   │   ├── client.go              # whatsmeow wrapper síncrono (ctx): connect, send, media
│   │   ├── jid.go                 # Canonicalização LID ↔ PN
│   │   ├── events.go              # whatsmeow events → core events
│   │   ├── client_test.go         # Client contra store whatsmeow offline
│   │   ├── events_test.go / extract_test.go
│   │   └── history_test.go        # Conversão de history sync com resolver fake
│   ├── ui/
│   │   ├── auth/
│   │   │   ├── qr.go              # QR code auth screen (half-block + sextant rendering)
│   │   │   └── qr_test.go
│   │   ├── chatlist/
│   │   │   ├── chatlist.go        # Chat list panel (bubbles/list)
│   │   │   └── item.go            # list.Item para conversas
│   │   ├── chatview/
│   │   │   ├── chatview.go        # Message viewport + cursor de seleção + lazy-load
│   │   │   ├── message.go         # Renderização de mensagens (texto + media)
│   │   │   └── media.go           # Thumbnails half-block (JPEG/PNG/WebP)
│   │   ├── input/
│   │   │   ├── input.go           # Text input (bubbles/textarea) + path input
│   │   │   ├── filepicker.go      # GUI file picker (zenity/kdialog/qarma/yad)
│   │   │   ├── input_test.go
│   │   │   └── filepicker_test.go
│   │   ├── statusbar/statusbar.go # Conexão, versão, JID
│   │   └── titlebar/titlebar.go   # Nome do chat ativo, typing indicator
│   ├── store/
│   │   ├── store.go               # App-level SQLite (conversations + messages + media)
│   │   ├── migrations.go          # Schema + migrações idempotentes
│   │   └── store_test.go / alias_test.go
│   ├── config/
│   │   ├── config.go              # TOML config loading
│   │   └── config_test.go
│   └── debug/
│       ├── logger.go              # Debug logger (zerolog → arquivo)
│       └── logger_test.go
├── data/                          # gitignored: whatsmeow.db, watui.db, media/, debug.log
├── mise.toml
├── go.mod
├── Makefile
└── .gitignore
```

---

### Arquitetura Core: Bridge whatsmeow → Bubble Tea

O desafio central é conectar o modelo event-driven do whatsmeow com o loop Model-View-Update do Bubble Tea. O pacote `internal/whatsapp` não importa Bubble Tea; a ponte tem duas metades:

```
eventos:  whatsmeow WebSocket → events.go handler → c.send(core.Event) → event handler → p.Send() → app.Update()
comandos: app.Update() → WAClient (app/waadapter.go, tea.Cmd) → whatsapp.Client síncrono (ctx, resultado/erro) → evento core como tea.Msg
```

- **Eventos**: `whatsapp.Client.SetEventHandler(func(core.Event))` recebe, em `main.go`, `func(e core.Event) { p.Send(e) }`. Cada evento whatsmeow é traduzido para um evento de domínio em `core/` (struct simples, recebida pelo app como `tea.Msg`).
- **Comandos**: os métodos do client são síncronos e recebem `context.Context` (`Connect(ctx) error`, `SendText/SendFile/SendAudio(ctx, …) (core.MessageSent, error)`, `DownloadMedia(ctx, msg) (path, error)`, `OpenMedia`, `MarkRead`, `SendChatPresence`, …). O adapter `app.NewWAClient` embrulha cada chamada em `tea.Cmd` e mapeia resultado/erro para eventos (`MessageSendFailed`, `MediaDownloaded`/`MediaDownloadFailed`, `LoginFailed`; `*whatsapp.ConnectError` vira a msg privada `connectFailedMsg`, único erro que leva a `StateError`). O fluxo QR bloqueia dentro de `Connect` emitindo `core.QRCode`/`core.QRTimeout` pelo handler.
- O history sync passa por `convertHistoryConversation` (pura, com um resolver para JID canônico/nomes), que desembrulha `EphemeralMessage`, `ViewOnceMessage*`, `DocumentWithCaptionMessage` etc. antes de extrair o conteúdo.
- O `internal/core/` não importa nada do restante do projeto (evita import cycles).
- O root `app.Model` roteia mensagens para os child models. O estado de conversas fica em `core.Chats`, que devolve `core.Effects`; o app atualiza UI na hora e devolve `tea.Cmd`s para SQLite (via `writeQueue`, em ordem: conversas → mensagens → não lidas) e `MarkRead`. Nenhum I/O roda dentro do `Update()`; erros de persistência voltam como `persistErrMsg` (log + status bar).

#### Sequência de startup

1. Parse flags → Load config → Ensure data dirs
2. Open app SQLite store + run migrations
3. Open whatsmeow sqlstore container
4. Create `whatsapp.Client` (sem event handler ainda)
5. Create `app.Model` com `app.NewWAClient(waClient)` → Create `tea.Program`
6. `waClient.SetEventHandler(func(e core.Event) { p.Send(e) })`
7. `p.Run()` → `Init()` roda o cmd `Connect()` do adapter, que chama `waClient.Connect(ctx)` (QR flow ou reconexão)

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
| j/↓ | Próximo chat | Próxima mensagem (cursor) | (texto) |
| k/↑ | Chat anterior | Mensagem anterior (cursor) | (texto) |
| Enter | Abrir chat | Abrir/tocar media selecionada | Enviar msg |
| i | Focar input | Focar input | — |
| Esc | — | Focar chat list | Limpar/defocar |
| Ctrl+C | Sair | Sair | Sair |
| / | Buscar chats | — | — |
| Ctrl+U/Ctrl+D | — | Meia tela up/down | — |
| g/G | — | Topo/fim | — |
| Ctrl+F | — | — | Modo attach (path) |
| Ctrl+P | — | — | Modo audio (path) |
| Ctrl+O | — | — | Abrir GUI file picker |

---

### Data Models

```go
// em internal/core/models.go

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

    // Media (zero = texto puro)
    MediaType, MediaPath, MimeType, FileName string
    Thumbnail                                []byte
    Width, Height, Duration                  int
    IsAnimated                               bool
    DirectPath                               string // + MediaKey, FileSHA256, FileEncSHA256
}
```

`Message.PreviewText()` gera o preview da chat list (`[image] legenda`, `[voice message]`…).

SQLite schema: tabela `conversations` (PK: jid) + tabela `messages` (PK: id+chat_jid, index por chat+timestamp, colunas de media adicionadas por migração idempotente).

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

#### 🟡 Fase 7: Renderização de Media (parcial)

Entregue:
- Metadados de media em `Message` + migração idempotente das colunas em `messages`
- `extractMedia()` para image/video/gif/audio/voice/document/sticker (live + history sync)
- Thumbnails embutidos (JPEG) renderizados em half-blocks 24-bit; figurinhas WebP estáticas decodificadas
- Download on-demand via `DownloadMediaWithPath` com cache em `data/media/` (0o700/0o600, IDs sanitizados)
- Cursor de seleção na message view; `Enter` abre imagem/documento (`xdg-open`) ou toca áudio (`mpv`/`ffplay`/`aplay`)
- Auto-download de figurinhas ao abrir o chat (limite de 10)

Pendente (movido para Fase 13):
- Kitty graphics protocol / sixel para imagens em resolução real
- Primeiro frame de GIF/figurinha animada
- Waveform ASCII para áudio

---

### ✅ Fase 7.5: Estabilização (bugs + testes)

**Objetivo:** corrigir bugs visíveis e criar base de testes antes de refatorar a arquitetura. Entregue como PRs empilhados (`phase-7.5/*`).

- **Doc/infra:** CONTEXT.md atualizado, README (instalação), `main.commit` para o ldflag existente, remoção de arquivos vazios em `app/`
- **Test harness:** fake de `WAClient` + helper de `app.Model` com store SQLite temporário
- **Eventos:** reações, edições, revogações e demais `ProtocolMessage` não viram mais mensagens `[media]`; testes de tabela para `extractTextContent`/`extractMedia`
- **Não lidas / preview / recibos:**
  - Abrir o chat zera `UnreadCount` também no cache `m.conversations` (antes o badge voltava com o valor antigo +1)
  - Mensagens `IsFromMe` (enviadas por outro dispositivo) não incrementam não lidas
  - Preview da conversa usa `PreviewText()` (media sem legenda não deixa preview vazio)
  - Recibos de leitura só para mensagens ainda não lidas; mensagem recebida com o chat aberto é marcada como lida
- **Media:**
  - Auto-download de figurinhas prioriza as mais recentes (visíveis), não as mais antigas
  - Falha de download aparece na status bar e limpa o `pendingOpenMsgID`

Backlog conhecido (não incluído na 7.5):
- Possível reconexão dupla: app agenda `Connect()` enquanto whatsmeow já reconecta sozinho; após 5 tentativas o app para em silêncio
- Lazy-load de mensagens antigas não consulta aliases LID↔PN
- History sync de grupos sem `SenderName`
- Typing indicator exibe telefone em vez do nome e não resolve alias
- `go install github.com/watui/watui/...` não funciona: module path ≠ repositório (`Guistoff081/watui`)

---

### ✅ Fase 8: Core desacoplado (refactor para testabilidade)

**Objetivo:** separar domínio e sessão WhatsApp do Bubble Tea — pré-requisito para daemon, plugin Omarchy e multi-conta.

- Novo pacote `internal/core`: regras de conversas (merge, aliases, não lidas, preview, recibos) sem dependência de `tea`
- `whatsapp.Client` emite eventos de domínio (`chan core.Event` / callback) e expõe métodos síncronos com `context.Context`; adapter fino converte para `tea.Msg`/`tea.Cmd`
- Interface para o store no `app`/`core`; I/O de SQLite fora do `Update()` (via `tea.Cmd`); erros de persistência logados em vez de `_ =`
- Quebrar `app.go` (~1000 linhas) em handlers por domínio
- Metas de cobertura: `core` ≥ 80%, `whatsapp` (conversão de eventos) ≥ 60%, `app` ≥ 50%

**Verificação:** `go test ./...` roda sem terminal nem rede; fluxo de mensagem testado ponta a ponta com fakes.

PRs empilhados (`phase-8/*`):
1. `01-core-package` — modelos + eventos de `theme` → `internal/core` (sufixo `Msg` removido, interface selada `core.Event`); CI roda em PRs empilhados e checa gofmt
2. `02-whatsapp-sync` ∥ `03-core-chats` (paralelos) — `whatsapp` sem `tea` (API síncrona + adapter `tea.Cmd` no `app`); motor de estado `core.Chats` puro com efeitos
3. `04-async-io` — interface de store, SQLite e recibos fora do `Update()` via `tea.Cmd` (fila de escrita ordenada, abertura de chat assíncrona com guarda de troca), erros de persistência na status bar; só falha de conexão leva a `StateError`
4. `05-split-app` — `app.go` quebrado por domínio + metas de cobertura (atingido: `app` 95%, `core` 99%, `whatsapp` 90%) + docs de arquitetura

---

### 🔜 Fase 9: Daemon `watuid` + plugin Omarchy (Quickshell)

**Objetivo:** a sessão WhatsApp roda em um daemon; TUI e shell do Omarchy são clientes.

Motivação: o mesmo device whatsmeow não pode ser aberto por dois processos, então o plugin do shell precisa de um backend compartilhado.

```
watuid (systemd --user)
  ├─ whatsmeow + store SQLite
  └─ $XDG_RUNTIME_DIR/watui.sock  (NDJSON versionado: requests + stream de eventos)
        ├─ watui (TUI) — cliente do socket
        └─ plugin Quickshell — Socket + SplitParser (padrão de crmne.hyprmoncfg)
```

- Protocolo: `{type:"request",protocol_version:1,id,method,params}` / `{type:"event",...}`; métodos `subscribe`, `listChats`, `getMessages`, `send`, `markRead`, `pairQR`
- `watui` detecta o socket e vira cliente; sem daemon, mantém modo embutido
- Plugin em `~/.config/omarchy/plugins/watui/` (`manifest.json`, kinds `bar-widget` + `panel` + `service`):
  - Bar widget: total de não lidas + estado de conexão (cores via `qs.Commons.Color`)
  - Panel: chats recentes, preview, resposta rápida, QR de pareamento em QML
  - Notificações via `omarchy-notification-send` (respeita chats silenciados)
  - `IpcHandler { target: "watui" }` → `toggle`, `openChat`, `markAllRead`; binds Hyprland via `omarchy-shell watui toggle`
  - Abrir TUI completa: `omarchy-launch-or-focus-tui --app-id=TUI.float watui`
  - Settings via `barWidget.schema` (notificações, tamanho do preview)
- Validação: `qmllint`, `omarchy plugin validate .`

**Verificação:** daemon ativo → badge na barra atualiza ao receber mensagem → resposta rápida pelo painel aparece na TUI aberta.

---

### 🔜 Fase 10: Gravação e Envio de Áudio

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

### 🔜 Fase 11: Temas e Esquemas de Cores

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

### 🔜 Fase 12: Multi-tenant (Múltiplas Contas)

**Objetivo:** suporte a múltiplas contas WhatsApp simultâneas (pessoal + business, etc.).

- Cada conta tem seu próprio diretório: `data/accounts/<account-id>/` com `whatsmeow.db` e `watui.db` separados
- Arquivo de contas: `~/.config/watui/accounts.toml` com lista de contas configuradas
- Startup: inicializar todos os `whatsapp.Client` em paralelo; cada um com seu `SetEventHandler` embrulhando os eventos com o ID da conta
- UI: indicador de conta ativa na title bar / status bar
- Atalho para trocar conta ativa (e.g. `Ctrl+A` abre account switcher overlay)
- Chat list mostra conversas da conta ativa (ou view unificada com badge de conta)
- Notificações de mensagens de contas em background (status bar badge)
- Adicionar `AccountID` em `Conversation` e `Message`; atualizar schema SQLite
- Com o daemon da Fase 9, `watuid` gerencia os clients; TUI/plugin escolhem a conta ativa

**Verificação:** duas contas logadas → trocar entre elas → conversas e mensagens isoladas por conta.

---

### 🔜 Fase 13: Melhorias e Otimizações

**Objetivo:** qualidade de vida, performance e features avançadas.

#### Media (restante da Fase 7)
- Kitty graphics protocol (Ghostty) com fallback sixel → half-block
- Primeiro frame de GIF/figurinha animada com indicador `[GIF]`
- Waveform ASCII para áudio/voz

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
