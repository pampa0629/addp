package skp

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

// These are authored ZIP/VFF container controls, not SketchUp-produced models
// or positive evidence of a complete model/composition profile.
type vffTestMember struct {
	name   string
	data   []byte
	method uint16
}

func vffFixture(t *testing.T, members ...vffTestMember) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zip.NewWriter(&compressed)
	var records []vffZIPRecord
	for _, member := range members {
		body := member.data
		if member.method == zip.Deflate {
			var stream bytes.Buffer
			f, err := flate.NewWriter(&stream, flate.DefaultCompression)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Write(body); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			body = stream.Bytes()
		}
		h := &zip.FileHeader{Name: member.name, Method: member.method, Flags: 0x800, ReaderVersion: 20,
			CRC32: crc32.ChecksumIEEE(member.data), CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(member.data))}
		h.SetMode(0644)
		w, err := writer.CreateRaw(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(body); err != nil {
			t.Fatal(err)
		}
		records = append(records, vffZIPRecord{crc: h.CRC32})
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	var header bytes.Buffer
	header.Write([]byte{0xff, 0xfe, 0xff, 14})
	for _, r := range "SketchUp Model" {
		binary.Write(&header, binary.LittleEndian, uint16(r))
	}
	version := "{26.0.433}"
	header.Write([]byte{0xff, 0xfe, 0xff, byte(len(version))})
	for _, r := range version {
		binary.Write(&header, binary.LittleEndian, uint16(r))
	}
	header.WriteString("VFF")
	binary.Write(&header, binary.LittleEndian, uint16(8))
	binary.Write(&header, binary.LittleEndian, uint16(1))
	binary.Write(&header, binary.LittleEndian, uint16(17))
	binary.Write(&header, binary.LittleEndian, foldedVFFCRC(records))
	return append(header.Bytes(), compressed.Bytes()...)
}

func vffTestLimits() vffReadLimits {
	return vffReadLimits{sourceBytes: 4 << 20, readBytes: 8 << 20, expandedBytes: 4 << 20, memberBytes: 2 << 20, members: 128}
}
func readTestVFF(data []byte, limits vffReadLimits) (*vffContainer, error) {
	return readVFFContainer(context.Background(), bytes.NewReader(data), int64(len(data)), limits)
}
func directoryPosition(data []byte) int { return bytes.Index(data, []byte{'P', 'K', 1, 2}) }
func basePosition(data []byte) int      { return bytes.Index(data, []byte{'P', 'K', 3, 4}) }

// Reproduce the exact inactive ZIP64 reservation observed in all 131 outer
// members of eight independently sourced VFF files. No actual ZIP64 sizes are
// enabled: both 32-bit headers still contain the same non-sentinel sizes.
func vffReservedLocalFixture(t *testing.T, method uint16) []byte {
	t.Helper()
	b := vffFixture(t, vffTestMember{"model.dat", []byte("exact model bytes"), method})
	local := basePosition(b)
	position := local + 30 + len("model.dat")
	extra := make([]byte, 20)
	binary.LittleEndian.PutUint16(extra, 1)
	binary.LittleEndian.PutUint16(extra[2:], 16)
	b = append(append(bytes.Clone(b[:position]), extra...), b[position:]...)
	binary.LittleEndian.PutUint16(b[local+4:], 45)
	binary.LittleEndian.PutUint16(b[local+28:], 20)
	end := b[len(b)-22:]
	binary.LittleEndian.PutUint32(end[16:], binary.LittleEndian.Uint32(end[16:])+20)
	return b
}

func TestVFFContainerExactInactiveZIP64Reservation(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		b := vffReservedLocalFixture(t, method)
		got, err := readTestVFF(b, vffTestLimits())
		if err != nil || got == nil || string(got.members["model.dat"]) != "exact model bytes" {
			t.Fatalf("exact reserved local header: %#v %v", got, err)
		}
	}
	for name, change := range map[string]func([]byte){
		"unknown-id":              func(b []byte) { b[basePosition(b)+30+len("model.dat")] = 2 },
		"wrong-length":            func(b []byte) { b[basePosition(b)+32+len("model.dat")] = 8 },
		"nonzero-original-size":   func(b []byte) { b[basePosition(b)+34+len("model.dat")] = 1 },
		"nonzero-compressed-size": func(b []byte) { b[basePosition(b)+42+len("model.dat")] = 1 },
		"unknown-version":         func(b []byte) { b[basePosition(b)+4] = 46 },
		"wrong-reserved-version":  func(b []byte) { b[basePosition(b)+4] = 20 },
		"wrong-extra-size":        func(b []byte) { b[basePosition(b)+28] = 19 },
		"active-zip64":            func(b []byte) { binary.LittleEndian.PutUint32(b[basePosition(b)+22:], 0xffffffff) },
	} {
		t.Run(name, func(t *testing.T) {
			b := vffReservedLocalFixture(t, zip.Deflate)
			change(b)
			got, err := readTestVFF(b, vffTestLimits())
			if err == nil || got != nil {
				t.Fatal("accepted an unproved local extension")
			}
		})
	}
}

