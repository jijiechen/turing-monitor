package orient

import (
	"fmt"
	"image"
	"image/color"
	"testing"
)

// marked builds a small image whose four corners and centre are all different,
// so a wrong rotation is visible as a specific pixel in the wrong place rather
// than an image that merely looks odd.
func marked() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(0, 0, color.RGBA{R: 1, A: 255}) // top-left
	img.Set(2, 0, color.RGBA{G: 1, A: 255}) // top-right
	img.Set(0, 1, color.RGBA{B: 1, A: 255}) // bottom-left
	img.Set(2, 1, color.RGBA{R: 1, G: 1, A: 255})
	return img
}

func at(img image.Image, x, y int) color.RGBA {
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// TestRotate90MapsCorners pins the direction of rotation. Getting it backwards
// is the easiest mistake here and produces a plausible-looking but wrong
// result, so each corner is checked by name.
func TestRotate90MapsCorners(t *testing.T) {
	src := marked() // 3x2

	// A quarter turn clockwise: 3x2 becomes 2x3, and the top-left moves to
	// the top-right.
	got := Rotate90(src, 1)
	if b := got.Bounds(); b.Dx() != 2 || b.Dy() != 3 {
		t.Fatalf("size = %dx%d, want 2x3", b.Dx(), b.Dy())
	}
	if c := at(got, 1, 0); c.R != 1 {
		t.Errorf("clockwise: top-right = %+v, want the original top-left (red)", c)
	}
	if c := at(got, 1, 2); c.G != 1 {
		t.Errorf("clockwise: bottom-right = %+v, want the original top-right (green)", c)
	}
	if c := at(got, 0, 0); c.B != 1 {
		t.Errorf("clockwise: top-left = %+v, want the original bottom-left (blue)", c)
	}

	// Half a turn: same size, corners swap diagonally.
	got = Rotate90(src, 2)
	if b := got.Bounds(); b.Dx() != 3 || b.Dy() != 2 {
		t.Fatalf("180: size = %dx%d, want 3x2", b.Dx(), b.Dy())
	}
	if c := at(got, 2, 1); c.R != 1 {
		t.Errorf("180: bottom-right = %+v, want the original top-left (red)", c)
	}
	if c := at(got, 0, 0); c.R != 1 || c.G != 1 {
		t.Errorf("180: top-left = %+v, want the original bottom-right (red+green)", c)
	}

	// Three quarters clockwise is a quarter anticlockwise.
	got = Rotate90(src, 3)
	if c := at(got, 0, 2); c.R != 1 {
		t.Errorf("anticlockwise: bottom-left = %+v, want the original top-left (red)", c)
	}
}

// TestRotate90FourTurnsIsIdentity checks the round trip, including that a
// quarter turn twice is the same as half a turn.
func TestRotate90FourTurnsIsIdentity(t *testing.T) {
	src := marked()
	out := Rotate90(src, 4)

	if out.Bounds() != src.Bounds() {
		t.Fatalf("size changed: %v, want %v", out.Bounds(), src.Bounds())
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			if a, b := at(src, x, y), at(out, x, y); a != b {
				t.Errorf("pixel (%d,%d) = %+v after four turns, want %+v", x, y, b, a)
			}
		}
	}

	if a, b := Rotate90(src, 2), Rotate90(Rotate90(src, 1), 1); !sameImage(a, b) {
		t.Error("two quarter turns differ from one half turn")
	}
}

// TestRotate90NegativeAndLarge checks the turn count is normalised rather than
// indexing out of range.
func TestRotate90NegativeAndLarge(t *testing.T) {
	src := marked()
	if a, b := Rotate90(src, -1), Rotate90(src, 3); !sameImage(a, b) {
		t.Error("-1 turn differs from 3 turns")
	}
	if a, b := Rotate90(src, 9), Rotate90(src, 1); !sameImage(a, b) {
		t.Error("9 turns differs from 1 turn")
	}
}

// TestRotate90Exactness checks nothing is interpolated: a rotation by a
// multiple of a quarter turn must move pixels, not blend them.
func TestRotate90Exactness(t *testing.T) {
	// A pattern that would smear under any interpolation.
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if (x+y)%2 == 0 {
				src.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				src.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	got := Rotate90(src, 1)

	seen := map[color.RGBA]int{}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			seen[at(got, x, y)]++
		}
	}
	if len(seen) != 2 {
		t.Errorf("found %d distinct colours after rotation, want 2: interpolation occurred", len(seen))
	}
}

