package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	DataDir string   `toml:"data_dir"`
	UI      UIConfig `toml:"ui"`
}

type UIConfig struct {
	// Reserved for future theme and keybinding overrides.
}

func defaults() Config {
	home, _ := os.UserHomeDir()
	return Config{
		DataDir: filepath.Join(home, ".local", "share", "watui"),
	}
}

// Load reads ~/.config/watui/config.toml and returns its values merged over
// the built-in defaults. Missing file or parse errors silently fall back to
// defaults so the app always starts.
func Load() Config {
	cfg := defaults()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg
	}

	path := filepath.Join(home, ".config", "watui", "config.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return defaults()
	}

	return cfg
}
