package debug

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/watui/watui/internal/theme"
)

// Logger writes development debug output to a file. All methods are no-ops when
// the receiver is nil.
type Logger struct {
	mu  sync.Mutex
	log *slog.Logger
	f   *os.File
}

// New opens an append-only log file and returns a Logger. Returns an error if
// logPath is empty or the file cannot be opened.
func New(logPath string) (*Logger, error) {
	if logPath == "" {
		return nil, fmt.Errorf("debug: log path is empty")
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("debug: open log file: %w", err)
	}

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return &Logger{
		log: slog.New(handler),
		f:   f,
	}, nil
}

// Writer returns the underlying file writer for sharing with other loggers.
func (l *Logger) Writer() io.Writer {
	if l == nil {
		return io.Discard
	}
	return l.f
}

func (l *Logger) Debug(msg string, attrs ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Debug(msg, attrs...)
}

func (l *Logger) Info(msg string, attrs ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Info(msg, attrs...)
}

func (l *Logger) Warn(msg string, attrs ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Warn(msg, attrs...)
}

// Error logs an error with context and a full stack trace.
func (l *Logger) Error(err error, context string, attrs ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	args := append([]any{"error", err, "stack", string(debug.Stack())}, attrs...)
	l.log.Error(context, args...)
}

// LogPanic logs a recovered panic value with a stack trace.
func (l *Logger) LogPanic(v any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Error("panic recovered",
		"panic", fmt.Sprint(v),
		"stack", string(debug.Stack()),
	)
}

// LogMsg records a Bubble Tea message type and safe metadata. High-frequency
// noise (keys, resize, timers) is skipped.
func (l *Logger) LogMsg(msg tea.Msg) {
	if l == nil || msg == nil || isNoisyMsg(msg) {
		return
	}

	attrs := []any{"msg", fmt.Sprintf("%T", msg)}
	attrs = append(attrs, summarizeMsg(msg)...)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log.Info("app", attrs...)
}

func (l *Logger) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.f.Close()
}

func isNoisyMsg(msg tea.Msg) bool {
	switch fmt.Sprintf("%T", msg) {
	case "tea.KeyMsg", "tea.WindowSizeMsg", "app.clearStatusMsg", "app.typingStopMsg", "spinner.TickMsg":
		return true
	default:
		return false
	}
}

func summarizeMsg(msg tea.Msg) []any {
	switch m := msg.(type) {
	case theme.NewMessageMsg:
		return []any{"chat", m.Message.ChatJID, "id", m.Message.ID, "from_me", m.Message.IsFromMe}
	case theme.MessageSentMsg:
		return []any{"chat", m.ChatJID.String(), "id", m.MessageID}
	case theme.MessageSendFailedMsg:
		attrs := []any{"chat", m.ChatJID.String(), "id", m.MessageID}
		if m.Err != nil {
			attrs = append(attrs, "error", m.Err.Error())
		}
		return attrs
	case theme.MessageStatusMsg:
		return []any{"chat", m.ChatJID.String(), "id", m.MessageID, "status", m.Status}
	case theme.ConnectedMsg:
		return []any{"jid", m.JID.String()}
	case theme.DisconnectedMsg:
		attrs := []any{}
		if m.Err != nil {
			attrs = append(attrs, "error", m.Err.Error())
		}
		return attrs
	case theme.LoginSuccessMsg:
		return []any{"jid", m.JID.String()}
	case theme.LoginFailedMsg:
		if m.Err != nil {
			return []any{"error", m.Err.Error()}
		}
	case theme.QRCodeMsg:
		return []any{"code_len", len(m.Code)}
	case theme.TypingMsg:
		return []any{"chat", m.ChatJID.String(), "sender", m.Sender.String(), "typing", m.IsTyping}
	case theme.ConversationListMsg:
		return []any{"count", len(m.Conversations)}
	case theme.MessagesLoadedMsg:
		return []any{"chat", m.ChatJID.String(), "count", len(m.Messages)}
	case theme.ChatSelectedMsg:
		return []any{"jid", m.JID.String()}
	case theme.ErrorMsg:
		attrs := []any{"context", m.Context}
		if m.Err != nil {
			attrs = append(attrs, "error", m.Err.Error())
		}
		return attrs
	case error:
		return []any{"error", m.Error()}
	}
	return nil
}