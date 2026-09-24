package whichkey

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/ui/commands"
)

func TestViewListsOptions(t *testing.T) {
	out := ansi.Strip(View("␣", []commands.Option{
		{Key: "?", Label: "All keybindings"},
		{Key: "a", Label: "attach", IsGroup: true},
	}, 60))
	for _, want := range []string{"␣", "?  All keybindings", "a  +attach"} {
		if !strings.Contains(out, want) {
			t.Errorf("View() missing %q:\n%s", want, out)
		}
	}
}

func TestViewRespectsWidth(t *testing.T) {
	out := View("␣ a — attach", []commands.Option{{Key: "f", Label: strings.Repeat("very long label ", 10)}}, 30)
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 30 {
			t.Errorf("line width %d > 30: %q", w, ansi.Strip(l))
		}
	}
}
