// Package framesrc describes a source of raw pixels.
//
// It exists so a display can also be the thing that produces its pixels. That
// is the case on Linux with evdi, where the kernel module creates a real DRM
// device and a userspace library hands back its framebuffer. The display and
// the pixel source are the same object there, which the usual split between
// "somewhere to draw" and "somewhere to capture from" cannot express.
package framesrc

// Format is a pixel layout, named after the DRM fourcc it corresponds to.
//
// It matters because a wrong guess is not a subtle failure: reading the wrong
// layout gives a picture with the channels swapped or the rows skewed.
type Format uint32

// DRM fourcc values, as reported by evdi for the framebuffer it hands out.
// Fourcc packs characters little-endian, so XRGB is "XR24" read as bytes.
const (
	FormatUnknown Format = 0
	FormatXRGB888 Format = 0x34325258 // 'X' 'R' '2' '4'
	FormatARGB888 Format = 0x34325241 // 'A' 'R' '2' '4'
	FormatXBGR888 Format = 0x34324258 // 'X' 'B' '2' '4'
	FormatABGR888 Format = 0x34324241 // 'A' 'B' '2' '4'
	FormatRGB565  Format = 0x36314752 // 'R' 'G' '1' '6'
)

func (f Format) String() string {
	switch f {
	case FormatXRGB888:
		return "XRGB8888"
	case FormatARGB888:
		return "ARGB8888"
	case FormatXBGR888:
		return "XBGR8888"
	case FormatABGR888:
		return "ABGR8888"
	case FormatRGB565:
		return "RGB565"
	default:
		return "unknown"
	}
}

// BytesPerPixel is the storage size of one pixel in this format, or 0 if the
// format is not one this package understands.
func (f Format) BytesPerPixel() int {
	switch f {
	case FormatXRGB888, FormatARGB888, FormatXBGR888, FormatABGR888:
		return 4
	case FormatRGB565:
		return 2
	default:
		return 0
	}
}

// Source is a display that produces its own frames.
//
// NextFrame blocks until a frame is available and returns it as raw pixels,
// tightly packed with no row padding, in the format the source reports. The
// returned slice is only valid until the next call.
type Source interface {
	// NextFrame returns the next frame's pixels and its dimensions.
	NextFrame() (pix []byte, width, height int, err error)

	// Format reports the pixel layout of the frames.
	Format() Format

	// Close releases the source.
	Close() error
}
