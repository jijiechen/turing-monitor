package media

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// --- minimal MP4 construction helpers -------------------------------------

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func u16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

// box builds an ISO BMFF box: size(4) type(4) payload.
func mkBox(typ string, parts ...[]byte) []byte {
	var payload bytes.Buffer
	for _, p := range parts {
		payload.Write(p)
	}
	var out bytes.Buffer
	out.Write(u32(uint32(8 + payload.Len())))
	out.WriteString(typ)
	out.Write(payload.Bytes())
	return out.Bytes()
}

// fullBox is box with the version/flags header that ISO BMFF "full" boxes carry.
func fullBox(typ string, parts ...[]byte) []byte {
	return mkBox(typ, append([][]byte{u32(0)}, parts...)...)
}

// -- test fixture -----------------------------------------------------------

// Timing constants chosen so the frame rate is the familiar 29.97.
const (
	testTimescale = 30000
	testDelta     = 1001
)

var (
	testSPS = []byte{0x67, 0x42, 0x00, 0x1e, 0x95, 0xa8, 0x14, 0x01}
	testPPS = []byte{0x68, 0xce, 0x38, 0x80}
	testIDR = []byte{0x65, 0x88, 0x84, 0x00, 0x21, 0xff} // type 5
	testP   = []byte{0x41, 0x9a, 0x02, 0x34}             // type 1
)

// buildTestMP4 assembles a minimal but structurally valid MP4 holding one
// video track with two samples, the first of which is an IDR.
func buildTestMP4() []byte {
	// Samples are stored as 4-byte-length-prefixed NAL units.
	sample0 := append(u32(uint32(len(testIDR))), testIDR...)
	sample1 := append(u32(uint32(len(testP))), testP...)
	mdata := append(append([]byte{}, sample0...), sample1...)

	ftyp := mkBox("ftyp", []byte("isom"), u32(512), []byte("isomiso2avc1mp41"))
	mdat := mkBox("mdat", mdata)

	// The chunk offset must point at the first sample inside mdat.
	chunkOffset := uint32(len(ftyp) + 8)

	avcC := mkBox("avcC",
		[]byte{0x01, 0x42, 0x00, 0x1e}, // version, profile, compat, level
		[]byte{0xFF},                   // 6 bits reserved | lengthSizeMinusOne = 3 (4-byte lengths)
		[]byte{0xE1},                   // 3 bits reserved | numSPS = 1
		u16(uint16(len(testSPS))), testSPS,
		[]byte{0x01}, // numPPS = 1
		u16(uint16(len(testPPS))), testPPS,
	)

	// VisualSampleEntry fixed fields are 78 bytes before child boxes.
	avc1 := mkBox("avc1", make([]byte, 78), avcC)
	stsd := fullBox("stsd", u32(1), avc1)
	stsz := fullBox("stsz", u32(0), u32(2), u32(uint32(len(sample0))), u32(uint32(len(sample1))))
	stsc := fullBox("stsc", u32(1), u32(1), u32(2), u32(1))
	stco := fullBox("stco", u32(1), u32(chunkOffset))
	// One timing entry covering both samples.
	stts := fullBox("stts", u32(1), u32(2), u32(testDelta))
	stbl := mkBox("stbl", stsd, stsz, stsc, stco, stts)
	minf := mkBox("minf", stbl)

	// mdhd: creation(4) modification(4) timescale(4) duration(4) language(2) quality(2)
	mdhd := fullBox("mdhd", u32(0), u32(0), u32(testTimescale), u32(2*testDelta), u16(0), u16(0))

	// hdlr: version/flags(4) pre_defined(4) handler_type(4) ...
	hdlr := fullBox("hdlr", u32(0), []byte("vide"), make([]byte, 12))
	mdia := mkBox("mdia", mdhd, hdlr, minf)
	trak := mkBox("trak", mdia)
	moov := mkBox("moov", trak)

	return append(append(ftyp, mdat...), moov...)
}

// --- tests -----------------------------------------------------------------

func TestExtractAnnexB(t *testing.T) {
	data := buildTestMP4()

	var out bytes.Buffer
	if err := ExtractAnnexB(bytes.NewReader(data), int64(len(data)), &out); err != nil {
		t.Fatalf("ExtractAnnexB: %v", err)
	}

	got := out.Bytes()
	nals := parseAnnexB(t, got)

	// Expect SPS, PPS, then the IDR sample preceded by a repeated SPS/PPS pair,
	// then the P-frame.
	wantTypes := []byte{nalSPS, nalPPS, nalSPS, nalPPS, nalIDR, 1}
	if len(nals) != len(wantTypes) {
		t.Fatalf("got %d NAL units, want %d: %v", len(nals), len(wantTypes), nalTypes(nals))
	}
	for i, want := range wantTypes {
		if got := nals[i][0] & 0x1F; got != want {
			t.Errorf("NAL %d has type %d, want %d", i, got, want)
		}
	}
	if !bytes.Equal(nals[0], testSPS) {
		t.Errorf("first NAL = % x, want SPS % x", nals[0], testSPS)
	}
	if !bytes.Equal(nals[4], testIDR) {
		t.Errorf("IDR NAL = % x, want % x", nals[4], testIDR)
	}
	if !bytes.Equal(nals[5], testP) {
		t.Errorf("P NAL = % x, want % x", nals[5], testP)
	}
}

