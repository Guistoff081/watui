package whatsapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog"
	"github.com/watui/watui/internal/core"
	"github.com/watui/watui/internal/debug"
	"go.mau.fi/whatsmeow"
	waCompanionReg "go.mau.fi/whatsmeow/proto/waCompanionReg"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type Client struct {
	wm       *whatsmeow.Client
	store    *sqlstore.Container
	onEvent  func(core.Event)
	mu       sync.Mutex
	log      waLog.Logger
	dbg      *debug.Logger
	mediaDir string

	// makePoster extracts a still frame from src into dst (PNG). Injected so
	// tests don't need ffmpeg; see ffmpegPoster.
	makePoster func(ctx context.Context, src, dst string) error
}

func NewClient(dbPath, mediaDir string, dbg *debug.Logger) (*Client, error) {
	container, err := sqlstore.New(
		context.Background(),
		"sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", dbPath),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("whatsmeow store: %w", err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}

	// Identify the linked device. These props are sent during pairing, so an
	// already-paired session keeps its old "(unknown)" name until it is re-linked.
	store.DeviceProps.Os = proto.String("watui")
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()

	var log waLog.Logger
	if dbg != nil {
		zl := zerolog.New(dbg.Writer()).
			With().
			Timestamp().
			Str("module", "whatsapp").
			Logger().
			Level(zerolog.DebugLevel)
		log = waLog.Zerolog(zl)
	} else {
		log = waLog.Stdout("whatsapp", "WARN", true)
	}
	wm := whatsmeow.NewClient(deviceStore, log)

	c := &Client{
		wm:         wm,
		store:      container,
		log:        log,
		dbg:        dbg,
		mediaDir:   mediaDir,
		makePoster: ffmpegPoster,
	}

	wm.AddEventHandler(c.handleEvent)

	return c, nil
}

// SetEventHandler sets the callback that receives domain events (connection
// state, QR codes, incoming messages, receipts, history sync). It is called from
// whatsmeow's event goroutines, so fn must be safe for concurrent use. Must be
// called before Connect.
func (c *Client) SetEventHandler(fn func(core.Event)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEvent = fn
}

func (c *Client) send(evt core.Event) {
	c.mu.Lock()
	fn := c.onEvent
	c.mu.Unlock()
	if fn != nil {
		fn(evt)
	}
}

// ConnectError reports that the websocket connection could not be opened
// before QR pairing started. It is distinct from a login failure: nothing was
// rejected by WhatsApp, the transport simply never came up.
type ConnectError struct{ Err error }

func (e *ConnectError) Error() string { return "connect: " + e.Err.Error() }
func (e *ConnectError) Unwrap() error { return e.Err }

// Connect starts the WhatsApp connection and blocks until it is established or
// fails.
//
// If the device is not logged in yet, Connect runs the QR flow: each code is
// emitted as core.QRCode (and expiry as core.QRTimeout) through the event
// handler, and Connect returns nil once pairing succeeds (the rest of the login
// arrives as events) or when the QR channel closes after a timeout. A failure to
// open the socket before pairing is returned as *ConnectError; a pairing error
// is returned as-is.
//
// ctx bounds the QR wait only. The socket itself outlives the call: its
// lifetime is managed by whatsmeow (and Disconnect), not by ctx.
//
// Connecting while already connected is not an error.
func (c *Client) Connect(ctx context.Context) error {
	if c.wm.Store.ID != nil {
		err := c.wm.Connect()
		if errors.Is(err, whatsmeow.ErrAlreadyConnected) {
			return nil
		}
		return err
	}

	// Not logged in — start QR code flow.
	qrChan, _ := c.wm.GetQRChannel(ctx)
	if err := c.wm.Connect(); err != nil {
		return &ConnectError{Err: err}
	}
	return c.waitQR(qrChan)
}

// waitQR relays QR channel items to the event handler until the channel
// reaches a terminal state.
func (c *Client) waitQR(qrChan <-chan whatsmeow.QRChannelItem) error {
	for evt := range qrChan {
		switch evt.Event {
		case "code":
			c.send(core.QRCode{Code: evt.Code})
		case "timeout":
			if c.dbg != nil {
				c.dbg.Warn("QR code session timed out")
			}
			c.send(core.QRTimeout{})
		case "success":
			// The login itself is reported by the event handler.
			return nil
		case "error":
			err := evt.Error
			if err == nil {
				err = fmt.Errorf("QR login failed")
			}
			if c.dbg != nil {
				c.dbg.Error(err, "QR login error")
			}
			return err
		default:
			err := fmt.Errorf("QR pairing failed: %s", evt.Event)
			if evt.Error != nil {
				err = fmt.Errorf("QR pairing failed: %s: %w", evt.Event, evt.Error)
			}
			if c.dbg != nil {
				c.dbg.Error(err, "QR channel terminal event")
			}
			return err
		}
	}
	return nil
}

