package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsNonEmpty(t *testing.T) {
	cfg := defaults()
	if cfg.DataDir == "" {
		t.Error("default DataDir must not be empty")
	}
}

func withHome(t *testing.T, dir string) {
	t.Helper()
	orig, set := os.LookupEnv("HOME")
	os.Setenv("HOME", dir)
	t.Cleanup(func() {
		if set {
			os.Setenv("HOME", orig)
		} else {
			os.Unsetenv("HOME")
		}
	})
}

func TestLoadMissingFile(t *testing.T) {
	withHome(t, t.TempDir())
	cfg := Load()
	if cfg.DataDir == "" {
		t.Error("Load() with missing config file must return non-empty DataDir")
	}
}

func TestLoadValidFile(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "watui")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte(`data_dir = "/custom/watui/data"`+"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	withHome(t, home)
	cfg := Load()
	if cfg.DataDir != "/custom/watui/data" {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, "/custom/watui/data")
	}
}

func TestLoadInvalidToml(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "watui")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("not valid [[[toml"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	withHome(t, home)
	cfg := Load()
	// Invalid TOML must fall back to defaults, not return a zero-value struct.
	if cfg.DataDir == "" {
		t.Error("Load() with invalid TOML must fall back to defaults (non-empty DataDir)")
	}
}

func TestLoadEmptyDataDir(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "watui")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// TOML with no data_dir key — DataDir should stay at default.
	if err := os.WriteFile(
		filepath.Join(cfgDir, "config.toml"),
		[]byte("[ui]\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	withHome(t, home)
	cfg := Load()
	if cfg.DataDir == "" {
		t.Error("Load() with TOML that omits data_dir must preserve the default DataDir")
	}
}
