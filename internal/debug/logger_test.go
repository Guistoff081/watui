package debug

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/watui/watui/internal/theme"
)

func TestNewCreatesLogFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watui-debug.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Info("startup", "version", "test")
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "startup") {
		t.Errorf("log file missing message, got: %q", content)
	}
	if !strings.Contains(content, "version=test") {
		t.Errorf("log file missing attribute, got: %q", content)
	}
}

func TestErrorIncludesStackTrace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watui-debug.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.Error(errors.New("connection refused"), "whatsapp connect failed")
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "connection refused") {
		t.Errorf("log file missing error, got: %q", content)
	}
	if !strings.Contains(content, "stack=") {
		t.Errorf("log file missing stack trace, got: %q", content)
	}
	if !strings.Contains(content, "goroutine") {
		t.Errorf("log file stack trace looks incomplete, got: %q", content)
	}
}

func TestLogMsgSkipsNoisyMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watui-debug.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.LogMsg(tea.KeyMsg{})
	l.LogMsg(tea.WindowSizeMsg{Width: 80, Height: 24})
	l.LogMsg(spinner.TickMsg{})
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) != 0 {
		t.Errorf("noisy messages should not be logged, got: %q", string(data))
	}
}

func TestLogMsgRecordsAppMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watui-debug.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer l.Close()

	l.LogMsg(theme.NewMessageMsg{
		Message: theme.Message{
			ID:      "ABC123",
			ChatJID: "1234567890@s.whatsapp.net",
		},
	})
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "NewMessageMsg") {
		t.Errorf("log file missing message type, got: %q", content)
	}
	if !strings.Contains(content, "ABC123") {
		t.Errorf("log file missing message id, got: %q", content)
	}
}

func TestNilLoggerIsNoop(t *testing.T) {
	var l *Logger
	l.Debug("test")
	l.Info("test")
	l.Warn("test")
	l.Error(errors.New("err"), "ctx")
	l.LogPanic("panic")
	l.LogMsg(theme.QRCodeMsg{Code: "x"})
	if err := l.Close(); err != nil {
		t.Fatalf("Close() on nil logger error = %v", err)
	}
}