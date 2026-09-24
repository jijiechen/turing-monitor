package dashboard

import (
	"fmt"
	"image"

	"github.com/jijiechen/turing-monitor/internal/metrics"
)

const (
	// margin is the gap between the panel edge and the outermost card.
	margin = 20
	// cardGap is the space between adjacent cards.
	cardGap = 16
	// headerHeight is the space the hostname and uptime occupy.
	headerHeight = 96
)

// Render draws one dashboard frame at the given size.
//
// The size is the one the viewer sees, so a panel mounted on its side is drawn
// wide and rotated afterwards rather than drawn tall and read sideways. A tall
// frame gets a single column of cards; a wide one gets two.
//
// rates must come from metrics.Delta of this snapshot against the previous
// one; the first frame after startup has no previous sample and so shows zero
// CPU and network activity until the second frame.
func Render(s metrics.Snapshot, rates metrics.Rates, w, h int, theme Theme) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	c := newCanvas(img, theme)
	c.fill(img.Bounds(), theme.Background)

	if w > h {
		renderWide(c, s, rates, w, h)
	} else {
		renderTall(c, s, rates, w, h)
	}
	return img
}

// renderTall stacks the cards in one column, which is what a panel in its
// native orientation gets.
func renderTall(c *canvas, s metrics.Snapshot, rates metrics.Rates, w, h int) {
	inner := w - 2*margin
	y := drawHeader(c, s, margin, margin, inner)

	cpuH := 300
	drawCPU(c, s, rates, image.Rect(margin, y, margin+inner, y+cpuH))
	y += cpuH + cardGap

	memH := memoryHeight(s)
	drawMemory(c, s, image.Rect(margin, y, margin+inner, y+memH))
	y += memH + cardGap

	netH := networkHeight
	drawNetwork(c, rates, image.Rect(margin, y, margin+inner, y+netH))
	y += netH + cardGap

	// The remaining space is shared between disk and temperature. Without
	// temperature data (macOS) the disk card takes all of it.
	remaining := h - margin - y
	if len(s.Temps) == 0 {
		drawDisk(c, s, image.Rect(margin, y, margin+inner, y+remaining))
		return
	}
	half := (remaining - cardGap) / 2
	drawDisk(c, s, image.Rect(margin, y, margin+inner, y+half))
	top := y + half + cardGap
	drawTemps(c, s, image.Rect(margin, top, margin+inner, y+remaining))
}

// renderWide splits the cards into two columns, which suits the short, wide
// frame a panel mounted on its side produces. Stacking them instead would
// leave most of the width empty and run off the bottom.
func renderWide(c *canvas, s metrics.Snapshot, rates metrics.Rates, w, h int) {
	inner := w - 2*margin
	top := drawHeader(c, s, margin, margin, inner)
	bodyH := h - margin - top

	colW := (inner - cardGap) / 2
	leftX := margin
	rightX := margin + colW + cardGap
	bodyBottom := top + bodyH

	// Left column: the two cards that carry the most information.
	cpuH := bodyH * 52 / 100
	drawCPU(c, s, rates, image.Rect(leftX, top, leftX+colW, top+cpuH))
	drawMemory(c, s, image.Rect(leftX, top+cpuH+cardGap, leftX+colW, bodyBottom))

	// Right column: throughput, storage, and temperatures when there are any.
	drawNetwork(c, rates, image.Rect(rightX, top, rightX+colW, top+networkHeight))
	diskTop := top + networkHeight + cardGap

	if len(s.Temps) == 0 {
		drawDisk(c, s, image.Rect(rightX, diskTop, rightX+colW, bodyBottom))
		return
	}
	diskH := 180
	drawDisk(c, s, image.Rect(rightX, diskTop, rightX+colW, diskTop+diskH))
	tempsTop := diskTop + diskH + cardGap
	drawTemps(c, s, image.Rect(rightX, tempsTop, rightX+colW, bodyBottom))
}

const networkHeight = 180

// memoryHeight leaves room for the swap row only when the system has swap,
// which is usual on macOS and often absent on Linux. The values are the
// card's content height plus its bottom padding.
func memoryHeight(s metrics.Snapshot) int {
	if s.Swap.Total > 0 {
		return 288
	}
	return 200
}

// drawHeader renders the hostname and uptime, and returns the next free y.
func drawHeader(c *canvas, s metrics.Snapshot, x, y, w int) int {
	c.text(x+4, y+52, s.Hostname, 40, c.theme.Text, false)
	c.textRight(x+w-4, y+50, humanDuration(s.Uptime), 26, c.theme.Dim, true)
	c.textRight(x+w-4, y+80, "uptime", 18, c.theme.Dim, false)
	return y + headerHeight
}

