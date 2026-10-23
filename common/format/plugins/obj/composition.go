package obj

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/format"
)

const (
	MaxCompositionBytes     = 64 << 20
	MaxCompositionRefs      = 200
	MaxCompositionNameBytes = 4096
	MaxCompositionLineBytes = 1 << 20
)

var _ format.Model3DCompositionProvider = (*Plugin)(nil)

// MaterialLibraryDeclaration is the format-native declaration of one MTL and
// its complete raw texture references. It carries neither resolved paths nor
// permission, item identity or content bytes.
type MaterialLibraryDeclaration struct {
	Reference   string   `json:"reference"`
	TextureRefs []string `json:"texture_refs"`
}

// ReadModel3DComposition returns complete mtllib declarations, not texture
// references. Each declaration is one name, as consumed by the bound Assimp.
// The caller owns matching, path boundaries and the separate MTL observations.
func (*Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if size == 0 {
		return nil, fmt.Errorf("OBJ composition requires a nonempty primary")
	}
	return scanComposition(ctx, input, size, false)
}

// ReadMTLTextureRefs parses a caller-bound MTL without opening any dependency.
// Empty MTL files are valid. A failed read or parse never publishes partial refs.
func ReadMTLTextureRefs(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	return scanComposition(ctx, input, size, true)
}

func scanComposition(ctx context.Context, input io.ReaderAt, size int64, material bool) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size < 0 || size > MaxCompositionBytes {
		return nil, fmt.Errorf("OBJ/MTL composition size exceeds its budget")
	}
	section := io.NewSectionReader(input, 0, size)
	scanner := bufio.NewScanner(bufio.NewReaderSize(section, 64<<10))
	scanner.Buffer(make([]byte, 64<<10), MaxCompositionLineBytes+2)
	refs := []string{}
	seen := map[string]bool{}
	declarations := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line := scanner.Text()
		if len(line) > MaxCompositionLineBytes || !utf8.ValidString(line) {
			return nil, fmt.Errorf("invalid OBJ/MTL composition line")
		}
		for _, c := range line {
			if c < 32 && c != '\t' || c == 127 || c == '\ufeff' {
				return nil, fmt.Errorf("unsupported OBJ/MTL composition text")
			}
		}
		trailingSpace := strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t")
		line = strings.Trim(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			return nil, fmt.Errorf("OBJ/MTL continuation is unsupported")
		}
		keyword, remainder := compositionToken(line)
		lower := strings.ToLower(keyword)
		var ref string
		if material {
			if mtlTextureKeyword(lower) {
				var err error
				ref, err = mtlTextureName(remainder)
				if err != nil {
					return nil, err
				}
			} else if !mtlValueKeyword(lower) {
				return nil, fmt.Errorf("unsupported MTL composition statement")
			}
		} else {
			switch {
			case keyword == "mtllib":
				ref = remainder
			case strings.HasPrefix(lower, "mtllib"), lower == "maplib", lower == "usemap",
				lower == "call", lower == "csh", lower == "shadow_obj", lower == "trace_obj":
				return nil, fmt.Errorf("unsupported OBJ dependency statement")
			}
		}
		if ref == "" && (keyword == "mtllib" || material && mtlTextureKeyword(lower)) {
			return nil, fmt.Errorf("missing OBJ/MTL composition reference")
		}
		if ref == "" {
			continue
		}
		if trailingSpace {
			return nil, fmt.Errorf("ambiguous OBJ/MTL dependency whitespace")
		}
		declarations++
		if declarations > MaxCompositionRefs || len(ref) > MaxCompositionNameBytes || strings.ContainsAny(ref, "#\\\"'") {
			return nil, fmt.Errorf("unsupported OBJ/MTL composition reference")
		}
		if !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read OBJ/MTL composition: %w", err)
	}
	consumed, err := section.Seek(0, io.SeekCurrent)
	if err != nil || consumed != size {
		return nil, io.ErrUnexpectedEOF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return refs, nil
}

func compositionToken(line string) (string, string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.Trim(line[i+1:], " \t")
}

func mtlTextureKeyword(keyword string) bool {
	switch keyword {
	case "map_ka", "map_kd", "map_ks", "map_d", "map_ke", "map_emissive",
		"map_bump", "bump", "map_kn", "norm", "map_disp", "disp", "map_ns",
		"map_pr", "map_pm", "map_ps":
		return true
	}
	return false
}

func mtlValueKeyword(keyword string) bool {
	switch keyword {
	case "newmtl", "ka", "kd", "ks", "ke", "ns", "ni", "d", "tr", "tf", "illum",
		"sharpness", "pr", "pm", "ps", "pc", "pcr", "aniso", "anisor":
		return true
	}
	return false
}

func mtlTextureName(remainder string) (string, error) {
	for strings.HasPrefix(remainder, "-") {
		option, rest := compositionToken(remainder)
		option = strings.ToLower(option)
		count := 1
		switch option {
		case "-o", "-s", "-t":
			count = 3
		case "-mm":
			count = 2
		case "-blendu", "-blendv", "-clamp", "-boost", "-texres", "-bm", "-imfchan":
		default:
			return "", fmt.Errorf("unsupported MTL texture option")
		}
		for i := 0; i < count; i++ {
			value, tail := compositionToken(rest)
			if value == "" || !validMTLOptionValue(option, value) {
				return "", fmt.Errorf("invalid MTL texture option argument")
			}
			rest = tail
		}
		remainder = rest
	}
	return remainder, nil
}

func validMTLOptionValue(option, value string) bool {
	switch option {
	case "-blendu", "-blendv", "-clamp":
		return value == "on" || value == "off"
	case "-imfchan":
		return len(value) == 1 && strings.Contains("rgbmlz", value)
	case "-texres":
		n, err := strconv.ParseInt(value, 10, 32)
		return err == nil && n > 0
	default:
		n, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	}
}