func TestContentSize(t *testing.T) {
	const w, h = 720, 1280
	for _, tc := range []struct {
		o     Orientation
		wantW int
		wantH int
	}{
		{Portrait, 720, 1280},
		{Landscape, 1280, 720},
		{PortraitInverted, 720, 1280},
		{LandscapeInverted, 1280, 720},
	} {
		if gotW, gotH := tc.o.ContentSize(w, h); gotW != tc.wantW || gotH != tc.wantH {
			t.Errorf("%s: ContentSize = %dx%d, want %dx%d", tc.o, gotW, gotH, tc.wantW, tc.wantH)
		}
	}
}

// TestToNativeProducesNativeSize checks the pipeline invariant: whatever the
// mounting, what is sent is always the panel's native framebuffer size.
func TestToNativeProducesNativeSize(t *testing.T) {
	const nativeW, nativeH = 720, 1280
	for _, o := range All() {
		cw, ch := o.ContentSize(nativeW, nativeH)
		content := image.NewRGBA(image.Rect(0, 0, cw, ch))
		out := o.ToNative(content)

		if b := out.Bounds(); b.Dx() != nativeW || b.Dy() != nativeH {
			t.Errorf("%s: ToNative gave %dx%d, want %dx%d", o, b.Dx(), b.Dy(), nativeW, nativeH)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Orientation
	}{
		{"portrait", Portrait}, {"0", Portrait},
		{"landscape", Landscape}, {"1", Landscape},
		{"Landscape", Landscape},
		{"portrait-inverted", PortraitInverted}, {"inverted", PortraitInverted}, {"2", PortraitInverted},
		{"landscape-inverted", LandscapeInverted}, {"3", LandscapeInverted},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	if _, err := Parse("sideways"); err == nil {
		t.Error("Parse accepted an unknown orientation")
	}
}

func sameImage(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	bo := a.Bounds()
	for y := bo.Min.Y; y < bo.Max.Y; y++ {
		for x := bo.Min.X; x < bo.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb || aa != ba {
				return false
			}
		}
	}
	return true
}

// BenchmarkRotate90 measures the cost of rotating a full panel frame, which is
// what the video path pays per frame. It decides whether a pure-Go rotation is
// fast enough for 30 fps or whether the encoding path needs a SIMD helper.
func BenchmarkRotate90(b *testing.B) {
	for _, size := range []struct{ w, h int }{{720, 1280}, {1280, 720}} {
		src := image.NewRGBA(image.Rect(0, 0, size.w, size.h))
		b.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(b *testing.B) {
			b.SetBytes(int64(len(src.Pix)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Rotate90(src, 1)
			}
		})
	}
}

// TestToNativePlacesContentTopOnTheRightEdge pins the mapping between a
// mounting and the rotation applied. It is derived rather than guessed:
// content's top row has to land on whichever native edge faces up for the
// viewer, and getting this backwards yields an upside-down-looking panel that
// is easy to mistake for a bug elsewhere.
func TestToNativePlacesContentTopOnTheRightEdge(t *testing.T) {
	const nativeW, nativeH = 720, 1280

	for _, tc := range []struct {
		o Orientation
		// wantEdge names the native edge that content's top row lands on.
		wantEdge string
	}{
		{Portrait, "top"},            // native top stays up
		{Landscape, "right"},         // panel turned anticlockwise: right edge is up
		{PortraitInverted, "bottom"}, // half a turn
		{LandscapeInverted, "left"},  // panel turned clockwise: left edge is up
	} {
		cw, ch := tc.o.ContentSize(nativeW, nativeH)
		content := image.NewRGBA(image.Rect(0, 0, cw, ch))
		// Mark the whole top row of the content.
		for x := 0; x < cw; x++ {
			content.Set(x, 0, color.RGBA{R: 255, A: 255})
		}

		native := tc.o.ToNative(content)
		if b := native.Bounds(); b.Dx() != nativeW || b.Dy() != nativeH {
			t.Fatalf("%s: native size %dx%d, want %dx%d", tc.o, b.Dx(), b.Dy(), nativeW, nativeH)
		}

		// Count marked pixels along each edge of the native framebuffer.
		counts := map[string]int{}
		for x := 0; x < nativeW; x++ {
			if c := at(native, x, 0); c.R > 128 {
				counts["top"]++
			}
			if c := at(native, x, nativeH-1); c.R > 128 {
				counts["bottom"]++
			}
		}
		for y := 0; y < nativeH; y++ {
			if c := at(native, 0, y); c.R > 128 {
				counts["left"]++
			}
			if c := at(native, nativeW-1, y); c.R > 128 {
				counts["right"]++
			}
		}

		if counts[tc.wantEdge] == 0 {
			t.Errorf("%s: content's top row did not land on the native %s edge (found: %v)",
				tc.o, tc.wantEdge, counts)
		}
	}
}
