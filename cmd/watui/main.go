package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/watui/watui/internal/app"
	"github.com/watui/watui/internal/store"
	"github.com/watui/watui/internal/whatsapp"
)

// Set by -ldflags "-X main.version=..."
var version = "dev"

func main() {
	dataDir := flag.String("data-dir", "./data", "path to data directory")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		fatalf("create data directory: %v", err)
	}

	waDBPath := filepath.Join(*dataDir, "whatsmeow.db")
	appDBPath := filepath.Join(*dataDir, "watui.db")

	waClient, err := whatsapp.NewClient(waDBPath)
	if err != nil {
		fatalf("init WhatsApp client: %v", err)
	}
	defer waClient.Disconnect()

	appStore, err := store.New(appDBPath)
	if err != nil {
		fatalf("init app store: %v", err)
	}
	defer appStore.Close()

	model := app.NewModel(waClient, appStore, version)
	program := tea.NewProgram(model, tea.WithAltScreen())
	waClient.SetSendMsg(program.Send)

	if _, err := program.Run(); err != nil {
		fatalf("run program: %v", err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "watui: "+format+"\n", args...)
	os.Exit(1)
}
