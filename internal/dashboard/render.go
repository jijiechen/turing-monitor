package dashboard

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/jijiechen/turing-monitor/internal/metrics"
)

// margin is the gap between the panel edge and the outermost card.
const margin = 20

// Render draws one dashboard frame at the given size.
//
// rates must come from metrics.Delta of this snapshot against the previous
// one; the first frame after startup has no previous sample and so shows zero
// CPU and network activity until the second frame.
func Render(s metrics.Snapshot, rates metrics.Rates, w, h int, theme Theme) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c := newCanvas(img, theme)
	c.fill(img.Bounds(), theme.Background)

	inner := w - 2*margin
	y := margin

	y = drawHeader(c, s, margin, y, inner)
	y = drawCPU(c, s, rates, margin, y, inner)
	y = drawMemory(c, s, margin, y, inner)
	y = drawNetwork(c, rates, margin, y, inner)

	// The remaining space is shared between disk and temperature. If there is
	// no temperature data (macOS), disk takes the whole area.
	remaining := h - margin - y
	if len(s.Temps) == 0 {
		drawDisk(c, s, margin, y, inner, remaining)
	} else {
		half := (remaining - 16) / 2
		y = drawDisk(c, s, margin, y, inner, half)
		drawTemps(c, s, margin, y+16, inner, remaining-half-16)
	}
	return img
}

// drawHeader renders the hostname and uptime, and returns the next free y.
func drawHeader(c *canvas, s metrics.Snapshot, x, y, w int) int {
	const h = 96
	c.text(x+4, y+52, s.Hostname, 40, c.theme.Text, false)
	c.textRight(x+w-4, y+50, humanDuration(s.Uptime), 26, c.theme.Dim, true)
	c.textRight(x+w-4, y+80, "uptime", 18, c.theme.Dim, false)
	return y + h
}

// drawCPU renders the CPU utilisation card: a large percentage, a bar, and the
// load averages.
func drawCPU(c *canvas, s metrics.Snapshot, rates metrics.Rates, x, y, w int) int {
	const h = 300
	r := image.Rect(x, y, x+w, y+h)
	c.panel(r, "CPU")

	col := c.theme.severity(rates.CPUPercent)
	pct := fmt.Sprintf("%.0f", rates.CPUPercent)

	// A large number reads at a glance from across a desk, which is the whole
	// point of a side panel.
	c.text(x+36, y+190, pct, 130, col, true)
	c.text(x+36+c.textWidth(pct, 130, true)+8, y+190, "%", 44, col, false)

	c.bar(image.Rect(x+36, y+220, x+w-36, y+248), rates.CPUPercent/100, col)

	load := fmt.Sprintf("%.2f   %.2f   %.2f", s.Load[0], s.Load[1], s.Load[2])
	c.text(x+36, y+284, "LOAD", 20, c.theme.Dim, false)
	c.textRight(x+w-36, y+284, load, 22, c.theme.Text, true)
	return y + h + 16
}

// drawMemory renders the memory card, including swap when it is configured.
func drawMemory(c *canvas, s metrics.Snapshot, x, y, w int) int {
	mem := s.Mem
	swap := s.Swap

	// Swap is common on macOS but often absent on Linux; only make room for it
	// when the system actually has some.
	h := 210
	if swap.Total > 0 {
		h = 300
	}
	r := image.Rect(x, y, x+w, y+h)
	c.panel(r, "MEMORY")

	col := c.theme.severity(mem.UsedPercent())
	c.textRight(x+w-24, y+38, fmt.Sprintf("%s / %s", humanBytes(mem.Used), humanBytes(mem.Total)), 22, c.theme.Text, true)

	c.text(x+36, y+120, fmt.Sprintf("%.0f%%", mem.UsedPercent()), 64, col, true)
	c.bar(image.Rect(x+36, y+140, x+w-36, y+172), mem.UsedPercent()/100, col)

	if swap.Total > 0 {
		c.text(x+36, y+226, "SWAP", 20, c.theme.Dim, false)
		c.textRight(x+w-36, y+226, fmt.Sprintf("%s / %s", humanBytes(swap.Used), humanBytes(swap.Total)), 22, c.theme.Text, true)
		c.bar(image.Rect(x+36, y+240, x+w-36, y+264), swap.UsedPercent()/100, c.theme.severity(swap.UsedPercent()))
	}
	return y + h + 16
}

// drawNetwork renders receive and transmit throughput.
func drawNetwork(c *canvas, rates metrics.Rates, x, y, w int) int {
	const h = 180
	r := image.Rect(x, y, x+w, y+h)
	c.panel(r, "NETWORK")

	c.text(x+36, y+92, "DOWN", 20, c.theme.Dim, false)
	c.textRight(x+w-36, y+96, humanRate(rates.RxPerSec), 34, c.theme.Accent, true)

	c.text(x+36, y+150, "UP", 20, c.theme.Dim, false)
	c.textRight(x+w-36, y+154, humanRate(rates.TxPerSec), 34, c.theme.Warn, true)
	return y + h + 16
}

// drawDisk renders usage of the root filesystem.
func drawDisk(c *canvas, s metrics.Snapshot, x, y, w, h int) int {
	r := image.Rect(x, y, x+w, y+h)
	c.panel(r, "DISK")

	if len(s.Mounts) == 0 {
		c.textCentre(x+w/2, y+h/2+10, "not available", 24, c.theme.Dim, false)
		return y + h
	}
	m := s.Mounts[0]
	col := c.theme.severity(m.UsedPercent())

	// When there are no temperature sensors this card is the last one and
	// stretches to fill the leftover space, so centre its content rather than
	// leaving a block of dead space at the bottom.
	const contentHeight = 96
	top := y + 52
	if spare := h - contentHeight - 52; spare > 0 {
		top += spare / 2
	}

	c.text(x+36, top+50, fmt.Sprintf("%.0f%%", m.UsedPercent()), 56, col, true)
	c.textRight(x+w-36, top+46, fmt.Sprintf("%s / %s", humanBytes(m.Used), humanBytes(m.Total)), 22, c.theme.Text, true)
	c.bar(image.Rect(x+36, top+70, x+w-36, top+100), m.UsedPercent()/100, col)
	return y + h
}

// drawTemps renders temperature sensors. It is only reached on systems that
// report them (Linux); macOS exposes no usable interface for an unprivileged
// tool.
func drawTemps(c *canvas, s metrics.Snapshot, x, y, w, h int) int {
	r := image.Rect(x, y, x+w, y+h)
	c.panel(r, "TEMPERATURE")

	// Show the hottest few sensors; a long list would not fit or be readable.
	sensors := s.Temps
	const maxShown = 4
	if len(sensors) > maxShown {
		sensors = sensors[:maxShown]
	}

	lineY := y + 78
	for _, sensor := range sensors {
		col := c.theme.Accent
		switch {
		case sensor.Celsius >= 90:
			col = c.theme.Crit
		case sensor.Celsius >= 75:
			col = c.theme.Warn
		}
		name := sensor.Name
		if len(name) > 22 {
			name = name[:22]
		}
		c.text(x+36, lineY, name, 20, c.theme.Dim, false)
		c.textRight(x+w-36, lineY, fmt.Sprintf("%.0f°C", sensor.Celsius), 24, col, true)
		lineY += 34
	}
	return y + h
}

var _ = draw.Src
