package auth

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skip2/go-qrcode"
)

type Model struct {
	qrCode  string
	spinner spinner.Model
	width   int
	height  int
	status  string
}

var (
	colorPrimary = lipgloss.Color("#25D366")
	colorTextDim = lipgloss.Color("#8696A0")

	qrTitleStyle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Align(lipgloss.Center)

	qrSubtitleStyle = lipgloss.NewStyle().
			Foreground(colorTextDim).
			Align(lipgloss.Center)
)

func New() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorPrimary)

	return Model{
		spinner: s,
		status:  "Waiting for QR code...",
	}
}

func (m Model) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) SetQRCode(code string) {
	m.qrCode = renderQR(code)
	m.status = "Scan with WhatsApp"
}

func (m *Model) SetQRTimeout() {
	m.qrCode = ""
	m.status = "QR code expired, waiting for new one..."
}

func (m *Model) SetStatus(status string) {
	m.status = status
}

func (m Model) View() string {
	var content strings.Builder

	title := qrTitleStyle.Render("watui")
	subtitle := qrSubtitleStyle.Render("WhatsApp TUI Client")

	content.WriteString(title)
	content.WriteString("\n\n")
	content.WriteString(subtitle)
	content.WriteString("\n\n")

	if m.qrCode != "" {
		content.WriteString(m.qrCode)
		content.WriteString("\n\n")
	} else {
		content.WriteString(m.spinner.View())
		content.WriteString(" ")
	}

	content.WriteString(qrSubtitleStyle.Render(m.status))

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content.String(),
	)
}

// renderQR converts a QR code string to half-block Unicode characters for terminal display.
func renderQR(content string) string {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "Error generating QR code"
	}

	bitmap := qr.Bitmap()
	rows := len(bitmap)

	var buf strings.Builder

	// Styles with both foreground AND background set explicitly.
	// The '▀' char paints the top half with foreground and bottom half with background.
	blackOnBlack := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#000000"))
	blackOnWhite := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFFFFF"))
	whiteOnBlack := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000"))
	whiteOnWhite := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#FFFFFF"))

	// Process two rows at a time using Unicode half-block characters
	for y := 0; y < rows; y += 2 {
		for x := 0; x < len(bitmap[y]); x++ {
			top := bitmap[y][x]
			bottom := false
			if y+1 < rows {
				bottom = bitmap[y+1][x]
			}

			// Using '▀' (upper half block) with foreground=top color, background=bottom color
			switch {
			case top && bottom:
				buf.WriteString(blackOnBlack.Render("▀"))
			case top && !bottom:
				buf.WriteString(blackOnWhite.Render("▀"))
			case !top && bottom:
				buf.WriteString(whiteOnBlack.Render("▀"))
			default:
				buf.WriteString(whiteOnWhite.Render("▀"))
			}
		}
		if y+2 < rows {
			buf.WriteString("\n")
		}
	}

	return buf.String()
}
