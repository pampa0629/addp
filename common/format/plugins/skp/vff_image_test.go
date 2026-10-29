package skp

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func vffImageTestLimits() vffImageLimits {
	return vffImageLimits{images: 8, bytes: 1 << 20, pixels: 10000}
}
func vffImageTestContainer(raw ...[]byte) (*vffContainer, []vffImageReference) {
	c := &vffContainer{members: map[string][]byte{}}
	var refs []vffImageReference
	for i, b := range raw {
		name := string(rune('a'+i)) + "%20.png"
		c.members[name] = b
		refs = append(refs, vffImageReference{name, sha256.Sum256(b)})
	}
	return c, refs
}
func vffPNGChunk(kind string, data []byte) []byte {
	b := make([]byte, 8, 12+len(data))
	binary.BigEndian.PutUint32(b, uint32(len(data)))
	copy(b[4:], kind)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b[4:]))
}

// Authored scanlines and expected channel bytes are independent of image/png's
// encoder and color conversion, including invisible RGB and 16-bit precision.
func vffAuthoredPNG(depth byte, pixels []byte, width uint32) []byte {
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header, width)
	binary.BigEndian.PutUint32(header[4:], 1)
	header[8], header[9] = depth, 6
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	_, _ = w.Write(append([]byte{0}, pixels...))
	_ = w.Close()
	out := append([]byte("\x89PNG\r\n\x1a\n"), vffPNGChunk("IHDR", header)...)
	out = append(out, vffPNGChunk("IDAT", compressed.Bytes())...)
	return append(out, vffPNGChunk("IEND", nil)...)
}

func TestVFFImagePixelsIdentityAndAlpha(t *testing.T) {
	pixels := []byte{11, 22, 33, 0, 47, 99, 201, 128}
	a := vffAuthoredPNG(8, pixels, 2)
	b := vffAuthoredPNG(8, []byte{11, 22, 34, 0, 47, 99, 201, 128}, 2)
	c, refs := vffImageTestContainer(a, b, a)
	refs = append(refs, refs[0])
	got, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits())
	if err != nil || len(got) != 3 {
		t.Fatalf("facts: %#v %v", got, err)
	}
	var expected []byte
	for _, channel := range pixels {
		expected = binary.BigEndian.AppendUint16(expected, uint16(channel)*257)
	}
	if got[0].pixelsSHA256 != sha256.Sum256(expected) || got[0].pixelsSHA256 == got[1].pixelsSHA256 || got[0].pixelsSHA256 != got[2].pixelsSHA256 || got[0].member == got[2].member || got[0].minAlpha != 0 || got[0].maxAlpha != 32896 || got[0].width != 2 || got[0].height != 1 || got[0].format != "png" {
		t.Fatalf("identity/alpha: %#v", got)
	}
	wide := []byte{0x12, 0x34, 0x56, 0x78, 0xab, 0xcd, 0x80, 0x01, 0xff, 0xfe, 0x23, 0x45, 0x67, 0x89, 0, 0}
	c, refs = vffImageTestContainer(vffAuthoredPNG(16, wide, 2))
	got, err = readVFFImages(context.Background(), c, refs, vffImageTestLimits())
	if err != nil || got[0].pixelsSHA256 != sha256.Sum256(wide) || got[0].maxAlpha != 0x8001 {
		t.Fatalf("16-bit channels: %#v %v", got, err)
	}
}

func TestVFFImageGrayPaletteAndJPEG(t *testing.T) {
	for _, input := range []image.Image{
		image.NewGray(image.Rect(0, 0, 2, 1)),
		image.NewGray16(image.Rect(0, 0, 2, 1)),
		image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{color.NRGBA{R: 21, G: 45, B: 93, A: 0}}),
	} {
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, input); err != nil {
			t.Fatal(err)
		}
		c, refs := vffImageTestContainer(encoded.Bytes())
		if facts, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err != nil || len(facts) != 1 {
			t.Fatalf("PNG variant: %#v %v", facts, err)
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewGray(image.Rect(0, 0, 2, 1)), nil); err != nil {
		t.Fatal(err)
	}
	// An embedded thumbnail's EOI is segment data, not the main image end.
	app := []byte{0xff, 0xe1, 0, 8, 0xff, 0xd8, 0x55, 0x66, 0xff, 0xd9}
	raw := append(append(append([]byte{}, encoded.Bytes()[:2]...), app...), encoded.Bytes()[2:]...)
	c, refs := vffImageTestContainer(raw)
	got, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits())
	if err != nil || got[0].format != "jpeg" || got[0].minAlpha != 65535 {
		t.Fatalf("JPEG: %#v %v", got, err)
	}
	black := []byte{0, 0, 0, 0, 0, 0, 255, 255, 0, 0, 0, 0, 0, 0, 255, 255}
	if got[0].pixelsSHA256 != sha256.Sum256(black) {
		t.Fatal("authored black JPEG pixels differ")
	}
	sos := bytes.Index(encoded.Bytes(), []byte{0xff, 0xda})
	if sos < 0 {
		t.Fatal("JPEG fixture has no scan")
	}
	endHeader := sos + 2 + int(binary.BigEndian.Uint16(encoded.Bytes()[sos+2:]))
	emptyScan := append(append([]byte{}, encoded.Bytes()[:endHeader]...), 0xff, 0xd9)
	c, refs = vffImageTestContainer(emptyScan)
	if facts, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil || facts != nil || !strings.Contains(err.Error(), "image body") {
		t.Fatalf("header-only JPEG became body proof: %#v %v", facts, err)
	}
	for _, tail := range [][]byte{{0}, encoded.Bytes()} {
		c, refs = vffImageTestContainer(append(append([]byte{}, raw...), tail...))
		if facts, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil || facts != nil {
			t.Fatal("JPEG trailing bytes accepted")
		}
	}
}

