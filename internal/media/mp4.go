// Package media converts container formats into the raw elementary streams the
// panel's hardware decoder expects.
package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// AnnexB start code.
var startCode = []byte{0x00, 0x00, 0x00, 0x01}

// h264 NAL unit types we care about.
const (
	nalIDR = 5
	nalSPS = 7
	nalPPS = 8
)

// ExtractAnnexB rewrites an MP4 file containing H.264 video into an Annex-B
// elementary stream, which is what the panel decodes.
//
// This is a focused demuxer: it handles the boxes needed to locate video
// samples (moov/trak/mdia/minf/stbl, the avcC configuration, and the sample
// tables). It makes no attempt to support fragmented MP4, and it ignores audio
// and every other track.
func ExtractAnnexB(r io.ReaderAt, size int64, w io.Writer) error {
	moov, err := findBox(r, 0, size, "moov")
	if err != nil {
		return fmt.Errorf("locate moov: %w", err)
	}
	if moov == nil {
		return errors.New("not an MP4 file: no moov box found")
	}

	trak, err := findVideoTrak(r, moov)
	if err != nil {
		return err
	}
	stbl, err := videoStbl(r, trak)
	if err != nil {
		return err
	}
	stsd, err := findBox(r, stbl.payload, stbl.end, "stsd")
	if err != nil || stsd == nil {
		return errors.New("video track has no stsd box")
	}
	avcc, err := findAVCC(r, stsd)
	if err != nil {
		return err
	}
	if avcc == nil {
		return errors.New("no H.264 video track found in this MP4")
	}

	cfg, err := parseAVCC(r, avcc)
	if err != nil {
		return err
	}

	samples, err := parseSampleTable(r, stbl)
	if err != nil {
		return err
	}
	if len(samples) == 0 {
		return errors.New("no video samples found")
	}

	// Emit the parameter sets up front so a decoder can initialise before the
	// first frame.
	if err := writeNALs(w, [][]byte{cfg.sps, cfg.pps}); err != nil {
		return err
	}

	// Samples are stored as length-prefixed NAL units; convert each to
	// start-code delimited form. Repeat the parameter sets before every IDR so
	// the stream can be joined at any keyframe.
	buf := make([]byte, 0, 1<<20)
	for _, s := range samples {
		if s.size == 0 {
			continue
		}
		if cap(buf) < s.size {
			buf = make([]byte, s.size)
		}
		buf = buf[:s.size]
		if _, err := r.ReadAt(buf, s.offset); err != nil {
			return fmt.Errorf("read sample at %d: %w", s.offset, err)
		}

		nals, err := splitNALs(buf, cfg.nalLengthSize)
		if err != nil {
			return err
		}
		if containsIDR(nals) {
			if err := writeNALs(w, [][]byte{cfg.sps, cfg.pps}); err != nil {
				return err
			}
		}
		if err := writeNALs(w, nals); err != nil {
			return err
		}
	}
	return nil
}

// box is a parsed MP4 box header.
type box struct {
	typ     string
	payload int64 // offset of the box body
	end     int64 // offset just past the box
}

// findBox scans the boxes in [start, end) for the first one with the given
// type. It returns nil when no such box exists.
func findBox(r io.ReaderAt, start, end int64, typ string) (*box, error) {
	for off := start; off < end; {
		b, err := readBoxHeader(r, off, end)
		if err != nil {
			return nil, err
		}
		if b.typ == typ {
			return b, nil
		}
		if b.end <= off {
			return nil, fmt.Errorf("malformed box at offset %d", off)
		}
		off = b.end
	}
	return nil, nil
}

// eachBox calls fn for every box in [start, end).
func eachBox(r io.ReaderAt, start, end int64, fn func(*box) error) error {
	for off := start; off < end; {
		b, err := readBoxHeader(r, off, end)
		if err != nil {
			return err
		}
		if err := fn(b); err != nil {
			return err
		}
		if b.end <= off {
			return fmt.Errorf("malformed box at offset %d", off)
		}
		off = b.end
	}
	return nil
}

