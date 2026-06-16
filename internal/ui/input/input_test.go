package input

import (
	"os"
	"path/filepath"
	"testing"
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