// Disconnect cleanly disconnects from WhatsApp.
func (c *Client) Disconnect() {
	if c.wm != nil {
		c.wm.Disconnect()
	}
}

// GenerateMessageID returns a new client-side message ID. Generating it up front
// lets the UI use the same ID for its optimistic placeholder and the actual send,
// so status updates (and the server's own echo) line up.
func (c *Client) GenerateMessageID() string {
	return string(c.wm.GenerateMessageID())
}

// SendText sends a text message to jid using the provided message ID.
func (c *Client) SendText(ctx context.Context, jid types.JID, id, text string) (core.MessageSent, error) {
	return c.sendMessage(ctx, jid, id, &waE2E.Message{
		Conversation: proto.String(text),
	})
}

func (c *Client) sendMessage(ctx context.Context, jid types.JID, id string, msg *waE2E.Message) (core.MessageSent, error) {
	resp, err := c.wm.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: types.MessageID(id)})
	if err != nil {
		return core.MessageSent{}, err
	}
	return core.MessageSent{ChatJID: jid, MessageID: resp.ID, Timestamp: resp.Timestamp}, nil
}

const maxUploadSize = 64 << 20 // 64 MB

// ErrFileTooLarge is returned when a file exceeds maxUploadSize.
var ErrFileTooLarge = errors.New("file too large (max 64 MB)")

// readUpload reads path for upload, enforcing the size limit.
func readUpload(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(data) > maxUploadSize {
		return nil, ErrFileTooLarge
	}
	return data, nil
}

// SendFile reads path from disk, uploads it to WhatsApp, and sends it as a document.
func (c *Client) SendFile(ctx context.Context, jid types.JID, id, path string) (core.MessageSent, error) {
	data, err := readUpload(path)
	if err != nil {
		return core.MessageSent{}, err
	}

	mimeType := detectMIME(path, data)

	uploaded, err := c.wm.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return core.MessageSent{}, fmt.Errorf("upload: %w", err)
	}

	return c.sendMessage(ctx, jid, id, &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Mimetype:      proto.String(mimeType),
			FileName:      proto.String(filepath.Base(path)),
		},
	})
}

// SendAudio reads path from disk, uploads it, and sends it as a PTT voice message.
// The file should be OGG Opus for best compatibility with WhatsApp clients.
func (c *Client) SendAudio(ctx context.Context, jid types.JID, id, path string) (core.MessageSent, error) {
	data, err := readUpload(path)
	if err != nil {
		return core.MessageSent{}, err
	}

	mimeType := detectMIME(path, data)

	uploaded, err := c.wm.Upload(ctx, data, whatsmeow.MediaAudio)
	if err != nil {
		return core.MessageSent{}, fmt.Errorf("upload: %w", err)
	}

	return c.sendMessage(ctx, jid, id, &waE2E.Message{
		AudioMessage: &waE2E.AudioMessage{
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uploaded.FileLength),
			Mimetype:      proto.String(mimeType),
			PTT:           proto.Bool(true),
		},
	})
}