func readBoxHeader(r io.ReaderAt, off, limit int64) (*box, error) {
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], off); err != nil {
		return nil, fmt.Errorf("read box header at %d: %w", off, err)
	}
	size := int64(binary.BigEndian.Uint32(hdr[0:4]))
	typ := string(hdr[4:8])
	payload := off + 8

	switch size {
	case 0:
		// The box extends to the end of the enclosing container.
		size = limit - off
	case 1:
		// 64-bit size follows the type.
		var ext [8]byte
		if _, err := r.ReadAt(ext[:], off+8); err != nil {
			return nil, fmt.Errorf("read extended size at %d: %w", off, err)
		}
		size = int64(binary.BigEndian.Uint64(ext[:]))
		payload = off + 16
	}
	if size < payload-off {
		return nil, fmt.Errorf("box %q at %d has invalid size %d", typ, off, size)
	}
	return &box{typ: typ, payload: payload, end: off + size}, nil
}

// findVideoTrak returns the trak box of the first video track.
func findVideoTrak(r io.ReaderAt, moov *box) (*box, error) {
	var found *box
	err := eachBox(r, moov.payload, moov.end, func(trak *box) error {
		if trak.typ != "trak" || found != nil {
			return nil
		}
		mdia, err := findBox(r, trak.payload, trak.end, "mdia")
		if err != nil || mdia == nil {
			return err
		}
		// Only tracks whose handler is 'vide' carry picture.
		hdlr, err := findBox(r, mdia.payload, mdia.end, "hdlr")
		if err != nil {
			return err
		}
		if hdlr != nil && handlerIsVideo(r, hdlr) {
			found = trak
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan tracks: %w", err)
	}
	if found == nil {
		return nil, errors.New("no video track found in this MP4")
	}
	return found, nil
}

// videoMdia returns the mdia box of a video track.
func videoMdia(r io.ReaderAt, trak *box) (*box, error) {
	mdia, err := findBox(r, trak.payload, trak.end, "mdia")
	if err != nil {
		return nil, err
	}
	if mdia == nil {
		return nil, errors.New("video track has no mdia box")
	}
	return mdia, nil
}

// videoStbl returns the sample table of a video track.
func videoStbl(r io.ReaderAt, trak *box) (*box, error) {
	mdia, err := videoMdia(r, trak)
	if err != nil {
		return nil, err
	}
	minf, err := findBox(r, mdia.payload, mdia.end, "minf")
	if err != nil {
		return nil, err
	}
	if minf == nil {
		return nil, errors.New("video track has no minf box")
	}
	stbl, err := findBox(r, minf.payload, minf.end, "stbl")
	if err != nil {
		return nil, err
	}
	if stbl == nil {
		return nil, errors.New("video track has no stbl box")
	}
	return stbl, nil
}

// handlerIsVideo reports whether an hdlr box declares the 'vide' handler.
func handlerIsVideo(r io.ReaderAt, hdlr *box) bool {
	// hdlr: version(1) flags(3) pre_defined(4) handler_type(4) ...
	var buf [4]byte
	if _, err := r.ReadAt(buf[:], hdlr.payload+8); err != nil {
		return false
	}
	return string(buf[:]) == "vide"
}

// findAVCC descends stsd -> avc1 -> avcC.
func findAVCC(r io.ReaderAt, stsd *box) (*box, error) {
	// stsd: version(1) flags(3) entry_count(4), then sample entries.
	entries := stsd.payload + 8
	var found *box
	err := eachBox(r, entries, stsd.end, func(entry *box) error {
		if found != nil {
			return nil
		}
		switch entry.typ {
		case "avc1", "avc3":
			// VisualSampleEntry: 78 bytes of fixed fields before child boxes.
			child, err := findBox(r, entry.payload+78, entry.end, "avcC")
			if err != nil {
				return err
			}
			found = child
		}
		return nil
	})
	return found, err
}

// avcConfig holds the H.264 parameter sets and NAL length field size.
type avcConfig struct {
	nalLengthSize int
	sps, pps      []byte
}

// parseAVCC decodes an AVCDecoderConfigurationRecord (ISO/IEC 14496-15 §5.2.4.1):
//
//	version(1) profile(1) compat(1) level(1)
//	6 bits reserved | 2 bits lengthSizeMinusOne
//	3 bits reserved | 5 bits numSPS, then SPS entries as length(2) + data
//	numPPS(1), then PPS entries as length(2) + data
func parseAVCC(r io.ReaderAt, avcc *box) (avcConfig, error) {
	n := avcc.end - avcc.payload
	if n < 7 || n > 4096 {
		return avcConfig{}, fmt.Errorf("implausible avcC size %d", n)
	}
	buf := make([]byte, n)
	if _, err := r.ReadAt(buf, avcc.payload); err != nil {
		return avcConfig{}, fmt.Errorf("read avcC: %w", err)
	}

	cfg := avcConfig{nalLengthSize: int(buf[4]&0x03) + 1}
	pos := 5

	// Sequence parameter sets.
	numSPS := int(buf[pos] & 0x1F)
	pos++
	for i := 0; i < numSPS; i++ {
		nal, next, err := readLengthPrefixed(buf, pos)
		if err != nil {
			return avcConfig{}, fmt.Errorf("avcC SPS %d: %w", i, err)
		}
		if cfg.sps == nil {
			cfg.sps = nal
		}
		pos = next
	}

	// Picture parameter sets.
	if pos >= len(buf) {
		return avcConfig{}, errors.New("avcC truncated before PPS")
	}
	numPPS := int(buf[pos])
	pos++
	for i := 0; i < numPPS; i++ {
		nal, next, err := readLengthPrefixed(buf, pos)
		if err != nil {
			return avcConfig{}, fmt.Errorf("avcC PPS %d: %w", i, err)
		}
		if cfg.pps == nil {
			cfg.pps = nal
		}
		pos = next
	}

	if cfg.sps == nil || cfg.pps == nil {
		return avcConfig{}, errors.New("avcC is missing SPS or PPS")
	}
	return cfg, nil
}

// readLengthPrefixed reads a 2-byte-length-prefixed NAL unit at pos and returns
// it along with the offset just past it.
func readLengthPrefixed(buf []byte, pos int) (nal []byte, next int, err error) {
	if pos+2 > len(buf) {
		return nil, 0, errors.New("truncated length")
	}
	n := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
	pos += 2
	if pos+n > len(buf) {
		return nil, 0, fmt.Errorf("declared length %d exceeds remaining %d bytes", n, len(buf)-pos)
	}
	return buf[pos : pos+n], pos + n, nil
}

// sample is one coded video frame's location in the file.
type sample struct {
	offset int64
	size   int
}

// parseSampleTable reconstructs the byte offset and size of every video sample
// from the stsz, stsc and stco/co64 boxes.
func parseSampleTable(r io.ReaderAt, stbl *box) ([]sample, error) {
	sizes, err := parseSampleSizes(r, stbl)
	if err != nil {
		return nil, err
	}
	chunkOffsets, err := parseChunkOffsets(r, stbl)
	if err != nil {
		return nil, err
	}
	spc, err := parseSamplesPerChunk(r, stbl)
	if err != nil {
		return nil, err
	}
	if len(sizes) == 0 || len(chunkOffsets) == 0 || len(spc) == 0 {
		return nil, errors.New("incomplete sample table")
	}

	samples := make([]sample, 0, len(sizes))
	sampleIdx := 0
	for chunk := 0; chunk < len(chunkOffsets) && sampleIdx < len(sizes); chunk++ {
		count := samplesPerChunkAt(spc, chunk)
		offset := chunkOffsets[chunk]
		for i := 0; i < count && sampleIdx < len(sizes); i++ {
			size := sizes[sampleIdx]
			samples = append(samples, sample{offset: offset, size: size})
			offset += int64(size)
			sampleIdx++
		}
	}
	return samples, nil
}

// stscEntry maps a run of chunks to how many samples each holds.
type stscEntry struct {
	firstChunk      int
	samplesPerChunk int
}

func parseSampleSizes(r io.ReaderAt, stbl *box) ([]int, error) {
	stsz, err := findBox(r, stbl.payload, stbl.end, "stsz")
	if err != nil || stsz == nil {
		return nil, errors.New("no stsz box")
	}
	var hdr [12]byte
	if _, err := r.ReadAt(hdr[:], stsz.payload); err != nil {
		return nil, fmt.Errorf("read stsz: %w", err)
	}
	uniform := binary.BigEndian.Uint32(hdr[4:8])
	count := int(binary.BigEndian.Uint32(hdr[8:12]))
	if count < 0 || count > 1<<24 {
		return nil, fmt.Errorf("implausible sample count %d", count)
	}

	sizes := make([]int, count)
	if uniform != 0 {
		for i := range sizes {
			sizes[i] = int(uniform)
		}
		return sizes, nil
	}

	raw := make([]byte, count*4)
	if count > 0 {
		if _, err := r.ReadAt(raw, stsz.payload+12); err != nil {
			return nil, fmt.Errorf("read stsz table: %w", err)
		}
	}
	for i := range sizes {
		sizes[i] = int(binary.BigEndian.Uint32(raw[i*4 : i*4+4]))
	}
	return sizes, nil
}

func parseChunkOffsets(r io.ReaderAt, stbl *box) ([]int64, error) {
	if stco, err := findBox(r, stbl.payload, stbl.end, "stco"); err != nil {
		return nil, err
	} else if stco != nil {
		return readOffsetTable(r, stco, 4)
	}
	if co64, err := findBox(r, stbl.payload, stbl.end, "co64"); err != nil {
		return nil, err
	} else if co64 != nil {
		return readOffsetTable(r, co64, 8)
	}
	return nil, errors.New("no stco or co64 box")
}

func readOffsetTable(r io.ReaderAt, b *box, width int) ([]int64, error) {
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], b.payload); err != nil {
		return nil, fmt.Errorf("read chunk offset header: %w", err)
	}
	count := int(binary.BigEndian.Uint32(hdr[4:8]))
	if count < 0 || count > 1<<24 {
		return nil, fmt.Errorf("implausible chunk count %d", count)
	}

	raw := make([]byte, count*width)
	if count > 0 {
		if _, err := r.ReadAt(raw, b.payload+8); err != nil {
			return nil, fmt.Errorf("read chunk offsets: %w", err)
		}
	}
	offsets := make([]int64, count)
	for i := range offsets {
		if width == 8 {
			offsets[i] = int64(binary.BigEndian.Uint64(raw[i*8 : i*8+8]))
		} else {
			offsets[i] = int64(binary.BigEndian.Uint32(raw[i*4 : i*4+4]))
		}
	}
	return offsets, nil
}

