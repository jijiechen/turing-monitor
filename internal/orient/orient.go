// Package orient handles how a panel is physically mounted.
//
// The panel has a fixed native orientation: its framebuffer is 720x1280, and
// nothing in the protocol rotates what is sent to it. The device's own rotate
// commands do not apply to pushed content -- verified on hardware, where the
// image is unchanged after both. So if the panel is mounted on its side, the
// content has to be rotated before it is sent.
//
// Content is therefore authored in the orientation the user sees, and rotated
// into the panel's native orientation on the way out. Rotations by multiples of
// a quarter turn are exact pixel moves with no resampling, so text stays as
// crisp as it was drawn.
package orient

import (
	"fmt"
	"image"
	"image/draw"
	"strings"
)

// Orientation describes how the panel is mounted, relative to its native
// portrait orientation.
type Orientation int

const (
	// Portrait is the panel as it comes: taller than it is wide.
	Portrait Orientation = iota

	// Landscape means the panel has been turned a quarter turn anticlockwise,
	// so its native top edge is on the viewer's left and the viewer's up
	// direction is the panel's native right edge.
	Landscape

	// PortraitInverted is half a turn.
	PortraitInverted

	// LandscapeInverted is the other way up: the panel has been turned a
	// quarter turn clockwise, so its native top edge is on the viewer's right
	// and the viewer's up direction is the panel's native left edge.
	LandscapeInverted
)

var names = map[Orientation]string{
	Portrait:          "portrait",
	Landscape:         "landscape",
	PortraitInverted:  "portrait-inverted",
	LandscapeInverted: "landscape-inverted",
}

func (o Orientation) String() string {
	if s, ok := names[o]; ok {
		return s
	}
	return fmt.Sprintf("orientation(%d)", int(o))
}

// IsLandscape reports whether the mounted panel is wider than it is tall.
func (o Orientation) IsLandscape() bool {
	return o == Landscape || o == LandscapeInverted
}

// Parse reads an orientation by name, or by the number of quarter turns
// clockwise, which is what the panel's own rotate command uses.
func Parse(s string) (Orientation, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "portrait", "0":
		return Portrait, nil
	case "landscape", "1":
		return Landscape, nil
	case "portrait-inverted", "inverted", "2":
		return PortraitInverted, nil
	case "landscape-inverted", "3":
		return LandscapeInverted, nil
	default:
		return Portrait, fmt.Errorf("unknown orientation %q: want portrait, landscape, portrait-inverted or landscape-inverted", s)
	}
}

// All lists every orientation, for help text and validation.
func All() []Orientation {
	return []Orientation{Portrait, Landscape, PortraitInverted, LandscapeInverted}
}

// ContentSize returns the size content should be drawn at, given the panel's
// native framebuffer size.
func (o Orientation) ContentSize(nativeW, nativeH int) (int, int) {
	if o.IsLandscape() {
		return nativeH, nativeW
	}
	return nativeW, nativeH
}

// ToNative rotates content drawn at ContentSize into the panel's native
// orientation, ready to be sent.
func (o Orientation) ToNative(src image.Image) *image.RGBA {
	return Rotate90(src, int(o))
}

// Rotate90 returns src turned by quarterTurns lots of 90 degrees clockwise.
//
// The rotation is exact: every destination pixel is a copy of a source pixel,
// so nothing is interpolated and text keeps its edges. Four turns is the
// identity, including the pixel values themselves.
func Rotate90(src image.Image, quarterTurns int) *image.RGBA {
	q := ((quarterTurns % 4) + 4) % 4

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	if q == 0 {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
		return dst
	}

	dw, dh := h, w
	if q == 2 {
		dw, dh = w, h
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	// Fast path for the common case, so a video path is not paying for
	// interface method calls on every pixel. The origin is checked field by
	// field rather than against image.Point{}, because a composite literal in
	// an if condition parses as the start of the block.
	if s, ok := src.(*image.RGBA); ok && s.Rect.Min.X == 0 && s.Rect.Min.Y == 0 && s.Stride == w*4 {
		rotateRGBAPix(dst, s, q)
		return dst
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := src.At(b.Min.X+x, b.Min.Y+y)
			dx, dy := mapPoint(x, y, w, h, q)
			dst.Set(dx, dy, c)
		}
	}
	return dst
}

// mapPoint maps a source coordinate to its destination for a clockwise
// quarter-turn rotation.
func mapPoint(x, y, w, h, q int) (int, int) {
	switch q {
	case 1:
		// A quarter turn clockwise: the source's top-left ends up top-right.
		return h - 1 - y, x
	case 2:
		return w - 1 - x, h - 1 - y
	default: // 3
		return y, w - 1 - x
	}
}

// rotateRGBAPix is the same mapping over raw bytes.
func rotateRGBAPix(dst, src *image.RGBA, q int) {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	sp, dp := src.Pix, dst.Pix
	dw := dst.Stride

	for y := 0; y < h; y++ {
		srow := y * src.Stride
		for x := 0; x < w; x++ {
			si := srow + x*4
			dx, dy := mapPoint(x, y, w, h, q)
			di := dy*dw + dx*4
			dp[di+0] = sp[si+0]
			dp[di+1] = sp[si+1]
			dp[di+2] = sp[si+2]
			dp[di+3] = sp[si+3]
		}
	}
}