// detectMIME returns the MIME type for path, using file extension first and
// content sniffing as a fallback.
func detectMIME(path string, data []byte) string {
	if t := mime.TypeByExtension(filepath.Ext(path)); t != "" {
		return t
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	return http.DetectContentType(sniff)
}

// MarkRead sends read receipts for the given message IDs.
func (c *Client) MarkRead(ctx context.Context, chatJID, sender types.JID, messageIDs []string) error {
	ids := make([]types.MessageID, 0, len(messageIDs))
	for _, id := range messageIDs {
		ids = append(ids, types.MessageID(id))
	}
	return c.wm.MarkRead(ctx, ids, time.Now(), chatJID.ToNonAD(), sender.ToNonAD())
}

// SendPresence sets the user's presence (available/unavailable).
func (c *Client) SendPresence(ctx context.Context, available bool) error {
	if available {
		return c.wm.SendPresence(ctx, types.PresenceAvailable)
	}
	return c.wm.SendPresence(ctx, types.PresenceUnavailable)
}

// SendChatPresence sends typing/stopped indicator.
func (c *Client) SendChatPresence(ctx context.Context, jid types.JID, composing bool) error {
	if composing {
		return c.wm.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	}
	return c.wm.SendChatPresence(ctx, jid, types.ChatPresencePaused, types.ChatPresenceMediaText)
}

// GetContactName returns the best available name for a JID from whatsmeow's
// contact store, or "" when unknown. Contacts are keyed by phone-number JID, so
// LID (@lid) JIDs are first mapped back to their phone number before lookup.
func (c *Client) GetContactName(ctx context.Context, jid types.JID) string {
	lookup := jid
	if jid.Server == types.HiddenUserServer {
		if pn, err := c.wm.Store.LIDs.GetPNForLID(ctx, jid); err == nil && pn.User != "" {
			lookup = pn
		}
	}
	contact, err := c.wm.Store.Contacts.GetContact(ctx, lookup)
	if err != nil || !contact.Found {
		return ""
	}
	return bestContactName(contact.FullName, contact.PushName, contact.BusinessName)
}

func bestContactName(full, push, business string) string {
	if full != "" {
		return full
	}
	if push != "" {
		return push
	}
	return business
}

// GetAllContactNames returns a map of JID string -> best name for all contacts.
// Each name is registered under both the phone-number JID and (when known) the
// matching LID JID, since WhatsApp increasingly addresses chats by LID.
func (c *Client) GetAllContactNames(ctx context.Context) (map[string]string, error) {
	contacts, err := c.wm.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(contacts))
	for jid, contact := range contacts {
		name := bestContactName(contact.FullName, contact.PushName, contact.BusinessName)
		if name == "" {
			continue
		}
		names[jid.String()] = name
		if jid.Server == types.DefaultUserServer {
			if lid, err := c.wm.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && lid.User != "" {
				names[lid.String()] = name
			}
		}
	}
	return names, nil
}

// GetGroupNames returns a map of JID string -> group name for all joined groups.
func (c *Client) GetGroupNames(ctx context.Context) (map[string]string, error) {
	groups, err := c.wm.GetJoinedGroups(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(groups))
	for _, g := range groups {
		if g.Name != "" {
			names[g.JID.String()] = g.Name
		}
	}
	return names, nil
}

// JID returns the current user's JID (nil if not logged in).
func (c *Client) JID() *types.JID {
	return c.wm.Store.ID
}

// WMClient returns the underlying whatsmeow client for advanced operations.
func (c *Client) WMClient() *whatsmeow.Client {
	return c.wm
}

// DownloadMedia downloads msg's media to the local cache (unless it is already
// there) and returns the cached file path.
func (c *Client) DownloadMedia(ctx context.Context, msg core.Message) (string, error) {
	if msg.DirectPath == "" || len(msg.MediaKey) == 0 {
		return "", fmt.Errorf("no download metadata for message %s", msg.ID)
	}

	ext := extFromMime(msg.MimeType)
	cachePath, err := mediaCachePath(c.mediaDir, msg.ID, ext)
	if err != nil {
		return "", fmt.Errorf("unsafe message ID: %w", err)
	}

	// Already cached — return immediately without a network call.
	if _, err := os.Stat(cachePath); err == nil {
		c.ensurePoster(ctx, msg, cachePath)
		return cachePath, nil
	}

	data, err := c.wm.DownloadMediaWithPath(
		ctx,
		msg.DirectPath,
		msg.FileEncSHA256,
		msg.FileSHA256,
		msg.MediaKey,
		waMediaType(msg.MediaType),
		mmsType(msg.MediaType),
		false,
	)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	// 0o600: media files are user-private (may contain personal content).
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		return "", fmt.Errorf("write cache: %w", err)
	}
	c.ensurePoster(ctx, msg, cachePath)
	return cachePath, nil
}

// ensurePoster writes the still-frame preview for media that needs one
// (core.Message.NeedsPoster) if it doesn't exist yet. It is best-effort: a
// missing ffmpeg only costs the inline preview, never the download.
func (c *Client) ensurePoster(ctx context.Context, msg core.Message, path string) {
	if !msg.NeedsPoster() || c.makePoster == nil {
		return
	}
	poster := core.PosterPath(path)
	if _, err := os.Stat(poster); err == nil {
		return
	}
	if err := c.makePoster(ctx, path, poster); err != nil && c.dbg != nil {
		c.dbg.Warn("poster extraction failed", "msg", msg.ID, "error", err.Error())
	}
}

