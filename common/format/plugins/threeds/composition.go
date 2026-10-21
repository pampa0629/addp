package threeds

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	maxCompositionBytes = 256 << 10
	maxCompositionReads = 4096
	maxCompositionRefs  = 200
	maxCompositionName  = 4096
)

// ReadModel3DComposition skips geometry and animation payloads by their chunk
// lengths. No range requests are made outside the observed primary file.
func (*Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if input == nil || size < 6 || size > MaxSummaryBytes {
		return nil, fmt.Errorf("3DS composition size is outside its budget")
	}
	s := compositionScan{ctx: ctx, input: input, size: size, refs: []string{}, seen: map[string]bool{}}
	header, err := s.read(0, 6)
	if err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint16(header) != 0x4d4d || int64(binary.LittleEndian.Uint32(header[2:])) != size {
		return nil, fmt.Errorf("invalid 3DS composition main chunk")
	}
	if err := s.scan(6, size, 0, false); err != nil {
		return nil, err
	}
	return s.refs, nil
}

type compositionScan struct {
	ctx          context.Context
	input        io.ReaderAt
	size, bytes  int64
	reads        int
	refs         []string
	declarations int
	seen         map[string]bool
}

func (s *compositionScan) read(offset, length int64) ([]byte, error) {
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 || length <= 0 || offset > s.size || length > s.size-offset ||
		s.reads >= maxCompositionReads || length > maxCompositionBytes-s.bytes {
		return nil, fmt.Errorf("3DS composition structure read exceeds its budget")
	}
	s.reads++
	s.bytes += length
	data := make([]byte, int(length))
	n, err := s.input.ReadAt(data, offset)
	if err != nil || int64(n) != length {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("read 3DS composition structure: %w", err)
	}
	return data, nil
}

func (s *compositionScan) scan(offset, end int64, depth int, material bool) error {
	if depth > 64 {
		return fmt.Errorf("3DS composition nesting exceeds its budget")
	}
	for offset < end {
		if end-offset < 6 {
			return fmt.Errorf("truncated 3DS composition chunk header")
		}
		header, err := s.read(offset, 6)
		if err != nil {
			return err
		}
		id := binary.LittleEndian.Uint16(header)
		length := int64(binary.LittleEndian.Uint32(header[2:]))
		if length < 6 || length > end-offset {
			return fmt.Errorf("invalid 3DS composition chunk length")
		}
		next := offset + length
		switch {
		case id == 0x3d3d || id == 0xafff || (material && isTextureMapChunk(id)):
			if err := s.scan(offset+6, next, depth+1, material || id == 0xafff); err != nil {
				return err
			}
		case id == 0xa300 && material:
			if length <= 6 || length-6 > maxCompositionName || s.declarations >= maxCompositionRefs {
				return fmt.Errorf("3DS composition declaration exceeds its budget")
			}
			s.declarations++
			name, err := s.read(offset+6, length-6)
			if err != nil {
				return err
			}
			if name[len(name)-1] != 0 || !utf8.Valid(name[:len(name)-1]) {
				return fmt.Errorf("invalid 3DS composition texture declaration")
			}
			for _, b := range name[:len(name)-1] {
				if b == 0 {
					return fmt.Errorf("invalid 3DS composition texture declaration")
				}
			}
			ref := string(name[:len(name)-1])
			if ref != "" && !s.seen[ref] {
				s.refs = append(s.refs, ref)
				s.seen[ref] = true
			}
		}
		offset = next
	}
	return nil
}

func isTextureMapChunk(id uint16) bool {
	switch id {
	case 0xa200, 0xa204, 0xa210, 0xa220, 0xa230, 0xa33a, 0xa33c, 0xa33d, 0xa33e, 0xa340, 0xa342, 0xa344, 0xa346, 0xa348, 0xa34a:
		return true
	}
	return false
}