func TestVFFContainerReadsOwnedExactMembers(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(map[uint16]string{zip.Store: "stored", zip.Deflate: "deflated"}[method], func(t *testing.T) {
			data := vffFixture(t, vffTestMember{"model.dat", []byte("opaque-model-semantics-not-admitted"), method}, vffTestMember{"materials/纹理/image.png", []byte("opaque-image-not-admitted"), method})
			before := bytes.Clone(data)
			got, err := readTestVFF(data, vffTestLimits())
			if err != nil {
				t.Fatal(err)
			}
			if got.version != "{26.0.433}" || got.schema != 17 || string(got.members["model.dat"]) != "opaque-model-semantics-not-admitted" || len(got.members) != 2 {
				t.Fatalf("unexpected container facts: %#v", got)
			}
			if !bytes.Equal(data, before) {
				t.Fatal("reader modified source")
			}
		})
	}
	var plugin format.FormatPlugin = NewPlugin()
	if _, ok := plugin.(format.Model3DCompositionProvider); ok {
		t.Fatal("container integrity must not advertise full SKP composition")
	}
}

func TestVFFContainerRejectsMalformedOrAmbiguousArchive(t *testing.T) {
	fixture := func() []byte {
		return vffFixture(t, vffTestMember{"model.dat", []byte("owned source bytes"), zip.Store})
	}
	cases := map[string]func([]byte) []byte{
		"header-label":       func(b []byte) []byte { b[4] ^= 1; return b },
		"version":            func(b []byte) []byte { b[38] = 'x'; return b },
		"vff-block":          func(b []byte) []byte { b[basePosition(b)-13] = 'X'; return b },
		"directory-checksum": func(b []byte) []byte { b[basePosition(b)-1] ^= 1; return b },
		"truncation":         func(b []byte) []byte { return b[:len(b)-1] },
		"trailing-bytes":     func(b []byte) []byte { return append(b, 0) },
		"archive-comment":    func(b []byte) []byte { binary.LittleEndian.PutUint16(b[len(b)-2:], 1); return b },
		"multiple-disks":     func(b []byte) []byte { binary.LittleEndian.PutUint16(b[len(b)-18:], 1); return b },
		"zip64-count":        func(b []byte) []byte { binary.LittleEndian.PutUint16(b[len(b)-12:], 0xffff); return b },
		"local-name":         func(b []byte) []byte { b[basePosition(b)+30] = 'X'; return b },
		"local-crc":          func(b []byte) []byte { b[basePosition(b)+14] ^= 1; return b },
		"actual-body-crc":    func(b []byte) []byte { b[basePosition(b)+30+len("model.dat")] ^= 1; return b },
		"central-extra":      func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+30:], 1); return b },
		"encrypted":          func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 1); return b },
		"data-descriptor":    func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+8:], 8); return b },
		"unsupported-method": func(b []byte) []byte { binary.LittleEndian.PutUint16(b[directoryPosition(b)+10:], 99); return b },
		"symlink": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[directoryPosition(b)+38:], 0120777<<16)
			return b
		},
		"mismatched-count": func(b []byte) []byte {
			binary.LittleEndian.PutUint16(b[len(b)-14:], 2)
			binary.LittleEndian.PutUint16(b[len(b)-12:], 2)
			return b
		},
		"overlap": func(b []byte) []byte { binary.LittleEndian.PutUint32(b[directoryPosition(b)+42:], 1); return b },
		"unclaimed-gap": func(b []byte) []byte {
			position := directoryPosition(b)
			b = append(append(bytes.Clone(b[:position]), 0), b[position:]...)
			end := b[len(b)-22:]
			binary.LittleEndian.PutUint32(end[16:], binary.LittleEndian.Uint32(end[16:])+1)
			return b
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := readTestVFF(mutate(fixture()), vffTestLimits())
			if err == nil || got != nil {
				t.Fatalf("expected atomic rejection, got %#v %v", got, err)
			}
		})
	}
	for _, name := range []string{"../model.dat", "/model.dat", "a//model.dat", "a/./model.dat", "a\\model.dat", "https:source/model.dat", "model.dat/"} {
		t.Run("path-"+name, func(t *testing.T) {
			body := []byte("x")
			if strings.HasSuffix(name, "/") {
				body = nil
			}
			got, err := readTestVFF(vffFixture(t, vffTestMember{name, body, zip.Store}), vffTestLimits())
			if err == nil || got != nil {
				t.Fatal("accepted invalid member name")
			}
		})
	}
	for name, entries := range map[string][]vffTestMember{
		"duplicate":     {{"model.dat", []byte("a"), zip.Store}, {"model.dat", []byte("b"), zip.Store}},
		"missing-model": {{"other.dat", []byte("a"), zip.Store}},
		"empty-model":   {{"model.dat", nil, zip.Store}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readTestVFF(vffFixture(t, entries...), vffTestLimits())
			if err == nil || got != nil {
				t.Fatal("accepted missing, empty or duplicate root")
			}
		})
	}
}

