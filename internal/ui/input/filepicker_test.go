package input

import "testing"

func TestParsePickerOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "/home/ti/file.png", "/home/ti/file.png"},
		{"trailing newline", "/home/ti/file.png\n", "/home/ti/file.png"},
		{"crlf", "/home/ti/file.png\r\n", "/home/ti/file.png"},
		{"path with spaces", "/home/ti/My File.png\n", "/home/ti/My File.png"},
		{"multi-select pipe", "/a.png|/b.png\n", "/a.png"},
		{"multi-select newline", "/a.png\n/b.png\n", "/a.png"},
		{"empty", "\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePickerOutput(tt.in); got != tt.want {
				t.Errorf("parsePickerOutput(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFindFilePickerNoDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if p := findFilePicker(); p != nil {
		t.Errorf("expected no picker without a display, got %q", p.name)
	}
}

func TestPickerArgsAudioFilter(t *testing.T) {
	for _, p := range filePickers {
		audioArgs := p.args(true)
		docArgs := p.args(false)
		if len(audioArgs) <= len(docArgs) && p.name != "kdialog" {
			t.Errorf("%s: expected audio args to add a filter (audio=%v doc=%v)", p.name, audioArgs, docArgs)
		}
	}
}
