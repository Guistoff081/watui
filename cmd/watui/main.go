package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/watui/watui/internal/app"
	"github.com/watui/watui/internal/config"
	"github.com/watui/watui/internal/debug"
	"github.com/watui/watui/internal/store"
	"github.com/watui/watui/internal/whatsapp"
)

// Set by -ldflags "-X main.version=..."
var version = "dev"

func main() {
	cfg := config.Load()
	dataDir := flag.String("data-dir", cfg.DataDir, "path to data directory")
	debugFlag := flag.Bool("debug", false, "enable debug logging to a file")
	logFile := flag.String("log-file", "", "debug log file path (default: <data-dir>/watui-debug.log)")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		fatalf("create data directory: %v", err)
	}

	debugEnabled := *debugFlag || os.Getenv("WATUI_DEBUG") == "1"

	var logger *debug.Logger
	if debugEnabled {
		logPath := *logFile
		if logPath == "" {
			logPath = filepath.Join(*dataDir, "watui-debug.log")
		}

		var err error
		logger, err = debug.New(logPath)
		if err != nil {
			fatalf("init debug logger: %v", err)
		}
		defer logger.Close()

		defer func() {
			if r := recover(); r != nil {
				logger.LogPanic(r)
				fatalf("panic: %v", r)
			}
		}()

		logger.Info("debug logging enabled", "log_file", logPath, "version", version)
	}

	waDBPath := filepath.Join(*dataDir, "whatsmeow.db")
	appDBPath := filepath.Join(*dataDir, "watui.db")

	waClient, err := whatsapp.NewClient(waDBPath, logger)
	if err != nil {
		if logger != nil {
			logger.Error(err, "init WhatsApp client")
		}
		fatalf("init WhatsApp client: %v", err)
	}
	defer waClient.Disconnect()

	appStore, err := store.New(appDBPath)
	if err != nil {
		if logger != nil {
			logger.Error(err, "init app store")
		}
		fatalf("init app store: %v", err)
	}
	defer appStore.Close()

	model := app.NewModel(waClient, appStore, version, logger)
	program := tea.NewProgram(model, tea.WithAltScreen())
	waClient.SetSendMsg(program.Send)

	if _, err := program.Run(); err != nil {
		if logger != nil {
			logger.Error(err, "run program")
		}
		fatalf("run program: %v", err)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "watui: "+format+"\n", args...)
	os.Exit(1)
}
