package input

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNormalizePath(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "/home/ti/file.png", "/home/ti/file.png"},
		{"trim spaces", "  /home/ti/file.png  ", "/home/ti/file.png"},
		{"whole single quoted", "'/home/ti/My File.png'", "/home/ti/My File.png"},
		{"whole double quoted", "\"/home/ti/My File.png\"", "/home/ti/My File.png"},
		{"partial single quoted segment", "/home/ti/Imagens/'Captura de tela.png'", "/home/ti/Imagens/Captura de tela.png"},
		{"backslash escaped spaces", "/home/ti/My\\ File.png", "/home/ti/My File.png"},
		{"single quotes keep backslash literal", "'/home/ti/a\\b.png'", "/home/ti/a\\b.png"},
		{"tilde expansion", "~/file.png", filepath.Join(home, "file.png")},
		{"bare tilde", "~", home},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePath(tt.in); got != tt.want {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStartPromptsMatchShortcuts(t *testing.T) {
	m := New()
	m.SetSize(80, 4)
	m.SetFocused(true)

	_ = m.StartFilePrompt()
	if !m.InPathPrompt() || m.mode != modeFile {
		t.Fatalf("StartFilePrompt: mode = %v", m.mode)
	}
	// Enter with a path emits SendFileMsg, as with ctrl+f.
	m.pathInput.SetValue("/tmp/a.png")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !containsMsg(cmd, SendFileMsg{Path: "/tmp/a.png"}) {
		t.Errorf("Enter after StartFilePrompt did not emit SendFileMsg")
	}

	_ = m.StartAudioPrompt()
	if m.mode != modeAudio {
		t.Fatalf("StartAudioPrompt: mode = %v", m.mode)
	}
	m.SetFocused(false)
	if m.InPathPrompt() {
		t.Error("blur must reset the prompt")
	}
	if m.PickFile() == nil {
		t.Error("PickFile returned nil cmd")
	}
}

// containsMsg runs cmd (and nested batches) and reports whether want was produced.
func containsMsg(cmd tea.Cmd, want tea.Msg) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if containsMsg(c, want) {
				return true
			}
		}
		return false
	}
	return msg == want
}