func TestVFFContainerDoesNotAcceptDeflateTrailingDataOrFalseExpansion(t *testing.T) {
	fixture := func() []byte {
		return vffFixture(t, vffTestMember{"model.dat", bytes.Repeat([]byte("x"), 4000), zip.Deflate})
	}
	t.Run("trailing-compressed-byte", func(t *testing.T) {
		b := fixture()
		base := basePosition(b)
		central := directoryPosition(b)
		b = append(append(bytes.Clone(b[:central]), 0), b[central:]...)
		central++
		binary.LittleEndian.PutUint32(b[base+18:], binary.LittleEndian.Uint32(b[base+18:])+1)
		binary.LittleEndian.PutUint32(b[central+20:], binary.LittleEndian.Uint32(b[central+20:])+1)
		end := b[len(b)-22:]
		binary.LittleEndian.PutUint32(end[16:], binary.LittleEndian.Uint32(end[16:])+1)
		got, err := readTestVFF(b, vffTestLimits())
		if err == nil || got != nil || !strings.Contains(err.Error(), "unconsumed") {
			t.Fatalf("trailing compressed byte: %#v %v", got, err)
		}
	})
	t.Run("false-size", func(t *testing.T) {
		b := fixture()
		base := basePosition(b)
		central := directoryPosition(b)
		binary.LittleEndian.PutUint32(b[base+22:], 1)
		binary.LittleEndian.PutUint32(b[central+24:], 1)
		got, err := readTestVFF(b, vffTestLimits())
		if err == nil || got != nil {
			t.Fatal("accepted false expansion declaration")
		}
	})
}

type vffTrackingReader struct {
	source         *bytes.Reader
	reads          int64
	cancel         context.CancelFunc
	remainingCalls int
}

func (r *vffTrackingReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads += int64(len(p))
	n, err := r.source.ReadAt(p, off)
	if r.cancel != nil {
		r.remainingCalls--
		if r.remainingCalls == 0 {
			r.cancel()
		}
	}
	return n, err
}

func TestVFFContainerBudgetsAndCancellationAreAtomic(t *testing.T) {
	data := vffFixture(t, vffTestMember{"model.dat", bytes.Repeat([]byte("payload"), 100), zip.Deflate}, vffTestMember{"image.png", bytes.Repeat([]byte("pixels"), 100), zip.Store})
	tests := map[string]func(*vffReadLimits){
		"source":   func(l *vffReadLimits) { l.sourceBytes = int64(len(data) - 1) },
		"read":     func(l *vffReadLimits) { l.readBytes = 60 },
		"expanded": func(l *vffReadLimits) { l.expandedBytes = 1000 },
		"member":   func(l *vffReadLimits) { l.memberBytes = 500 },
		"members":  func(l *vffReadLimits) { l.members = 1 },
	}
	for name, configure := range tests {
		t.Run(name, func(t *testing.T) {
			limits := vffTestLimits()
			configure(&limits)
			input := &vffTrackingReader{source: bytes.NewReader(data)}
			got, err := readVFFContainer(context.Background(), input, int64(len(data)), limits)
			if err == nil || got != nil {
				t.Fatalf("budget did not reject atomically: %#v %v", got, err)
			}
			if input.reads > limits.readBytes {
				t.Fatalf("underlying reads %d exceeded cap %d", input.reads, limits.readBytes)
			}
			if name == "source" && input.reads != 0 {
				t.Fatal("overlarge source was read")
			}
		})
	}
	for _, after := range []int{0, 1, 5, 8, 9} {
		t.Run("cancel", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := &vffTrackingReader{source: bytes.NewReader(data), cancel: cancel, remainingCalls: after}
			if after == 0 {
				cancel()
			}
			got, err := readVFFContainer(ctx, input, int64(len(data)), vffTestLimits())
			if !errors.Is(err, context.Canceled) || got != nil {
				t.Fatalf("cancellation lost: %#v %v", got, err)
			}
		})
	}
}

func TestVFFContainerShortReaderNeverReturnsPartialFacts(t *testing.T) {
	data := vffFixture(t, vffTestMember{"model.dat", []byte("x"), zip.Store})
	got, err := readVFFContainer(context.Background(), bytes.NewReader(data[:40]), int64(len(data)), vffTestLimits())
	if err == nil || got != nil || !errors.Is(err, io.EOF) {
		t.Fatalf("short primary: %#v %v", got, err)
	}
}

// Extracted directory CRC facts of SketchUp/testup-2 uv_tile_at.skp,
// Git blob 76281e755cf6447dd7dd0b7dbd12bb8bcdb9a497. These numeric controls
// verify the header algorithm, not geometry or full source composition.
func TestVFFHeaderCRCMatchesIndependentRealSourceFacts(t *testing.T) {
	values := []uint32{2729576849, 1230647704, 3005649881, 3686069282, 3796797426, 946848115, 3740376872, 3740376872, 3749561520, 725134242}
	records := make([]vffZIPRecord, len(values))
	for i, value := range values {
		records[i].crc = value
	}
	if actual := foldedVFFCRC(records); actual != 0xb94c7a26 {
		t.Fatalf("header fold mismatch: %08x", actual)
	}
	records[0].crc ^= 1
	if foldedVFFCRC(records) == 0xb94c7a26 {
		t.Fatal("directory mutation was ignored")
	}
}
