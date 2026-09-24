package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/watui/watui/internal/theme"
	"github.com/watui/watui/internal/ui/commands"
)

// overlay is a modal drawn over the body; while open it receives keys first.
type overlay interface {
	Update(tea.KeyMsg) (overlay, tea.Cmd) // nil closes the overlay
	View(maxW, maxH int) string
	Centered() bool // false: bottom-right corner (which-key)
}

func (m *Model) openOverlay(o overlay) { m.overlay = o; m.leader = nil }

// placeOverlay draws box over body (bodyW×bodyH), bottom-right or centred,
// without changing body's size.
func placeOverlay(body, box string, bodyW, bodyH int, centered bool) string {
	lines := strings.Split(body, "\n")
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	boxLines := strings.Split(box, "\n")
	if len(boxLines) > bodyH {
		boxLines = boxLines[:bodyH]
	}
	boxW := 0
	for _, l := range boxLines {
		boxW = max(boxW, lipgloss.Width(l))
	}
	boxW = min(boxW, bodyW)
	top, left := bodyH-len(boxLines), bodyW-boxW
	if centered {
		top, left = (bodyH-len(boxLines))/2, (bodyW-boxW)/2
	}
	for i, bl := range boxLines {
		row := top + i
		line := lines[row]
		// Pad against the truncated prefix, not the whole line: a wide rune
		// straddling the cut leaves the prefix one column short.
		head := ansi.Truncate(line, left, "")
		pad := max(0, left-lipgloss.Width(head))
		lines[row] = head + strings.Repeat(" ", pad) +
			ansi.Truncate(bl, boxW, "") + ansi.TruncateLeft(line, left+boxW, "")
	}
	return strings.Join(lines[:bodyH], "\n")
}

// helpOverlay lists every leader command and the direct keys.
type helpOverlay struct{ reg *commands.Registry }

var directKeys = [][2]string{
	{"Tab / Shift+Tab", "cycle panels"}, {"j / k", "move"}, {"g / G", "top / bottom"},
	{"Enter", "open chat · send · open media"}, {"i", "focus input"}, {"Esc", "back / cancel"},
	{"ctrl+f / ctrl+p / ctrl+o", "attach · audio · picker (in input)"}, {"ctrl+c", "quit"},
}

func (h helpOverlay) Update(k tea.KeyMsg) (overlay, tea.Cmd) {
	if k.String() == "esc" || k.String() == "q" || k.String() == "?" {
		return nil, nil
	}
	return h, nil
}

func (h helpOverlay) Centered() bool { return true }

func (h helpOverlay) View(maxW, maxH int) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorPrimary)
	key := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText)
	dim := lipgloss.NewStyle().Foreground(theme.ColorTextDim)
	rows := []string{title.Render("Leader (Space)")}
	for _, c := range h.reg.Commands() {
		rows = append(rows, key.Render("␣ "+c.Keys)+"  "+dim.Render(c.Desc))
	}
	rows = append(rows, "", title.Render("Direct keys"))
	for _, d := range directKeys {
		rows = append(rows, key.Render(d[0])+"  "+dim.Render(d[1]))
	}
	rows = append(rows, "", dim.Render("esc to close"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(theme.ColorFocused).
		Background(theme.ColorBgPanel).Padding(0, 1)
	inner := max(4, maxW-box.GetHorizontalFrameSize())
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, inner, "…")
	}
	if maxH > 2 && len(rows) > maxH-2 {
		rows = rows[:maxH-2]
	}
	return box.Render(strings.Join(rows, "\n"))
}
