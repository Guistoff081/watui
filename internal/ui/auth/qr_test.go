package auth

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/skip2/go-qrcode"
)

// samplePairingContent mimics a real WhatsApp pairing payload length.
const samplePairingContent = "2@fk+v7hg1aJpI3Ae6eJJPu9oT6PiNWZ/pwZswDHp3ZSTU5l7FzT4z7CJWB5AYUGuq0Zvg7W6rBtoAuedzu6Nv7kJKnE0xURyi6es=,NAKMpWjLCojin/hFB62+rjMyR539e/OMS3/1fXZumy8=,GVQ9g3EL7raWDssihjDNqtbOejPqfu+6FXUxFBN1sW8=,VF4W22PnIrvibM6yKDnPNccAllsab2GKfBB4HxG5/sw="

// TestRenderQRMatchesNativeResolution ensures the QR is rendered 1:1 (no
// downsampling), since resampling a QR matrix destroys its scannability.
func TestRenderQRMatchesNativeResolution(t *testing.T) {
	qr, err := qrcode.New(samplePairingContent, qrcode.Low)
	if err != nil {
		t.Fatalf("qrcode.New() error = %v", err)
	}
	bitmap := qr.Bitmap()
	wantCols := len(bitmap[0])
	wantLines := (len(bitmap) + 1) / 2

	rendered := renderQR(samplePairingContent, wantCols, wantLines)
	if !strings.Contains(rendered, "▀") {
		t.Fatalf("expected a rendered QR, got: %q", rendered)
	}

	lines := strings.Split(rendered, "\n")
	if len(lines) != wantLines {
		t.Errorf("rendered height = %d lines, want %d (native, no downsampling)", len(lines), wantLines)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != wantCols {
			t.Errorf("line %d width = %d, want %d (native, no downsampling)", i, w, wantCols)
		}
	}
}

// TestRenderQRPreservesQuietZoneBorder verifies the border (quiet zone) is kept,
// which scanners require to detect the code.
func TestRenderQRPreservesQuietZoneBorder(t *testing.T) {
	withBorder, err := qrcode.New(samplePairingContent, qrcode.Low)
	if err != nil {
		t.Fatalf("qrcode.New() error = %v", err)
	}
	noBorder, err := qrcode.New(samplePairingContent, qrcode.Low)
	if err != nil {
		t.Fatalf("qrcode.New() error = %v", err)
	}
	noBorder.DisableBorder = true

	if len(withBorder.Bitmap()) <= len(noBorder.Bitmap()) {
		t.Skip("qrcode library produced no border difference")
	}

	cols := len(withBorder.Bitmap()[0])
	lines := (len(withBorder.Bitmap()) + 1) / 2
	rendered := renderQR(samplePairingContent, cols, lines)

	gotLines := strings.Count(rendered, "\n") + 1
	if gotLines != lines {
		t.Errorf("rendered with quiet zone = %d lines, want %d", gotLines, lines)
	}
}

// TestRenderQRTooSmallReturnsMessage ensures we never emit a corrupted/clipped QR
// when the viewport is too small; we show guidance instead.
func TestRenderQRTooSmallReturnsMessage(t *testing.T) {
	rendered := renderQR(samplePairingContent, 10, 5)
	if strings.Contains(rendered, "▀") {
		t.Errorf("expected fallback message for tiny viewport, got a QR render")
	}
	if !strings.Contains(rendered, "Terminal too small") {
		t.Errorf("expected 'Terminal too small' message, got: %q", rendered)
	}
}

// TestRenderQRUsesSextantWhenTooTallForHalfBlock verifies that a wide-but-short
// viewport (where half-blocks won't fit vertically) falls back to sextants and
// fits, rather than giving up with the "too small" message.
func TestRenderQRUsesSextantWhenTooTallForHalfBlock(t *testing.T) {
	qr, err := qrcode.New(samplePairingContent, qrcode.Low)
	if err != nil {
		t.Fatalf("qrcode.New() error = %v", err)
	}
	bitmap := qr.Bitmap()
	rows := len(bitmap)
	cols := len(bitmap[0])

	halfBlockLines := (rows + 1) / 2
	sextantLines := (rows + 2) / 3
	// Pick a height that's too short for half-blocks but fits sextants.
	maxHeight := halfBlockLines - 1
	if sextantLines > maxHeight {
		t.Fatalf("test assumption broken: sextant lines %d > maxHeight %d", sextantLines, maxHeight)
	}

	rendered := renderQR(samplePairingContent, cols, maxHeight)
	if strings.Contains(rendered, "Terminal too small") {
		t.Fatalf("expected sextant render, got too-small message")
	}
	if strings.Contains(rendered, "▀") {
		t.Errorf("expected sextant glyphs, but found half-block output")
	}

	gotLines := strings.Count(rendered, "\n") + 1
	if gotLines != sextantLines {
		t.Errorf("sextant render = %d lines, want %d", gotLines, sextantLines)
	}
	if gotLines > maxHeight {
		t.Errorf("sextant render = %d lines, exceeds maxHeight %d", gotLines, maxHeight)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if w := lipgloss.Width(line); w > (cols+1)/2 {
			t.Errorf("sextant line width = %d, want <= %d", w, (cols+1)/2)
		}
	}
}

// TestSextantRunesBitMapping spot-checks the 6-bit mask -> glyph table against
// known Unicode block-sextant assignments.
func TestSextantRunesBitMapping(t *testing.T) {
	cases := map[int]rune{
		0:  ' ',
		1:  '\U0001FB00', // SEXTANT-1 (top-left only)
		2:  '\U0001FB01', // SEXTANT-2 (top-right only)
		3:  '\U0001FB02', // SEXTANT-12
		4:  '\U0001FB03', // SEXTANT-3 (mid-left only)
		21: '▌',          // left column -> LEFT HALF BLOCK
		42: '▐',          // right column -> RIGHT HALF BLOCK
		63: '█',          // all sub-cells -> FULL BLOCK
	}
	for v, want := range cases {
		if got := sextantRunes[v]; got != want {
			t.Errorf("sextantRunes[%d] = %q (U+%04X), want %q (U+%04X)", v, got, got, want, want)
		}
	}
}

func TestQRBoundsUsesDefaultsWhenUnset(t *testing.T) {
	m := New()
	maxW, maxH := m.qrBounds()
	if maxW != 76 || maxH != 18 {
		t.Errorf("qrBounds() = (%d, %d), want (76, 18)", maxW, maxH)
	}
}
