package skp

// Explicit member facts only, never discovery or complete composition admission.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
)

type vffImageReference struct {
	member string
	sha256 [32]byte
}
type vffImageLimits struct {
	images, bytes int
	pixels        uint64
}
type vffImageFact struct {
	member, format             string
	width, height              int
	sourceSHA256, pixelsSHA256 [32]byte
	minAlpha, maxAlpha         uint16
}

func readVFFImages(ctx context.Context, container *vffContainer, refs []vffImageReference, limits vffImageLimits) ([]vffImageFact, error) {
	if container == nil || limits.images <= 0 || limits.bytes <= 0 || limits.pixels == 0 {
		return nil, fmt.Errorf("VFF images require explicit positive budgets")
	}
	seen := make(map[string][32]byte)
	var facts []vffImageFact
	var usedBytes int
	var usedPixels uint64
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if prior, ok := seen[ref.member]; ok {
			if prior != ref.sha256 {
				return nil, fmt.Errorf("conflicting VFF image identity")
			}
			continue
		}
		raw, ok := container.members[ref.member]
		if !ok || len(raw) == 0 {
			return nil, fmt.Errorf("missing exact VFF image member")
		}
		if len(facts) >= limits.images || len(raw) > limits.bytes-usedBytes {
			return nil, fmt.Errorf("VFF cumulative image count or byte budget exceeded")
		}
		usedBytes += len(raw)
		if sha256.Sum256(raw) != ref.sha256 {
			return nil, fmt.Errorf("stale VFF image identity")
		}
		format, err := vffImageFraming(ctx, raw)
		if err != nil {
			return nil, err
		}
		reader := func() io.Reader { return vffImageReader{ctx, bytes.NewReader(raw)} }
		config, actual, err := image.DecodeConfig(reader())
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if err != nil || actual != format || config.Width <= 0 || config.Height <= 0 {
			return nil, fmt.Errorf("invalid VFF image configuration: %v", err)
		}
		w, h := uint64(config.Width), uint64(config.Height)
		if h > (limits.pixels-usedPixels)/w {
			return nil, fmt.Errorf("VFF cumulative image pixel budget exceeded")
		}
		usedPixels += w * h
		decoded, actual, err := image.Decode(reader())
		if canceled := ctx.Err(); canceled != nil {
			return nil, canceled
		}
		if err != nil || actual != format {
			return nil, fmt.Errorf("invalid VFF image body: %v", err)
		}
		if decoded.Bounds() != image.Rect(0, 0, config.Width, config.Height) {
			return nil, fmt.Errorf("VFF decoded image dimensions differ")
		}
		fact := vffImageFact{member: ref.member, format: format, width: config.Width, height: config.Height, sourceSHA256: ref.sha256, minAlpha: 65535}
		digest := sha256.New()
		var pixel [8]byte
		for y := 0; y < config.Height; y++ {
			for x := 0; x < config.Width; x++ {
				if x%1024 == 0 {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
				}
				c := vffStraightColor(decoded.At(x, y))
				binary.BigEndian.PutUint16(pixel[0:2], c.R)
				binary.BigEndian.PutUint16(pixel[2:4], c.G)
				binary.BigEndian.PutUint16(pixel[4:6], c.B)
				binary.BigEndian.PutUint16(pixel[6:8], c.A)
				_, _ = digest.Write(pixel[:])
				if c.A < fact.minAlpha {
					fact.minAlpha = c.A
				}
				if c.A > fact.maxAlpha {
					fact.maxAlpha = c.A
				}
			}
		}
		copy(fact.pixelsSHA256[:], digest.Sum(nil))
		seen[ref.member] = ref.sha256
		facts = append(facts, fact)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

// Avoid premultiplying transparent RGB or rounding non-opaque 8-bit channels.
func vffStraightColor(c color.Color) color.NRGBA64 {
	switch c := c.(type) {
	case color.NRGBA:
		return color.NRGBA64{R: uint16(c.R) * 257, G: uint16(c.G) * 257, B: uint16(c.B) * 257, A: uint16(c.A) * 257}
	case color.NRGBA64:
		return c
	default:
		return color.NRGBA64Model.Convert(c).(color.NRGBA64)
	}
}

type vffImageReader struct {
	ctx    context.Context
	reader *bytes.Reader
}

func (r vffImageReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func vffImageFraming(ctx context.Context, raw []byte) (string, error) {
	if bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) {
		for offset, count := 8, 0; offset < len(raw); count++ {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if len(raw)-offset < 12 {
				break
			}
			n := uint64(binary.BigEndian.Uint32(raw[offset:]))
			if n > uint64(len(raw)-offset-12) {
				break
			}
			end := offset + 12 + int(n)
			kind := string(raw[offset+4 : offset+8])
			for _, letter := range raw[offset+4 : offset+8] {
				if !(letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z') {
					return "", fmt.Errorf("invalid VFF PNG chunk name")
				}
			}
			if kind[2]&32 != 0 || kind[0]&32 == 0 && kind != "IHDR" && kind != "PLTE" && kind != "IDAT" && kind != "IEND" {
				return "", fmt.Errorf("unsupported critical or reserved VFF PNG chunk")
			}
			if count == 0 && kind != "IHDR" {
				break
			}
			if crc32.ChecksumIEEE(raw[offset+4:end-4]) != binary.BigEndian.Uint32(raw[end-4:end]) {
				break
			}
			if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
				return "", fmt.Errorf("animated VFF PNG is unsupported")
			}
			if kind == "IEND" {
				if n == 0 && end == len(raw) {
					return "png", nil
				}
				break
			}
			offset = end
		}
		return "", fmt.Errorf("invalid or unconsumed VFF PNG framing")
	}
	if len(raw) < 2 || raw[0] != 0xff || raw[1] != 0xd8 {
		return "", fmt.Errorf("unsupported VFF image signature")
	}
	// Segment lengths protect APP/EXIF thumbnails from being interpreted as main
	// image markers. Entropy data alone permits byte stuffing/restart markers.
	scan, entropy := false, false
	for offset := 2; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if entropy {
			for offset < len(raw) && raw[offset] != 0xff {
				if offset%4096 == 0 {
					if err := ctx.Err(); err != nil {
						return "", err
					}
				}
				offset++
			}
		}
		if offset >= len(raw) || raw[offset] != 0xff {
			break
		}
		for offset < len(raw) && raw[offset] == 0xff {
			if offset%4096 == 0 {
				if err := ctx.Err(); err != nil {
					return "", err
				}
			}
			offset++
		}
		if offset >= len(raw) {
			break
		}
		marker := raw[offset]
		offset++
		if entropy && (marker == 0 || marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		entropy = false
		if marker == 0xd9 {
			if scan && offset == len(raw) {
				return "jpeg", nil
			}
			break
		}
		if marker == 0 || marker == 0xd8 || marker == 1 || marker >= 0xd0 && marker <= 0xd7 || len(raw)-offset < 2 {
			break
		}
		n := int(binary.BigEndian.Uint16(raw[offset:]))
		if n < 2 || n > len(raw)-offset {
			break
		}
		offset += n
		if marker == 0xda {
			scan, entropy = true, true
		}
	}
	return "", fmt.Errorf("invalid or unconsumed VFF JPEG framing")
}