func parseSamplesPerChunk(r io.ReaderAt, stbl *box) ([]stscEntry, error) {
	stsc, err := findBox(r, stbl.payload, stbl.end, "stsc")
	if err != nil || stsc == nil {
		return nil, errors.New("no stsc box")
	}
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], stsc.payload); err != nil {
		return nil, fmt.Errorf("read stsc: %w", err)
	}
	count := int(binary.BigEndian.Uint32(hdr[4:8]))
	if count <= 0 || count > 1<<20 {
		return nil, fmt.Errorf("implausible stsc entry count %d", count)
	}

	raw := make([]byte, count*12)
	if _, err := r.ReadAt(raw, stsc.payload+8); err != nil {
		return nil, fmt.Errorf("read stsc entries: %w", err)
	}
	entries := make([]stscEntry, count)
	for i := range entries {
		entries[i] = stscEntry{
			firstChunk:      int(binary.BigEndian.Uint32(raw[i*12 : i*12+4])),
			samplesPerChunk: int(binary.BigEndian.Uint32(raw[i*12+4 : i*12+8])),
		}
	}
	return entries, nil
}

// samplesPerChunkAt returns the sample count for a zero-based chunk index by
// finding the last stsc entry whose firstChunk is at or before it.
func samplesPerChunkAt(entries []stscEntry, chunk int) int {
	count := 0
	oneBased := chunk + 1
	for _, e := range entries {
		if e.firstChunk <= oneBased {
			count = e.samplesPerChunk
			continue
		}
		break
	}
	return count
}