func TestVFFImageMalformedAndUnclaimedBytes(t *testing.T) {
	raw := vffAuthoredPNG(8, []byte{1, 2, 3, 255}, 1)
	corrupt := append([]byte{}, raw...)
	corrupt[45] ^= 1
	// CRC-valid compressed body must still be actually decoded, not header-only.
	badBody := append([]byte{}, raw[:33]...)
	badBody = append(badBody, vffPNGChunk("IDAT", []byte("not zlib"))...)
	badBody = append(badBody, vffPNGChunk("IEND", nil)...)
	animation := append([]byte{}, raw[:33]...)
	animation = append(animation, vffPNGChunk("acTL", make([]byte, 8))...)
	animation = append(animation, raw[33:]...)
	unknownCritical := append([]byte{}, raw[:33]...)
	unknownCritical = append(unknownCritical, vffPNGChunk("ABCD", []byte("unexplained critical semantics"))...)
	unknownCritical = append(unknownCritical, raw[33:]...)
	for name, invalid := range map[string][]byte{
		"signature-only": raw[:8], "truncated": raw[:len(raw)-1], "CRC": corrupt,
		"body": badBody, "animation": animation, "unknown-critical": unknownCritical, "tail": append(append([]byte{}, raw...), 0),
		"concatenated": append(append([]byte{}, raw...), raw...), "JPEG-no-scan": {0xff, 0xd8, 0xff, 0xd9},
		"JPEG-bad-length": {0xff, 0xd8, 0xff, 0xe1, 0, 1, 0xff, 0xd9},
	} {
		t.Run(name, func(t *testing.T) {
			c, refs := vffImageTestContainer(raw, invalid)
			if facts, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil || facts != nil {
				t.Fatal("partial/invalid facts returned")
			}
		})
	}
	c, refs := vffImageTestContainer(raw)
	c.members["unexplained-auxiliary.bin"] = []byte("not an image")
	c.members["a .png"] = []byte("decoy")
	if _, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err != nil {
		t.Fatal(err)
	}
	delete(c.members, refs[0].member)
	if _, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil {
		t.Fatal("literal member repaired or guessed")
	}
}

func TestVFFImageBudgetsStaleFactsAndCancellation(t *testing.T) {
	raw := vffAuthoredPNG(8, []byte{1, 2, 3, 255, 4, 5, 6, 128}, 2)
	c, refs := vffImageTestContainer(raw, raw)
	for name, limit := range map[string]vffImageLimits{
		"count":    {images: 1, bytes: 1 << 20, pixels: 100},
		"bytes":    {images: 8, bytes: 2*len(raw) - 1, pixels: 100},
		"pixels":   {images: 8, bytes: 1 << 20, pixels: 3},
		"explicit": {},
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := readVFFImages(context.Background(), c, refs, limit); err == nil || got != nil {
				t.Fatal("budget exceeded")
			}
		})
	}
	limit := vffImageLimits{images: 1, bytes: len(raw), pixels: 2}
	if got, err := readVFFImages(context.Background(), c, []vffImageReference{refs[0], refs[0]}, limit); err != nil || len(got) != 1 {
		t.Fatal("same member counted twice")
	}
	old := refs[0]
	refs[0].sha256[0] ^= 1
	if got, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil || got != nil {
		t.Fatal("stale digest accepted")
	}
	if _, err := readVFFImages(context.Background(), c, []vffImageReference{old, refs[0]}, vffImageTestLimits()); err == nil {
		t.Fatal("conflicting repeat accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readVFFImages(ctx, c, []vffImageReference{old}, vffImageTestLimits()); err == nil {
		t.Fatal("cancellation ignored")
	}
	checks := &vffMaterialCancelContext{Context: context.Background(), remaining: 8}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	checks.Context, checks.cancel = ctx, cancel
	if got, err := readVFFImages(checks, c, []vffImageReference{old}, vffImageTestLimits()); err == nil || got != nil {
		t.Fatal("in-flight cancellation ignored")
	}
}

func TestVFFImageBudgetsBeforeAllocationAndMaterialFacts(t *testing.T) {
	// A syntactically valid large header with an invalid tiny body must fail
	// on the allocation budget, before attempting to decompress the body.
	raw := vffAuthoredPNG(8, []byte{1, 2, 3, 255}, 1)
	header := append([]byte{}, raw[16:29]...)
	binary.BigEndian.PutUint32(header, 1000)
	binary.BigEndian.PutUint32(header[4:], 1000)
	huge := append(append([]byte{}, raw[:8]...), vffPNGChunk("IHDR", header)...)
	huge = append(huge, raw[33:]...)
	c, refs := vffImageTestContainer(huge)
	if got, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil || got != nil || !strings.Contains(err.Error(), "pixel budget") {
		t.Fatalf("allocation was not gated: %#v %v", got, err)
	}
	// XML declaration success alone must not establish a decodable image.
	c, _ = vffMaterialControl()
	mats, err := readVFFMaterials(context.Background(), c, vffMaterialLimits())
	if err != nil {
		t.Fatal(err)
	}
	refs = []vffImageReference{{mats[0].image, mats[0].imageSHA256}}
	if got, err := readVFFImages(context.Background(), c, refs, vffImageTestLimits()); err == nil || got != nil {
		t.Fatal("signature-only XML fact became image proof")
	}
}