// ffmpegPoster extracts the first frame of src (animated WebP, MP4…) as PNG.
func ffmpegPoster(ctx context.Context, src, dst string) error {
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-y", "-i", abs, "-frames:v", "1", dst).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return os.Chmod(dst, 0o600)
}

// OpenMedia opens path in an appropriate external application. Audio/voice
// messages go to the first available player, which is started and not waited
// for; everything else goes to xdg-open, which is run to completion (it returns
// as soon as the viewer launches) so a missing handler is reported.
func (c *Client) OpenMedia(path, mediaType string) error {
	cmd, err := mediaOpenCommand(path, mediaType, exec.LookPath)
	if err != nil {
		return err
	}
	if filepath.Base(cmd.Path) == "xdg-open" {
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("xdg-open: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the player when it exits
	return nil
}

// mediaOpenCommand builds the command OpenMedia runs. lookPath is injected so
// player selection can be tested without the players installed.
//
// The path is made absolute, so it always starts with "/" and can never be
// mistaken for a flag. That replaces a "--" separator, which xdg-open rejects
// as an unknown option (it then opened nothing).
func mediaOpenCommand(path, mediaType string, lookPath func(string) (string, error)) (*exec.Cmd, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve media path: %w", err)
	}
	switch mediaType {
	case "audio", "voice":
		for _, player := range []string{"mpv", "ffplay", "aplay"} {
			if _, err := lookPath(player); err == nil {
				return exec.Command(player, abs), nil
			}
		}
	case "gif", "sticker":
		// Image viewers like imv show animated WebP as a black window, and
		// GIFs are really MP4s: loop them in mpv when available.
		if mediaType == "gif" || isAnimatedWebP(abs) {
			if _, err := lookPath("mpv"); err == nil {
				return exec.Command("mpv", "--loop=inf", abs), nil
			}
		}
	}
	return exec.Command("xdg-open", abs), nil
}

// isAnimatedWebP reports whether path is an extended WebP (VP8X) with the
// animation flag set.
func isAnimatedWebP(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var h [21]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return false
	}
	return string(h[0:4]) == "RIFF" && string(h[8:16]) == "WEBPVP8X" && h[20]&0x02 != 0
}

// waMediaType maps a theme media-type string to the whatsmeow MediaType constant.
func waMediaType(mt string) whatsmeow.MediaType {
	switch mt {
	case "video", "gif":
		return whatsmeow.MediaVideo
	case "audio", "voice":
		return whatsmeow.MediaAudio
	case "document":
		return whatsmeow.MediaDocument
	default: // image, sticker
		return whatsmeow.MediaImage
	}
}

// mmsType maps a theme media-type string to the WhatsApp mms type string.
func mmsType(mt string) string {
	switch mt {
	case "video", "gif":
		return "video"
	case "audio", "voice":
		return "audio"
	case "document":
		return "document"
	default:
		return "image"
	}
}

// mediaCachePath returns an absolute path inside mediaDir for a message ID + extension.
// The message ID is sanitized to prevent path traversal: only alphanumeric characters,
// hyphens, and underscores are kept. If the sanitized result is empty, the SHA-256 hex
// of the raw ID is used. The final path is verified to lie within mediaDir.
func mediaCachePath(mediaDir, msgID, ext string) (string, error) {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, msgID)
	if safe == "" {
		h := sha256.Sum256([]byte(msgID))
		safe = hex.EncodeToString(h[:])
	}

	p := filepath.Join(mediaDir, safe+ext)

	// Guard: verify the resolved path is still inside mediaDir.
	cleanDir := filepath.Clean(mediaDir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(p)+string(os.PathSeparator), cleanDir) {
		return "", fmt.Errorf("path %q escapes media directory", p)
	}
	return p, nil
}

// extFromMime returns a file extension (with leading dot) for a MIME type.
func extFromMime(mimeType string) string {
	// Strip parameters (e.g. "image/jpeg; charset=utf-8" → "image/jpeg").
	if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	// Common WhatsApp types first: the system MIME table can list rarer
	// extensions first (.jfif for JPEG, .f4v for MP4), which viewers and
	// xdg-open handle worse.
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "audio/ogg":
		return ".ogg"
	case "audio/mpeg":
		return ".mp3"
	}
	if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
		return exts[0]
	}
	return ""
}
