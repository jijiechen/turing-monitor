package vdisplay

// EDID synthesis.
//
// evdi_connect requires an EDID, because the panel it drives is defined by the
// descriptor rather than by hardware: a compositor reads the modes out of the
// EDID and drives whatever they describe. So to get a display of the panel's
// own resolution we have to state that resolution the way a real monitor would.
//
// This builds an EDID 1.4 base block from the VESA specification. Only the
// parts a compositor needs are filled in: one detailed timing descriptor, which
// is also flagged as preferred, plus a monitor name so the display is
// identifiable in system settings. Established and standard timings are left
// empty, which is normal for a display whose only mode is a non-standard one.
//
// Kept free of build tags so it can be tested anywhere, not only on a machine
// with evdi and an X server.

const (
	// edidBaseBlockSize is the size of an EDID 1.4 base block.
	edidBaseBlockSize = 128

	// descriptorBytes is the size of one descriptor slot in the base block.
	descriptorBytes = 18

	// firstDescriptor is the offset of descriptor slot 1.
	firstDescriptor = 54
)

// edidHeader begins every EDID block; the pattern is easy to spot in a hex
// dump and hard to produce by accident.
var edidHeader = []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

// timing is a video timing in the form an EDID descriptor records it.
type timing struct {
	pixelClockHz int
	hActive      int
	hFront       int
	hSync        int
	hBack        int
	vActive      int
	vFront       int
	vSync        int
	vBack        int
}

func (t timing) hTotal() int { return t.hActive + t.hFront + t.hSync + t.hBack }
func (t timing) vTotal() int { return t.vActive + t.vFront + t.vSync + t.vBack }

// timingFor builds a 60 Hz timing for a display of the given size.
//
// The blanking intervals are fixed rather than derived from a full CVT
// calculation. That is deliberate: nothing downstream generates a real signal
// from these numbers, so they only have to be self-consistent and enough for a
// compositor to accept the mode. The pixel clock is then computed from the
// totals, which is what makes them consistent.
func timingFor(width, height int) timing {
	const (
		hFront = 24
		hSync  = 48
		hBack  = 80
		vFront = 3
		vSync  = 6
		vBack  = 24
	)
	t := timing{
		hActive: width, hFront: hFront, hSync: hSync, hBack: hBack,
		vActive: height, vFront: vFront, vSync: vSync, vBack: vBack,
	}
	// Pixel clock is in Hz; the descriptor stores it in units of 10 kHz.
	t.pixelClockHz = t.hTotal() * t.vTotal() * 60
	return t
}

// BuildEDID synthesizes an EDID 1.4 base block describing one display.
//
// widthMM and heightMM are the physical size to report. They matter more than
// they look: a compositor derives the display's scale factor from the ratio of
// pixel size to physical size, so reporting a realistic size for a small panel
// makes it be treated as a high density display and leaves a cramped logical
// desktop. Pass the size that yields the intended scale.
func BuildEDID(width, height, widthMM, heightMM int, name string) []byte {
	b := make([]byte, edidBaseBlockSize)

	copy(b, edidHeader)

	// Manufacturer ID, three letters packed into 5 bits each, big-endian,
	// with bit 15 left clear.
	putManufacturer(b, "TUR")

	// Product code and serial. Arbitrary but stable, so a compositor that
	// remembers display arrangements keeps recognising this one.
	putUint16LE(b[10:], 0x0050) // matches the panel's own product ID
	putUint32LE(b[12:], 0x00000001)

	b[16] = 0  // week of manufacture, 0 meaning unspecified
	b[17] = 30 // year, offset from 1990
	b[18] = 1  // EDID version
	b[19] = 4  // EDID revision, so 1.4

	// Video input definition: digital, 8 bits per primary, DisplayPort.
	b[20] = 0xA5

	// Physical size in centimetres, rounded up so a small panel does not
	// report zero.
	b[21] = clampByte((widthMM + 9) / 10)
	b[22] = clampByte((heightMM + 9) / 10)

	// Gamma, as (gamma * 100) - 100. 2.2 is the usual value.
	b[23] = 120

	// Feature support: digital separate sync, and a preferred timing mode is
	// present in descriptor 1.
	b[24] = 0x0A

	// Chromaticity. These are the standard sRGB primaries, which is the right
	// answer for a panel of this kind and keeps the numbers meaningful.
	copy(b[25:35], []byte{
		0xEE, 0x95, 0xA3, 0x55, 0x4F, 0x9F, 0x26, 0x0F, 0x50, 0x54,
	})

	// Established and standard timings are left at zero: the display's only
	// mode is the one in descriptor 1.

	dtd := encodeDetailedTiming(timingFor(width, height), widthMM, heightMM)
	copy(b[firstDescriptor:], dtd)

	// The remaining descriptor slots hold the monitor name and are otherwise
	// unused, which is signalled by a zero tag.
	copy(b[firstDescriptor+descriptorBytes:], encodeMonitorName(name))
	for i := 2; i < 4; i++ {
		off := firstDescriptor + i*descriptorBytes
		b[off+2] = 0x00 // display descriptor
		b[off+3] = 0x10 // dummy descriptor
	}

	b[126] = 0 // no extension blocks
	b[127] = checksum(b[:127])

	return b
}

