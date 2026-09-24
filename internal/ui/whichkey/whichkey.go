// Package whichkey renders the leader-key popup listing the keys available
// after the current prefix.
package whichkey

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/theme"
	"github.com/watui/watui/internal/ui/commands"
)

var (
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorFocused).
			Background(theme.ColorBgPanel).
			Padding(0, 1)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary)
	keyStyle   = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText)
	groupStyle = lipgloss.NewStyle().Foreground(theme.ColorPrimary)
	descStyle  = lipgloss.NewStyle().Foreground(theme.ColorTextDim)
)

// View renders title and opts in a box no wider than maxW columns.
func View(title string, opts []commands.Option, maxW int) string {
	inner := max(4, maxW-boxStyle.GetHorizontalFrameSize())
	rows := []string{titleStyle.Render(ansi.Truncate(title, inner, "…"))}
	for _, o := range opts {
		label := o.Label
		style := descStyle
		if o.IsGroup {
			label, style = "+"+label, groupStyle
		}
		row := keyStyle.Render(o.Key) + "  " + style.Render(label)
		rows = append(rows, ansi.Truncate(row, inner, "…"))
	}
	return boxStyle.Render(strings.Join(rows, "\n"))
}