func TestProbeFPS(t *testing.T) {
	data := buildTestMP4()
	fps, err := ProbeFPS(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("ProbeFPS: %v", err)
	}
	want := float64(testTimescale) / float64(testDelta) // 29.97...
	if diff := fps - want; diff > 0.01 || diff < -0.01 {
		t.Errorf("ProbeFPS = %v, want %v", fps, want)
	}
}

func TestProbeFPSRejectsNonMP4(t *testing.T) {
	junk := bytes.Repeat([]byte{0xAB}, 512)
	if _, err := ProbeFPS(bytes.NewReader(junk), int64(len(junk))); err == nil {
		t.Fatal("expected an error for non-MP4 input")
	}
}

func TestExtractAnnexBRejectsNonMP4(t *testing.T) {
	junk := bytes.Repeat([]byte{0xAB}, 512)
	var out bytes.Buffer
	err := ExtractAnnexB(bytes.NewReader(junk), int64(len(junk)), &out)
	if err == nil {
		t.Fatal("expected an error for non-MP4 input")
	}
}

func TestExtractAnnexBRejectsTruncated(t *testing.T) {
	data := buildTestMP4()
	// Cut the file off before moov, so no video track can be found.
	var out bytes.Buffer
	if err := ExtractAnnexB(bytes.NewReader(data[:64]), 64, &out); err == nil {
		t.Fatal("expected an error for a truncated file")
	}
}

func TestSplitNALs(t *testing.T) {
	data := append(u32(3), []byte{0x65, 0x11, 0x22}...)
	data = append(data, u32(2)...)
	data = append(data, 0x41, 0x33)

	nals, err := splitNALs(data, 4)
	if err != nil {
		t.Fatalf("splitNALs: %v", err)
	}
	if len(nals) != 2 {
		t.Fatalf("got %d NALs, want 2", len(nals))
	}
	if nals[0][0]&0x1F != nalIDR {
		t.Errorf("first NAL type = %d, want IDR", nals[0][0]&0x1F)
	}
	if nals[1][0]&0x1F != 1 {
		t.Errorf("second NAL type = %d, want 1", nals[1][0]&0x1F)
	}
}

func TestSplitNALsRejectsOverrun(t *testing.T) {
	// Declares 99 bytes but supplies 2.
	data := append(u32(99), 0x65, 0x11)
	if _, err := splitNALs(data, 4); err == nil {
		t.Fatal("expected an error for a NAL length overrun")
	}
}

func TestContainsIDR(t *testing.T) {
	if containsIDR([][]byte{testP}) {
		t.Error("P-frame reported as an IDR")
	}
	if !containsIDR([][]byte{testP, testIDR}) {
		t.Error("IDR not detected")
	}
}

// TestExtractAnnexBRealFile runs the demuxer against a real MP4 when
// TURZX_TEST_MP4 points at one, which is how the implementation was validated
// against the vendor's own sample videos.
func TestExtractAnnexBRealFile(t *testing.T) {
	path := os.Getenv("TURZX_TEST_MP4")
	if path == "" {
		t.Skip("set TURZX_TEST_MP4 to a real .mp4 to run this")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := ExtractAnnexB(f, info.Size(), &out); err != nil {
		t.Fatalf("ExtractAnnexB(%s): %v", path, err)
	}
	if out.Len() == 0 {
		t.Fatal("produced no output")
	}

	nals := parseAnnexB(t, out.Bytes())
	if nals[0][0]&0x1F != nalSPS {
		t.Errorf("stream does not start with SPS, got type %d", nals[0][0]&0x1F)
	}
	var idrs, slices int
	for _, n := range nals {
		switch n[0] & 0x1F {
		case nalIDR:
			idrs++
		case 1:
			slices++
		}
	}
	if idrs == 0 {
		t.Error("no IDR frames in the extracted stream")
	}
	t.Logf("%s: %d bytes, %d NAL units, %d IDR, %d non-IDR slices",
		path, out.Len(), len(nals), idrs, slices)
}

// parseAnnexB splits an Annex-B stream into NAL units.
func parseAnnexB(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var nals [][]byte
	for i := 0; i+3 < len(data); {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			start := i + 4
			end := len(data)
			for j := start; j+3 < len(data); j++ {
				if data[j] == 0 && data[j+1] == 0 && data[j+2] == 0 && data[j+3] == 1 {
					end = j
					break
				}
			}
			if end > start {
				nals = append(nals, data[start:end])
			}
			i = end
			continue
		}
		i++
	}
	if len(nals) == 0 {
		t.Fatal("no Annex-B NAL units found")
	}
	return nals
}

func nalTypes(nals [][]byte) []byte {
	out := make([]byte, len(nals))
	for i, n := range nals {
		out[i] = n[0] & 0x1F
	}
	return out
}
