package chatview

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	_ "golang.org/x/image/webp"
)

// cachedThumbnail returns the ANSI half-block thumbnail for the given raw image
// bytes, consulting and populating thumbCache to avoid re-rendering on every
// rebuildContent call.
func cachedThumbnail(msgID string, data []byte, mimeType string, cols int, thumbCache map[string]string) string {
	cacheKey := fmt.Sprintf("%s:%d", msgID, cols)
	if v, ok := thumbCache[cacheKey]; ok {
		return v
	}
	result := thumbnailBytesToHalfBlock(data, cols)
	thumbCache[cacheKey] = result
	return result
}

// cachedThumbnailFromPath reads the file at path and delegates to cachedThumbnail.
func cachedThumbnailFromPath(msgID, path, mimeType string, cols int, thumbCache map[string]string) string {
	cacheKey := fmt.Sprintf("%s:%d", msgID, cols)
	if v, ok := thumbCache[cacheKey]; ok {
		return v
	}
	data, err := os.ReadFile(path)
	if err != nil {
		thumbCache[cacheKey] = ""
		return ""
	}
	result := thumbnailBytesToHalfBlock(data, cols)
	thumbCache[cacheKey] = result
	return result
}

// thumbnailBytesToHalfBlock decodes image bytes and renders a half-block ANSI
// thumbnail sized to cols terminal columns. Each terminal row encodes 2 pixel
// rows (top pixel → foreground, bottom pixel → background of '▀').
//
// On decode error (e.g. animated/extended WebP that golang.org/x/image/webp
// cannot handle) an empty string is returned so the caller can fall back to a
// text placeholder.
func thumbnailBytesToHalfBlock(data []byte, cols int) string {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	return imageToHalfBlock(src, cols)
}

// imageToHalfBlock scales src to cols×rows (rows determined by aspect ratio,
// capped at 16) and renders each pair of pixel rows as a single terminal row
// of '▀' half-block characters with 24-bit ANSI fg/bg colors.
func imageToHalfBlock(src image.Image, cols int) string {
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	// Target dimensions: cols wide, aspect-correct height (2px per row), capped at 16 rows.
	rows := cols * srcH / srcW / 2
	if rows < 1 {
		rows = 1
	}
	if rows > 16 {
		rows = 16
	}
	pixH := rows * 2 // pixel height (2 pixels per terminal row)
	pixW := cols

	// Nearest-neighbour resize into an RGBA image.
	scaled := image.NewRGBA(image.Rect(0, 0, pixW, pixH))
	draw.Draw(scaled, scaled.Bounds(), &nnScaler{src: src, srcW: srcW, srcH: srcH, dstW: pixW, dstH: pixH}, image.Point{}, draw.Src)

	var b strings.Builder
	for row := 0; row < rows; row++ {
		for col := 0; col < pixW; col++ {
			top := scaled.RGBAAt(col, row*2)
			var bot color.RGBA
			if row*2+1 < pixH {
				bot = scaled.RGBAAt(col, row*2+1)
			}
			// Emit combined fg+bg 24-bit ANSI: fg = top pixel, bg = bottom pixel.
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀",
				top.R, top.G, top.B,
				bot.R, bot.G, bot.B,
			)
		}
		b.WriteString("\x1b[0m") // reset at end of each row
		if row < rows-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// nnScaler implements image.Image for nearest-neighbour scaling via draw.Draw.
type nnScaler struct {
	src        image.Image
	srcW, srcH int
	dstW, dstH int
}

func (s *nnScaler) ColorModel() color.Model { return s.src.ColorModel() }
func (s *nnScaler) Bounds() image.Rectangle { return image.Rect(0, 0, s.dstW, s.dstH) }
func (s *nnScaler) At(x, y int) color.Color {
	sx := x * s.srcW / s.dstW
	sy := y * s.srcH / s.dstH
	b := s.src.Bounds()
	return s.src.At(b.Min.X+sx, b.Min.Y+sy)
}
