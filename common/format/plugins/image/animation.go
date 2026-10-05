package image

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
)

const animationReadLimit = 8 << 20
const animationFrameLimit = 10000
const animationBlockLimit = 65536

var errAnimationInvalid = errors.New("invalid animation structure")
var errAnimationBudget = errors.New("animation budget exceeded")
var errAnimationUnsupported = errors.New("unsupported animation rendering block")

type animationReader struct {
	ctx    context.Context
	input  io.Reader
	blocks int
}

func (r *animationReader) read(b []byte) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	_, err := io.ReadFull(r.input, b)
	return err
}
func (r *animationReader) skip(n int64) error {
	var buffer [4096]byte
	for n > 0 {
		size := min(n, int64(len(buffer)))
		if err := r.read(buffer[:size]); err != nil {
			return err
		}
		n -= size
	}
	return nil
}
func (r *animationReader) block() error {
	r.blocks++
	if r.blocks > animationBlockLimit {
		return errAnimationBudget
	}
	return nil
}
func (r *animationReader) subBlocks(requireData bool) error {
	seenData := false
	for {
		if err := r.block(); err != nil {
			return err
		}
		var size [1]byte
		if err := r.read(size[:]); err != nil {
			return err
		}
		if size[0] == 0 {
			if requireData && !seenData {
				return errAnimationInvalid
			}
			return nil
		}
		seenData = true
		if err := r.skip(int64(size[0])); err != nil {
			return err
		}
	}
}

// Timing is the sum of encoded delays for one cycle, independent of playback
// policy. Structural summaries never decode or retain compressed pixels.
func describeAnimation(ctx context.Context, encoding string, input io.Reader, limit *io.LimitedReader) (map[string]interface{}, error) {
	r := &animationReader{ctx: ctx, input: input}
	var count int
	var duration int64
	var animated bool
	var err error
	if encoding == "gif" {
		count, duration, err = r.gif()
	} else {
		count, duration, animated, err = r.webp()
	}
	status := "parsed"
	if encoding == "webp" && err == nil && !animated {
		status = "not_animated"
	}
	if err != nil {
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case errors.Is(err, errAnimationBudget):
			status = "budget_exceeded"
		case errors.Is(err, errAnimationUnsupported):
			status = "unsupported"
		case errors.Is(err, errAnimationInvalid):
			status = "invalid"
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			status = "invalid"
			if limit.N == 0 {
				status = "budget_exceeded"
			}
		default:
			return nil, err
		}
	}
	result := map[string]interface{}{"summary_status": status}
	if status == "parsed" {
		result["frame_count"] = count
		result["duration_ms"] = duration
	}
	return map[string]interface{}{"animation": result}, nil
}

func (r *animationReader) gif() (int, int64, error) {
	var header [13]byte
	if err := r.read(header[:]); err != nil {
		return 0, 0, err
	}
	if string(header[:6]) != "GIF87a" && string(header[:6]) != "GIF89a" {
		return 0, 0, errAnimationInvalid
	}
	width := int(binary.LittleEndian.Uint16(header[6:8]))
	height := int(binary.LittleEndian.Uint16(header[8:10]))
	if width == 0 || height == 0 {
		return 0, 0, errAnimationInvalid
	}
	globalPalette := header[10]&0x80 != 0
	if globalPalette {
		if err := r.skip(int64(3 << (header[10]&7 + 1))); err != nil {
			return 0, 0, err
		}
	}
	count := 0
	var duration int64
	var delay int64
	control := false
	for {
		if err := r.block(); err != nil {
			return 0, 0, err
		}
		var marker [1]byte
		if err := r.read(marker[:]); err != nil {
			return 0, 0, err
		}
		switch marker[0] {
		case 0x3b:
			if count == 0 || control {
				return 0, 0, errAnimationInvalid
			}
			return count, duration, nil
		case 0x21:
			if err := r.read(marker[:]); err != nil {
				return 0, 0, err
			}
			switch marker[0] {
			case 0xf9:
				var gce [6]byte
				if err := r.read(gce[:]); err != nil {
					return 0, 0, err
				}
				if control || gce[0] != 4 || gce[5] != 0 || (gce[1]>>2)&7 > 3 {
					return 0, 0, errAnimationInvalid
				}
				if gce[1]&2 != 0 {
					return 0, 0, errAnimationUnsupported
				}
				control = true
				delay = int64(binary.LittleEndian.Uint16(gce[2:4])) * 10
			case 0x01:
				return 0, 0, errAnimationUnsupported
			case 0xff:
				if err := r.read(marker[:]); err != nil {
					return 0, 0, err
				}
				if marker[0] != 11 {
					return 0, 0, errAnimationInvalid
				}
				if err := r.skip(11); err != nil {
					return 0, 0, err
				}
				if err := r.subBlocks(false); err != nil {
					return 0, 0, err
				}
			default:
				if err := r.subBlocks(false); err != nil {
					return 0, 0, err
				}
			}
		case 0x2c:
			if count >= animationFrameLimit {
				return 0, 0, errAnimationBudget
			}
			var frame [9]byte
			if err := r.read(frame[:]); err != nil {
				return 0, 0, err
			}
			x := int(binary.LittleEndian.Uint16(frame[:2]))
			y := int(binary.LittleEndian.Uint16(frame[2:4]))
			w := int(binary.LittleEndian.Uint16(frame[4:6]))
			h := int(binary.LittleEndian.Uint16(frame[6:8]))
			if w == 0 || h == 0 || x+w > width || y+h > height {
				return 0, 0, errAnimationInvalid
			}
			if frame[8]&0x80 != 0 {
				if err := r.skip(int64(3 << (frame[8]&7 + 1))); err != nil {
					return 0, 0, err
				}
			} else if !globalPalette {
				return 0, 0, errAnimationInvalid
			}
			if err := r.read(marker[:]); err != nil {
				return 0, 0, err
			}
			if marker[0] < 2 || marker[0] > 8 {
				return 0, 0, errAnimationInvalid
			}
			if err := r.subBlocks(true); err != nil {
				return 0, 0, err
			}
			count++
			duration += delay
			delay = 0
			control = false
		default:
			return 0, 0, errAnimationInvalid
		}
	}
}