// splitNALs splits one sample into NAL units. MP4 stores each unit with a
// length prefix whose width is given by the avcC record.
func splitNALs(data []byte, lengthSize int) ([][]byte, error) {
	var nals [][]byte
	for pos := 0; pos < len(data); {
		if pos+lengthSize > len(data) {
			return nil, errors.New("truncated NAL length")
		}
		var n int
		for i := 0; i < lengthSize; i++ {
			n = n<<8 | int(data[pos+i])
		}
		pos += lengthSize
		if n == 0 {
			continue
		}
		if pos+n > len(data) {
			return nil, fmt.Errorf("NAL length %d exceeds remaining %d bytes", n, len(data)-pos)
		}
		nals = append(nals, data[pos:pos+n])
		pos += n
	}
	return nals, nil
}

// containsIDR reports whether any NAL unit is an instantaneous decoder refresh
// slice, which marks a keyframe.
func containsIDR(nals [][]byte) bool {
	for _, n := range nals {
		if len(n) > 0 && n[0]&0x1F == nalIDR {
			return true
		}
	}
	return false
}

// writeNALs writes NAL units in Annex-B form.
func writeNALs(w io.Writer, nals [][]byte) error {
	for _, n := range nals {
		if len(n) == 0 {
			continue
		}
		if _, err := w.Write(startCode); err != nil {
			return err
		}
		if _, err := w.Write(n); err != nil {
			return err
		}
	}
	return nil
}