// drawCPU renders CPU utilisation: a large percentage, a bar, and the load
// averages.
func drawCPU(c *canvas, s metrics.Snapshot, rates metrics.Rates, r image.Rectangle) {
	c.panel(r, "CPU")

	col := c.theme.severity(rates.CPUPercent)
	pct := fmt.Sprintf("%.0f", rates.CPUPercent)

	// A large number reads at a glance from across a desk, which is the whole
	// point of a side panel. It is anchored to the bottom of the card so a
	// shorter card in the wide layout still lines up.
	baseline := r.Max.Y - 110
	c.text(r.Min.X+36, baseline, pct, 130, col, true)
	c.text(r.Min.X+36+c.textWidth(pct, 130, true)+8, baseline, "%", 44, col, false)

	c.bar(image.Rect(r.Min.X+36, baseline+30, r.Max.X-36, baseline+58), rates.CPUPercent/100, col)

	load := fmt.Sprintf("%.2f   %.2f   %.2f", s.Load[0], s.Load[1], s.Load[2])
	c.text(r.Min.X+36, r.Max.Y-16, "LOAD", 20, c.theme.Dim, false)
	c.textRight(r.Max.X-36, r.Max.Y-16, load, 22, c.theme.Text, true)
}

// drawMemory renders the memory card, including swap when it is configured.
func drawMemory(c *canvas, s metrics.Snapshot, r image.Rectangle) {
	mem := s.Mem
	swap := s.Swap
	c.panel(r, "MEMORY")

	col := c.theme.severity(mem.UsedPercent())
	c.textRight(r.Max.X-24, r.Min.Y+38,
		fmt.Sprintf("%s / %s", humanBytes(mem.Used), humanBytes(mem.Total)), 22, c.theme.Text, true)

	c.text(r.Min.X+36, r.Min.Y+120, fmt.Sprintf("%.0f%%", mem.UsedPercent()), 64, col, true)
	c.bar(image.Rect(r.Min.X+36, r.Min.Y+140, r.Max.X-36, r.Min.Y+172), mem.UsedPercent()/100, col)

	if swap.Total > 0 {
		c.text(r.Min.X+36, r.Min.Y+214, "SWAP", 20, c.theme.Dim, false)
		c.textRight(r.Max.X-36, r.Min.Y+214,
			fmt.Sprintf("%s / %s", humanBytes(swap.Used), humanBytes(swap.Total)), 22, c.theme.Text, true)
		c.bar(image.Rect(r.Min.X+36, r.Min.Y+228, r.Max.X-36, r.Min.Y+252),
			swap.UsedPercent()/100, c.theme.severity(swap.UsedPercent()))
	}
}

// drawNetwork renders receive and transmit throughput.
func drawNetwork(c *canvas, rates metrics.Rates, r image.Rectangle) {
	c.panel(r, "NETWORK")

	c.text(r.Min.X+36, r.Min.Y+92, "DOWN", 20, c.theme.Dim, false)
	c.textRight(r.Max.X-36, r.Min.Y+96, humanRate(rates.RxPerSec), 34, c.theme.Accent, true)

	c.text(r.Min.X+36, r.Min.Y+150, "UP", 20, c.theme.Dim, false)
	c.textRight(r.Max.X-36, r.Min.Y+154, humanRate(rates.TxPerSec), 34, c.theme.Warn, true)
}

// drawDisk renders usage of the root filesystem.
func drawDisk(c *canvas, s metrics.Snapshot, r image.Rectangle) {
	c.panel(r, "DISK")

	if len(s.Mounts) == 0 {
		c.textCentre(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2+10, "not available", 24, c.theme.Dim, false)
		return
	}
	m := s.Mounts[0]
	col := c.theme.severity(m.UsedPercent())

	// The card may stretch to fill leftover space, so centre its content rather
	// than leaving a block of dead space at the bottom.
	const contentHeight = 96
	top := r.Min.Y + 52
	if spare := r.Dy() - contentHeight - 52; spare > 0 {
		top += spare / 2
	}

	c.text(r.Min.X+36, top+50, fmt.Sprintf("%.0f%%", m.UsedPercent()), 56, col, true)
	c.textRight(r.Max.X-36, top+46,
		fmt.Sprintf("%s / %s", humanBytes(m.Used), humanBytes(m.Total)), 22, c.theme.Text, true)
	c.bar(image.Rect(r.Min.X+36, top+70, r.Max.X-36, top+100), m.UsedPercent()/100, col)
}

// drawTemps renders temperature sensors. It is only reached on systems that
// report them (Linux); macOS exposes no usable interface for an unprivileged
// tool.
func drawTemps(c *canvas, s metrics.Snapshot, r image.Rectangle) {
	c.panel(r, "TEMPERATURE")

	// Show as many sensors as the card has room for. A fixed count would
	// overflow the shorter cards the wide layout produces.
	const lineHeight = 34
	firstLine := r.Min.Y + 78
	maxRows := (r.Max.Y - 16 - firstLine) / lineHeight
	if maxRows < 1 {
		maxRows = 1
	}

	sensors := s.Temps
	if len(sensors) > maxRows {
		sensors = sensors[:maxRows]
	}

	lineY := firstLine
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
		c.text(r.Min.X+36, lineY, name, 20, c.theme.Dim, false)
		c.textRight(r.Max.X-36, lineY, fmt.Sprintf("%.0f°C", sensor.Celsius), 24, col, true)
		lineY += lineHeight
	}
}