func animationUint24(b []byte) int64 { return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 }
func (r *animationReader) chunk(remaining int64) (string, int64, int64, error) {
	if err := r.block(); err != nil {
		return "", 0, 0, err
	}
	if remaining < 8 {
		return "", 0, 0, errAnimationInvalid
	}
	var header [8]byte
	if err := r.read(header[:]); err != nil {
		return "", 0, 0, err
	}
	size := int64(binary.LittleEndian.Uint32(header[4:]))
	span := 8 + size + (size & 1)
	if span > remaining {
		return "", 0, 0, errAnimationInvalid
	}
	return string(header[:4]), size, span, nil
}
func (r *animationReader) padding(size int64) error {
	if size&1 == 0 {
		return nil
	}
	var b [1]byte
	if err := r.read(b[:]); err != nil {
		return err
	}
	if b[0] != 0 {
		return errAnimationInvalid
	}
	return nil
}
func (r *animationReader) webp() (int, int64, bool, error) {
	var header [12]byte
	if err := r.read(header[:]); err != nil {
		return 0, 0, false, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WEBP" {
		return 0, 0, false, errAnimationInvalid
	}
	remaining := int64(binary.LittleEndian.Uint32(header[4:8])) - 4
	kind, size, span, err := r.chunk(remaining)
	if err != nil {
		return 0, 0, false, err
	}
	if kind == "VP8 " || kind == "VP8L" {
		return 0, 0, false, nil
	}
	if kind != "VP8X" || size != 10 {
		return 0, 0, false, errAnimationInvalid
	}
	var canvas [10]byte
	if err := r.read(canvas[:]); err != nil {
		return 0, 0, false, err
	}
	if canvas[0]&2 == 0 {
		return 0, 0, false, nil
	}
	width := animationUint24(canvas[4:7]) + 1
	height := animationUint24(canvas[7:]) + 1
	if width*height > 1<<32-1 {
		return 0, 0, true, errAnimationInvalid
	}
	remaining -= span
	count := 0
	var duration int64
	seenANIM := false
	for remaining > 0 {
		kind, size, span, err = r.chunk(remaining)
		if err != nil {
			return 0, 0, true, err
		}
		switch kind {
		case "VP8X", "VP8 ", "VP8L", "ALPH":
			return 0, 0, true, errAnimationInvalid
		case "ANIM":
			if seenANIM || count != 0 || size != 6 {
				return 0, 0, true, errAnimationInvalid
			}
			seenANIM = true
			if err := r.skip(size); err != nil {
				return 0, 0, true, err
			}
		case "ANMF":
			if !seenANIM || size < 16 {
				return 0, 0, true, errAnimationInvalid
			}
			if count >= animationFrameLimit {
				return 0, 0, true, errAnimationBudget
			}
			var frame [16]byte
			if err := r.read(frame[:]); err != nil {
				return 0, 0, true, err
			}
			x := animationUint24(frame[:3]) * 2
			y := animationUint24(frame[3:6]) * 2
			w := animationUint24(frame[6:9]) + 1
			h := animationUint24(frame[9:12]) + 1
			if x+w > width || y+h > height {
				return 0, 0, true, errAnimationInvalid
			}
			if err := r.webpFrame(size - 16); err != nil {
				return 0, 0, true, err
			}
			count++
			duration += animationUint24(frame[12:15])
		default:
			if err := r.skip(size); err != nil {
				return 0, 0, true, err
			}
		}
		if err := r.padding(size); err != nil {
			return 0, 0, true, err
		}
		remaining -= span
	}
	if !seenANIM || count == 0 {
		return 0, 0, true, errAnimationInvalid
	}
	return count, duration, true, nil
}
func (r *animationReader) webpFrame(remaining int64) error {
	image := false
	alpha := false
	for remaining > 0 {
		kind, size, span, err := r.chunk(remaining)
		if err != nil {
			return err
		}
		switch kind {
		case "ALPH":
			if alpha || image || size < 1 {
				return errAnimationInvalid
			}
			alpha = true
		case "VP8 ", "VP8L":
			if image || kind == "VP8L" && alpha || size < 1 {
				return errAnimationInvalid
			}
			image = true
		case "ANMF", "ANIM", "VP8X":
			return errAnimationInvalid
		}
		if err := r.skip(size); err != nil {
			return err
		}
		if err := r.padding(size); err != nil {
			return err
		}
		remaining -= span
	}
	if !image {
		return errAnimationInvalid
	}
	return nil
}
