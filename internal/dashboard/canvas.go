package dashboard

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// canvas draws onto an RGBA image with a small set of primitives. Fonts are the
// Go fonts shipped with x/image, so the binary carries no external assets.
type canvas struct {
	img   *image.RGBA
	theme Theme

	faces map[faceKey]font.Face
}

type faceKey struct {
	mono bool
	size float64
}

func newCanvas(img *image.RGBA, theme Theme) *canvas {
	return &canvas{img: img, theme: theme, faces: map[faceKey]font.Face{}}
}

// font returns a cached font face. Fonts are expensive to parse, and the
// dashboard repaints several times a second.
func (c *canvas) font(size float64, mono bool) font.Face {
	key := faceKey{mono: mono, size: size}
	if f, ok := c.faces[key]; ok {
		return f
	}

	src := goregular.TTF
	if mono {
		src = gomono.TTF
	}
	f, err := opentype.Parse(src)
	if err != nil {
		// The embedded fonts are known-good; a parse failure is a programming
		// error, and every caller would have to handle it otherwise.
		panic(fmt.Sprintf("dashboard: parse embedded font: %v", err))
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(fmt.Sprintf("dashboard: build font face: %v", err))
	}
	c.faces[key] = face
	return face
}

// text draws s with its baseline-left corner at (x, y).
func (c *canvas) text(x, y int, s string, size float64, col color.Color, mono bool) {
	d := &font.Drawer{
		Dst:  c.img,
		Src:  image.NewUniform(col),
		Face: c.font(size, mono),
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}

// textWidth measures s in device pixels.
func (c *canvas) textWidth(s string, size float64, mono bool) int {
	return font.MeasureString(c.font(size, mono), s).Round()
}

// textRight draws s with its baseline-right corner at (x, y).
func (c *canvas) textRight(x, y int, s string, size float64, col color.Color, mono bool) {
	c.text(x-c.textWidth(s, size, mono), y, s, size, col, mono)
}

// textCentre draws s horizontally centred on x.
func (c *canvas) textCentre(x, y int, s string, size float64, col color.Color, mono bool) {
	c.text(x-c.textWidth(s, size, mono)/2, y, s, size, col, mono)
}

func (c *canvas) fill(r image.Rectangle, col color.Color) {
	draw.Draw(c.img, r, image.NewUniform(col), image.Point{}, draw.Src)
}

// panel draws a card with a title in its top-left corner.
func (c *canvas) panel(r image.Rectangle, title string) {
	c.fill(r, c.theme.Panel)
	c.stroke(r, c.theme.Border)
	if title != "" {
		c.text(r.Min.X+20, r.Min.Y+38, title, 22, c.theme.Dim, false)
	}
}

// stroke draws a one-pixel border inside r.
func (c *canvas) stroke(r image.Rectangle, col color.Color) {
	c.fill(image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), col)
	c.fill(image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), col)
	c.fill(image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y), col)
	c.fill(image.Rect(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y), col)
}

// bar draws a horizontal usage bar with a rounded fill proportion.
func (c *canvas) bar(r image.Rectangle, fraction float64, col color.Color) {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	c.fill(r, c.theme.Background)
	filled := int(float64(r.Dx()) * fraction)
	if filled > 0 {
		c.fill(image.Rect(r.Min.X, r.Min.Y, r.Min.X+filled, r.Max.Y), col)
	}
	c.stroke(r, c.theme.Border)
}

// humanBytes formats a byte count with a binary unit.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	units := "KMGT"
	if exp > len(units)-1 {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), units[exp])
}

// humanRate formats a bytes-per-second rate using decimal units, which is how
// network speeds are conventionally quoted.
func humanRate(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= 1e6:
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/1e6)
	case bytesPerSec >= 1e3:
		return fmt.Sprintf("%.0f KB/s", bytesPerSec/1e3)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

// humanDuration formats a duration compactly, e.g. "3d 4h" or "5h 12m".
func humanDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
