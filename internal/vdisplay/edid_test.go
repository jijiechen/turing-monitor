package vdisplay

import (
	"strings"
	"testing"
)

// decodeTiming is the inverse of encodeDetailedTiming, so the tests can check
// that what went in comes back out.
func decodeTiming(d []byte) timing {
	le16 := func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
	return timing{
		pixelClockHz: le16(d[0:2]) * 10000,
		hActive:      int(d[2]) | int(d[4]>>4)<<8,
		hFront:       int(d[8]) | int((d[11]>>6)&0x03)<<8,
		hSync:        int(d[9]) | int((d[11]>>4)&0x03)<<8,
		hBack:        (int(d[3]) | int(d[4]&0x0F)<<8) - (int(d[8]) | int((d[11]>>6)&0x03)<<8) - (int(d[9]) | int((d[11]>>4)&0x03)<<8),
		vActive:      int(d[5]) | int(d[7]>>4)<<8,
		vFront:       int(d[10]>>4) | int((d[11]>>2)&0x03)<<4,
		vSync:        int(d[10]&0x0F) | int(d[11]&0x03)<<4,
		vBack:        (int(d[6]) | int(d[7]&0x0F)<<8) - (int(d[10]>>4) | int((d[11]>>2)&0x03)<<4) - (int(d[10]&0x0F) | int(d[11]&0x03)<<4),
	}
}

// TestBuildEDIDStructure covers the invariants a compositor relies on. A bad
// checksum or a malformed descriptor is rejected outright, which would leave
// no display at all rather than a wrong-looking one.
func TestBuildEDIDStructure(t *testing.T) {
	e := BuildEDID(720, 1280, 183, 325, "TURZX 5.2")

	if len(e) != edidBaseBlockSize {
		t.Fatalf("length = %d, want %d", len(e), edidBaseBlockSize)
	}

	// The header is fixed and must match exactly.
	for i, want := range edidHeader {
		if e[i] != want {
			t.Fatalf("header byte %d = %#x, want %#x", i, e[i], want)
		}
	}

	// The checksum makes the whole block sum to zero modulo 256. This is the
	// single most important property: get it wrong and the block is discarded.
	var sum byte
	for _, b := range e {
		sum += b
	}
	if sum != 0 {
		t.Errorf("checksum: block sums to %d, want 0", sum)
	}

	if e[18] != 1 || e[19] != 4 {
		t.Errorf("EDID version %d.%d, want 1.4", e[18], e[19])
	}
	if e[126] != 0 {
		t.Errorf("extension count = %d, want 0", e[126])
	}
	// Bit 1 of the feature byte announces a preferred timing in descriptor 1.
	if e[24]&0x02 == 0 {
		t.Error("preferred timing flag is not set")
	}
}

// TestBuildEDIDTimingRoundTrips checks the detailed timing descriptor carries
// the resolution it was asked for, and that the pixel clock agrees with the
// totals rather than being an unrelated number.
func TestBuildEDIDTimingRoundTrips(t *testing.T) {
	for _, size := range []struct{ w, h, wmm, hmm int }{
		{720, 1280, 183, 325},
		{480, 1920, 100, 400},
		{800, 1280, 200, 320},
	} {
		e := BuildEDID(size.w, size.h, size.wmm, size.hmm, "test")
		got := decodeTiming(e[firstDescriptor : firstDescriptor+descriptorBytes])

		if got.hActive != size.w || got.vActive != size.h {
			t.Errorf("active area = %dx%d, want %dx%d", got.hActive, got.vActive, size.w, size.h)
		}
		if got.hFront != 24 || got.hSync != 48 || got.vFront != 3 || got.vSync != 6 {
			t.Errorf("blanking does not round-trip: %+v", got)
		}
		if got.hBack != 80 || got.vBack != 24 {
			t.Errorf("back porch does not round-trip: hBack=%d vBack=%d", got.hBack, got.vBack)
		}

		// The descriptor stores the pixel clock in units of 10 kHz, so the
		// value read back is the exact one quantised to that step. Comparing
		// for equality would be testing the format's precision, not this code.
		wantClock := got.hTotal() * got.vTotal() * 60
		if diff := got.pixelClockHz - wantClock; diff > 10000 || diff < -10000 {
			t.Errorf("pixel clock %d Hz is not 60 Hz over %dx%d lines (%d, off by %d)",
				got.pixelClockHz, got.hTotal(), got.vTotal(), wantClock, diff)
		}
	}
}

// TestBuildEDIDPhysicalSize checks the reported size, which decides whether a
// compositor treats the display as high density.
func TestBuildEDIDPhysicalSize(t *testing.T) {
	e := BuildEDID(720, 1280, 183, 325, "x")
	d := e[firstDescriptor : firstDescriptor+descriptorBytes]

	wmm := int(d[12]) | int(d[14]>>4)<<8
	hmm := int(d[13]) | int(d[14]&0x0F)<<8
	if wmm != 183 || hmm != 325 {
		t.Errorf("physical size = %dmm x %dmm, want 183mm x 325mm", wmm, hmm)
	}

	// Centimetre fields are rounded up, so a small panel must not report zero.
	small := BuildEDID(320, 960, 40, 120, "x")
	if small[21] == 0 || small[22] == 0 {
		t.Errorf("centimetre size rounds to zero: %d x %d", small[21], small[22])
	}
}

func TestBuildEDIDMonitorName(t *testing.T) {
	e := BuildEDID(720, 1280, 183, 325, "TURZX")
	d := e[firstDescriptor+descriptorBytes : firstDescriptor+2*descriptorBytes]

	if d[3] != 0xFC {
		t.Fatalf("descriptor tag = %#x, want 0xFC (monitor name)", d[3])
	}
	got := string(d[5:18])
	if !strings.HasPrefix(got, "TURZX") {
		t.Errorf("name = %q, want it to start with TURZX", got)
	}
	// The name is terminated by a newline and padded with spaces.
	if idx := strings.IndexByte(got, '\n'); idx != 5 {
		t.Errorf("name %q: newline at %d, want 5", got, idx)
	}
	if strings.TrimRight(got[5:], " ") != "\n" {
		t.Errorf("name %q: not padded with spaces after the terminator", got)
	}
}

func TestBuildEDIDLongNameIsTruncated(t *testing.T) {
	long := strings.Repeat("A", 40)
	e := BuildEDID(720, 1280, 183, 325, long)
	d := e[firstDescriptor+descriptorBytes : firstDescriptor+2*descriptorBytes]

	// The field is 13 bytes, so a longer name must be cut rather than overflow
	// into the next descriptor.
	if got := string(d[5:18]); got != strings.Repeat("A", 13) {
		t.Errorf("name = %q, want 13 A's", got)
	}
}

func TestBuildEDIDDeterministic(t *testing.T) {
	a := BuildEDID(720, 1280, 183, 325, "TURZX")
	b := BuildEDID(720, 1280, 183, 325, "TURZX")
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("byte %d differs between builds; a compositor remembering "+
				"display arrangements needs a stable identity", i)
		}
	}
}