// encodeDetailedTiming packs a timing into the 18-byte descriptor format.
func encodeDetailedTiming(t timing, widthMM, heightMM int) []byte {
	d := make([]byte, descriptorBytes)

	// Pixel clock in 10 kHz units, little-endian.
	putUint16LE(d[0:], uint16(t.pixelClockHz/10000))

	hBlank, vBlank := t.hTotal()-t.hActive, t.vTotal()-t.vActive

	d[2] = byte(t.hActive & 0xFF)
	d[3] = byte(hBlank & 0xFF)
	d[4] = byte((t.hActive>>8)<<4 | (hBlank >> 8))

	d[5] = byte(t.vActive & 0xFF)
	d[6] = byte(vBlank & 0xFF)
	d[7] = byte((t.vActive>>8)<<4 | (vBlank >> 8))

	d[8] = byte(t.hFront & 0xFF)
	d[9] = byte(t.hSync & 0xFF)
	d[10] = byte((t.vFront&0x0F)<<4 | (t.vSync & 0x0F))
	d[11] = byte((t.hFront>>8)<<6 | (t.hSync>>8)<<4 | (t.vFront>>4)<<2 | (t.vSync >> 4))

	d[12] = byte(widthMM & 0xFF)
	d[13] = byte(heightMM & 0xFF)
	d[14] = byte((widthMM>>8)<<4 | (heightMM >> 8))

	d[15], d[16] = 0, 0 // no border

	// Digital separate sync, both polarities positive.
	d[17] = 0x1B

	return d
}

// encodeMonitorName packs a display descriptor carrying a product name, which
// is what a compositor shows in its display list. Names longer than the 13
// bytes available are truncated.
func encodeMonitorName(name string) []byte {
	d := make([]byte, descriptorBytes)
	d[3] = 0xFC // monitor name tag

	const maxLen = 13
	for i := 0; i < maxLen; i++ {
		switch {
		case i < len(name):
			d[5+i] = name[i]
		case i == len(name):
			// A newline terminates the string, and the rest is padded with
			// spaces; this is the convention monitors use.
			d[5+i] = '\n'
		default:
			d[5+i] = ' '
		}
	}
	return d
}

// checksum returns the byte that makes the sum of the block zero, which is how
// EDID detects corruption.
func checksum(b []byte) byte {
	var sum byte
	for _, v := range b {
		sum += v
	}
	return -sum
}

// putManufacturer packs three letters into the manufacturer ID field.
func putManufacturer(b []byte, id string) {
	if len(id) != 3 {
		return
	}
	v := uint16(id[0]-'A'+1)<<10 | uint16(id[1]-'A'+1)<<5 | uint16(id[2]-'A'+1)
	b[8] = byte(v >> 8)
	b[9] = byte(v)
}

func putUint16LE(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func putUint32LE(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func clampByte(v int) byte {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	default:
		return byte(v)
	}
}
