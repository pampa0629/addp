package stl

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/addp/common/format"
)

const (
	MaxCompositionASCIIBytes = 64 << 20
	MaxCompositionLineBytes  = 1 << 20
)

var _ format.Model3DCompositionProvider = (*Plugin)(nil)

// ReadModel3DComposition proves the single primary's structural identity. STL
// has no external file declarations; this does not validate its full geometry.
func (*Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size <= 0 {
		return nil, fmt.Errorf("STL composition requires a nonempty primary")
	}
	header := make([]byte, min(size, 84))
	if n, err := input.ReadAt(header, 0); n != len(header) || err != nil && err != io.EOF {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("read STL composition header: %w", err)
	}
	if size >= 84 && 84+50*int64(binary.LittleEndian.Uint32(header[80:84])) == size {
		// Check the observed end without scanning or allocating a large geometry.
		if size > 84 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var last [1]byte
			if n, err := input.ReadAt(last[:], size-1); n != 1 || err != nil && err != io.EOF {
				if err == nil {
					err = io.ErrUnexpectedEOF
				}
				return nil, fmt.Errorf("read STL composition end: %w", err)
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return []string{}, nil
	}
	if size > MaxCompositionASCIIBytes {
		return nil, fmt.Errorf("ASCII STL composition exceeds 64 MiB")
	}
	section := io.NewSectionReader(input, 0, size)
	scanner := bufio.NewScanner(section)
	scanner.Buffer(make([]byte, 64<<10), MaxCompositionLineBytes+2)
	state, solids := 0, 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		if len(line) > MaxCompositionLineBytes {
			return nil, fmt.Errorf("ASCII STL composition line exceeds 1 MiB")
		}
		for i := 0; i < len(line); i++ {
			if line[i] != '\t' && (line[i] < 32 || line[i] > 126) {
				return nil, fmt.Errorf("ASCII STL composition contains unsupported characters")
			}
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		valid := false
		switch state {
		case 0:
			valid = fields[0] == "solid"
			if valid {
				solids++
				state = 1
			}
		case 1:
			if fields[0] == "endsolid" {
				valid = true
				state = 0
			} else if len(fields) == 5 && fields[0] == "facet" && fields[1] == "normal" && finiteDecimalVector(fields[2:]) {
				valid = true
				state = 2
			}
		case 2:
			valid = len(fields) == 2 && fields[0] == "outer" && fields[1] == "loop"
			if valid {
				state = 3
			}
		case 3, 4, 5:
			valid = len(fields) == 4 && fields[0] == "vertex" && finiteDecimalVector(fields[1:])
			if valid {
				state++
			}
		case 6:
			valid = len(fields) == 1 && fields[0] == "endloop"
			if valid {
				state = 7
			}
		case 7:
			valid = len(fields) == 1 && fields[0] == "endfacet"
			if valid {
				state = 1
			}
		}
		if !valid {
			return nil, fmt.Errorf("invalid ASCII STL composition structure")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read ASCII STL composition: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	consumed, err := section.Seek(0, io.SeekCurrent)
	if err != nil || consumed != size {
		return nil, io.ErrUnexpectedEOF
	}
	if state != 0 || solids == 0 {
		return nil, fmt.Errorf("incomplete ASCII STL composition")
	}
	return []string{}, nil
}

func finiteDecimalVector(values []string) bool {
	for _, value := range values {
		for _, c := range value {
			if !(c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.' || c == 'e' || c == 'E') {
				return false
			}
		}
		if number, ok := parseFloat(value); !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return false
		}
	}
	return true
}
