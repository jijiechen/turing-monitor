package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"

	// Register the decoders we accept on the command line.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	xdraw "golang.org/x/image/draw"

	"github.com/jijiechen/turing-monitor/internal/proto"
)

// jpegQualities is tried in order until the encoded image fits the device's
// payload limit. The vendor application encodes at quality 95.
var jpegQualities = []int{95, 90, 85, 80, 70, 60, 50}

// prepareImage decodes an arbitrary image and returns a JPEG sized exactly to
// the panel, preserving the source aspect ratio and letterboxing the remainder
// with black.
//
// The panel always receives images in its native portrait orientation and does
// not scale, so the host has to do all fitting.
func prepareImage(data []byte, model proto.Model) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	w, h := model.Portrait()
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	fillBlack(canvas)

	scaled := fit(src, w, h)
	offset := image.Pt((w-scaled.Bounds().Dx())/2, (h-scaled.Bounds().Dy())/2)
	draw.Draw(canvas, scaled.Bounds().Add(offset), scaled, scaled.Bounds().Min, draw.Src)

	return encodeJPEG(canvas)
}

// fit scales src down to fit within w x h, preserving aspect ratio. Images
// smaller than the panel are left at their original size rather than being
// upscaled, which would only add blur.
func fit(src image.Image, w, h int) image.Image {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= w && sh <= h {
		return src
	}

	scale := min(float64(w)/float64(sw), float64(h)/float64(sh))
	dw := max(1, int(float64(sw)*scale))
	dh := max(1, int(float64(sh)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// encodeJPEG encodes at the highest quality that stays within the device's
// per-transfer payload limit.
func encodeJPEG(img image.Image) ([]byte, error) {
	var lastSize int
	for _, q := range jpegQualities {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, fmt.Errorf("encode JPEG: %w", err)
		}
		lastSize = buf.Len()
		if buf.Len() <= proto.MaxPayload {
			return buf.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("could not encode the image below the device's %d byte limit (best attempt %d bytes)",
		proto.MaxPayload, lastSize)
}

func fillBlack(img *image.RGBA) {
	black := color.RGBA{A: 255}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetRGBA(x, y, black)
		}
	}
}
