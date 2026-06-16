package auth

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skip2/go-qrcode"
)

const (
	layoutOverheadLines = 6 // title, subtitle, gaps, status
	layoutSideMargin    = 4
)

type Model struct {
	qrRaw   string
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

func (m *Model) SetDimensions(width, height int) {
	if width > 0 {
		m.width = width
	}
	if height > 0 {
		m.height = height
	}
}

func (m *Model) SetQRCode(code string) {
	m.qrRaw = code
	m.status = "Scan with WhatsApp"
}

func (m *Model) SetQRTimeout() {
	m.qrRaw = ""
	m.status = "QR code expired, waiting for new one..."
}

func (m *Model) SetStatus(status string) {
	m.status = status
}

func (m Model) View() string {
	var content strings.Builder

	content.WriteString(qrTitleStyle.Render("watui"))
	content.WriteString("\n")
	content.WriteString(qrSubtitleStyle.Render("WhatsApp TUI Client"))
	content.WriteString("\n\n")

	if m.qrRaw != "" {
		maxW, maxH := m.qrBounds()
		content.WriteString(renderQR(m.qrRaw, maxW, maxH))
		content.WriteString("\n\n")
	} else {
		content.WriteString(m.spinner.View())
		content.WriteString(" ")
	}

	content.WriteString(qrSubtitleStyle.Render(m.status))

	body := content.String()
	if m.width <= 0 || m.height <= 0 {
		return body
	}

	// Top-align when space is tight so the QR is never clipped at the top.
	bodyLines := strings.Count(body, "\n") + 1
	if bodyLines >= m.height {
		return lipgloss.Place(
			m.width, m.height,
			lipgloss.Center, lipgloss.Top,
			body,
		)
	}

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		body,
	)
}

func (m Model) qrBounds() (maxWidth, maxHeight int) {
	width := m.width
	height := m.height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	maxWidth = width - layoutSideMargin
	if maxWidth < 20 {
		maxWidth = 20
	}

	maxHeight = height - layoutOverheadLines
	if maxHeight < 8 {
		maxHeight = 8
	}
	return maxWidth, maxHeight
}

// renderQR renders a QR matrix using Unicode block glyphs. A QR matrix cannot be
// downscaled below one module per cell without corrupting its data, so the only
// way to fit a large code into a short terminal is to pack more modules per text
// line. We pick the densest renderer that fits:
//
//   - half-blocks  (1x2 modules/cell) — widest font support, used when it fits.
//   - sextants     (2x3 modules/cell) — ~3x denser vertically; needs a font with
//     Unicode 13 "Symbols for Legacy Computing" glyphs.
//
// If neither fits we ask the user to enlarge the window rather than render a
// broken, unscannable code.
func renderQR(content string, maxWidth, maxHeight int) string {
	qr, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "Error generating QR code"
	}
	// Keep the quiet-zone border: scanners (WhatsApp included) need it to lock on.

	bitmap := qr.Bitmap()
	if len(bitmap) == 0 || len(bitmap[0]) == 0 {
		return "Error generating QR code"
	}

	rows := len(bitmap)
	cols := len(bitmap[0])

	if cols <= maxWidth && ceilDiv(rows, 2) <= maxHeight {
		return renderHalfBlock(bitmap)
	}
	if ceilDiv(cols, 2) <= maxWidth && ceilDiv(rows, 3) <= maxHeight {
		return renderSextant(bitmap)
	}
	return qrSubtitleStyle.Render("Terminal too small — enlarge the window to scan")
}

func ceilDiv(a, b int) int {
	return (a + b - 1) / b
}

func renderHalfBlock(bitmap [][]bool) string {
	if len(bitmap) == 0 {
		return ""
	}

	rows := len(bitmap)
	var buf strings.Builder

	blackOnBlack := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#000000"))
	blackOnWhite := lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#FFFFFF"))
	whiteOnBlack := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#000000"))
	whiteOnWhite := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#FFFFFF"))

	for y := 0; y < rows; y += 2 {
		for x := 0; x < len(bitmap[y]); x++ {
			top := bitmap[y][x]
			bottom := false
			if y+1 < rows {
				bottom = bitmap[y+1][x]
			}

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

// sextantStyle paints "on" sub-cells dark and the rest light; the chosen glyph
// encodes which of the 2x3 sub-cells are dark.
var sextantStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#000000")).
	Background(lipgloss.Color("#FFFFFF"))

// renderSextant packs a 2-wide by 3-tall block of modules into a single cell using
// Unicode block-sextant glyphs, fitting ~3 module rows per text line.
func renderSextant(bitmap [][]bool) string {
	rows := len(bitmap)
	if rows == 0 {
		return ""
	}
	cols := len(bitmap[0])
	var buf strings.Builder

	for y := 0; y < rows; y += 3 {
		var line strings.Builder
		for x := 0; x < cols; x += 2 {
			v := 0
			if moduleDark(bitmap, x, y) {
				v |= 1
			}
			if moduleDark(bitmap, x+1, y) {
				v |= 2
			}
			if moduleDark(bitmap, x, y+1) {
				v |= 4
			}
			if moduleDark(bitmap, x+1, y+1) {
				v |= 8
			}
			if moduleDark(bitmap, x, y+2) {
				v |= 16
			}
			if moduleDark(bitmap, x+1, y+2) {
				v |= 32
			}
			line.WriteRune(sextantRunes[v])
		}
		buf.WriteString(sextantStyle.Render(line.String()))
		if y+3 < rows {
			buf.WriteString("\n")
		}
	}

	return buf.String()
}

// moduleDark reports whether a module is dark, treating out-of-range cells as
// light so the quiet zone simply extends into any trailing padding.
func moduleDark(bitmap [][]bool, x, y int) bool {
	if y < 0 || y >= len(bitmap) || x < 0 || x >= len(bitmap[y]) {
		return false
	}
	return bitmap[y][x]
}

// sextantRunes maps a 6-bit sub-cell mask to its glyph. Bit order is row-major:
// bit0 top-left, bit1 top-right, bit2 mid-left, bit3 mid-right, bit4 bottom-left,
// bit5 bottom-right. Unicode assigns U+1FB00.. in this same value order, skipping
// the four masks that already have dedicated glyphs (empty, left/right half, full).
var sextantRunes = buildSextantRunes()

func buildSextantRunes() [64]rune {
	var t [64]rune
	next := rune(0x1FB00)
	for v := 0; v < 64; v++ {
		switch v {
		case 0:
			t[v] = ' '
		case 21:
			t[v] = '▌' // U+258C LEFT HALF BLOCK (cols 1,3,5)
		case 42:
			t[v] = '▐' // U+2590 RIGHT HALF BLOCK (cols 2,4,6)
		case 63:
			t[v] = '█' // U+2588 FULL BLOCK
		default:
			t[v] = next
			next++
		}
	}
	return t
}