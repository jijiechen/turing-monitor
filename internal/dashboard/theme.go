// Package dashboard renders system metrics onto the panel.
//
// Frames are pushed as JPEG stills over the protocol's image command, which
// measures at roughly 8 fps on a 5.2" panel — far more than a dashboard needs.
// Using stills rather than H.264 keeps this path free of any encoder
// dependency on either platform.
package dashboard

import (
	"image/color"
)

// Theme is the dashboard's colour scheme.
type Theme struct {
	Background color.RGBA
	Panel      color.RGBA
	Border     color.RGBA
	Text       color.RGBA
	Dim        color.RGBA
	Accent     color.RGBA
	Warn       color.RGBA
	Crit       color.RGBA
}

// DefaultTheme is a dark scheme with a teal accent, chosen to stay legible on
// the panel's small, dense display.
var DefaultTheme = Theme{
	Background: color.RGBA{R: 0x0B, G: 0x0F, B: 0x18, A: 0xFF},
	Panel:      color.RGBA{R: 0x14, G: 0x1A, B: 0x27, A: 0xFF},
	Border:     color.RGBA{R: 0x24, G: 0x2E, B: 0x42, A: 0xFF},
	Text:       color.RGBA{R: 0xEC, G: 0xF1, B: 0xF8, A: 0xFF},
	Dim:        color.RGBA{R: 0x8B, G: 0x99, B: 0xB0, A: 0xFF},
	Accent:     color.RGBA{R: 0x2D, G: 0xD4, B: 0xBF, A: 0xFF},
	Warn:       color.RGBA{R: 0xF5, G: 0xC2, B: 0x42, A: 0xFF},
	Crit:       color.RGBA{R: 0xEF, G: 0x53, B: 0x53, A: 0xFF},
}

// severity picks a colour for a 0-100 percentage: accent normally, warn past
// 75, crit past 90. Thresholds are deliberately generous because a dashboard
// that cries wolf gets ignored.
func (t Theme) severity(pct float64) color.RGBA {
	switch {
	case pct >= 90:
		return t.Crit
	case pct >= 75:
		return t.Warn
	default:
		return t.Accent
	}
}
