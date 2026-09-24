package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ProbeFPS reports the average frame rate of an MP4's video track.
//
// The panel plays frames as they arrive, so the host has to tell it the
// intended rate (protocol command 15) or playback runs at whatever speed the
// USB link happens to deliver. This derives the rate from the media timescale
// and the sample timing table rather than guessing.
func ProbeFPS(r io.ReaderAt, size int64) (float64, error) {
	moov, err := findBox(r, 0, size, "moov")
	if err != nil {
		return 0, fmt.Errorf("locate moov: %w", err)
	}
	if moov == nil {
		return 0, errors.New("not an MP4 file: no moov box found")
	}
	trak, err := findVideoTrak(r, moov)
	if err != nil {
		return 0, err
	}
	mdia, err := videoMdia(r, trak)
	if err != nil {
		return 0, err
	}
	timescale, err := parseTimescale(r, mdia)
	if err != nil {
		return 0, err
	}
	if timescale == 0 {
		return 0, errors.New("video track has a zero timescale")
	}

	stbl, err := videoStbl(r, trak)
	if err != nil {
		return 0, err
	}
	stts, err := findBox(r, stbl.payload, stbl.end, "stts")
	if err != nil {
		return 0, err
	}
	if stts == nil {
		return 0, errors.New("video track has no stts box")
	}
	avgDelta, err := averageSampleDelta(r, stts)
	if err != nil {
		return 0, err
	}
	if avgDelta <= 0 {
		return 0, errors.New("video track has zero sample duration")
	}
	return float64(timescale) / avgDelta, nil
}

// parseTimescale reads the media timescale from an mdhd box.
func parseTimescale(r io.ReaderAt, mdia *box) (uint32, error) {
	mdhd, err := findBox(r, mdia.payload, mdia.end, "mdhd")
	if err != nil {
		return 0, err
	}
	if mdhd == nil {
		return 0, errors.New("video track has no mdhd box")
	}

	var ver [1]byte
	if _, err := r.ReadAt(ver[:], mdhd.payload); err != nil {
		return 0, fmt.Errorf("read mdhd version: %w", err)
	}

	// version/flags(4), then creation and modification times, then timescale.
	// Those timestamps are 32-bit in version 0 and 64-bit in version 1.
	off := int64(4 + 4 + 4)
	if ver[0] == 1 {
		off = 4 + 8 + 8
	}

	var ts [4]byte
	if _, err := r.ReadAt(ts[:], mdhd.payload+off); err != nil {
		return 0, fmt.Errorf("read mdhd timescale: %w", err)
	}
	return binary.BigEndian.Uint32(ts[:]), nil
}

// averageSampleDelta returns the mean sample duration in timescale units,
// weighted by how many samples share each duration.
func averageSampleDelta(r io.ReaderAt, stts *box) (float64, error) {
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], stts.payload); err != nil {
		return 0, fmt.Errorf("read stts header: %w", err)
	}
	count := int(binary.BigEndian.Uint32(hdr[4:8]))
	if count <= 0 || count > 1<<20 {
		return 0, fmt.Errorf("implausible stts entry count %d", count)
	}

	raw := make([]byte, count*8)
	if _, err := r.ReadAt(raw, stts.payload+8); err != nil {
		return 0, fmt.Errorf("read stts entries: %w", err)
	}

	var totalSamples, totalDelta uint64
	for i := 0; i < count; i++ {
		n := uint64(binary.BigEndian.Uint32(raw[i*8 : i*8+4]))
		d := uint64(binary.BigEndian.Uint32(raw[i*8+4 : i*8+8]))
		totalSamples += n
		totalDelta += n * d
	}
	if totalSamples == 0 {
		return 0, errors.New("stts contains no samples")
	}
	return float64(totalDelta) / float64(totalSamples), nil
}
