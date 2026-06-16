package input

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// PickerErrorMsg is emitted when a GUI file picker cannot be launched (e.g. none
// is installed). The app layer surfaces Err in the status bar.
type PickerErrorMsg struct{ Err string }

const audioFilter = "Audio | *.ogg *.opus *.mp3 *.m4a *.wav *.aac *.flac"

// filePicker describes a GUI file-chooser dialog that prints the selected path
// to stdout (file managers like nautilus/thunar are browsers, not pickers, so
// they can't be used here).
type filePicker struct {
	name string
	args func(audio bool) []string
}

// filePickers lists supported dialogs in priority order.
var filePickers = []filePicker{
	{
		name: "zenity",
		args: func(audio bool) []string {
			a := []string{"--file-selection", "--title=Select a file to send"}
			if audio {
				a = append(a, "--file-filter="+audioFilter)
			}
			return a
		},
	},
	{
		name: "qarma", // Qt drop-in replacement for zenity
		args: func(audio bool) []string {
			a := []string{"--file-selection", "--title=Select a file to send"}
			if audio {
				a = append(a, "--file-filter="+audioFilter)
			}
			return a
		},
	},
	{
		name: "kdialog",
		args: func(audio bool) []string {
			if audio {
				return []string{"--getopenfilename", startDir(), "audio/*"}
			}
			return []string{"--getopenfilename", startDir()}
		},
	},
	{
		name: "yad",
		args: func(audio bool) []string {
			a := []string{"--file", "--title=Select a file to send"}
			if audio {
				a = append(a, "--file-filter="+audioFilter)
			}
			return a
		},
	},
}

func startDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}

func hasDisplay() bool {
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// findFilePicker returns the first available dialog, honoring the
// WATUI_FILE_PICKER override, or nil when none is usable.
func findFilePicker() *filePicker {
	if !hasDisplay() {
		return nil
	}
	if override := os.Getenv("WATUI_FILE_PICKER"); override != "" {
		if _, err := exec.LookPath(override); err == nil {
			for i := range filePickers {
				if filePickers[i].name == override {
					return &filePickers[i]
				}
			}
			// Unknown picker name but it exists: treat it like zenity.
			return &filePicker{name: override, args: filePickers[0].args}
		}
	}
	for i := range filePickers {
		if _, err := exec.LookPath(filePickers[i].name); err == nil {
			return &filePickers[i]
		}
	}
	return nil
}

func filePickerAvailable() bool { return findFilePicker() != nil }

// parsePickerOutput extracts a single path from dialog stdout, tolerating
// trailing newlines and multi-selection separators.
func parsePickerOutput(out string) string {
	out = strings.TrimRight(out, "\r\n")
	if i := strings.IndexAny(out, "|\n"); i >= 0 {
		out = out[:i]
	}
	return strings.TrimSpace(out)
}

// pickFileCmd launches the GUI picker and emits SendFileMsg / SendAudioMsg with
// the chosen path. It returns nil when the user cancels the dialog.
func pickFileCmd(audio bool) tea.Cmd {
	picker := findFilePicker()
	if picker == nil {
		return func() tea.Msg {
			return PickerErrorMsg{Err: "no GUI file picker found (install zenity, kdialog, qarma, or yad)"}
		}
	}
	return func() tea.Msg {
		out, err := exec.Command(picker.name, picker.args(audio)...).Output()
		if err != nil {
			// A non-zero exit almost always means the user cancelled.
			return nil
		}
		path := parsePickerOutput(string(out))
		if path == "" {
			return nil
		}
		if audio {
			return SendAudioMsg{Path: path}
		}
		return SendFileMsg{Path: path}
	}
}
