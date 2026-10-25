package glb

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/format"
)

const (
	MaxCompositionBytes     int64 = 64 << 20
	MaxCompositionJSONBytes int64 = maxGLBJSONChunkLen
	maxCompositionDepth           = 64
	maxCompositionTokens          = 200000
	glbBINChunkType               = 0x004E4942
)

var _ format.Model3DCompositionProvider = (*Plugin)(nil)

// ReadModel3DComposition proves the bounded primary has no external resources
// under the initial GLB profile. It does not validate binary geometry or images.
func (*Plugin) ReadModel3DComposition(ctx context.Context, input io.ReaderAt, size int64) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil || size < 20 || size > MaxCompositionBytes || size%4 != 0 {
		return nil, fmt.Errorf("GLB composition requires an aligned primary of 20 bytes to 64 MiB")
	}
	read := func(dst []byte, offset int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n, err := input.ReadAt(dst, offset); n != len(dst) || err != nil && err != io.EOF {
			if err == nil || err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return fmt.Errorf("read GLB composition structure: %w", err)
		}
		return ctx.Err()
	}
	var header [20]byte
	if err := read(header[:], 0); err != nil {
		return nil, err
	}
	if string(header[:4]) != glbMagic || binary.LittleEndian.Uint32(header[4:8]) != 2 || int64(binary.LittleEndian.Uint32(header[8:12])) != size {
		return nil, fmt.Errorf("invalid GLB 2.0 header or observed length")
	}
	jsonLength := int64(binary.LittleEndian.Uint32(header[12:16]))
	if binary.LittleEndian.Uint32(header[16:20]) != glbJSONChunkType || jsonLength == 0 || jsonLength%4 != 0 || jsonLength > MaxCompositionJSONBytes || jsonLength > size-20 {
		return nil, fmt.Errorf("invalid GLB composition JSON chunk")
	}
	raw := make([]byte, jsonLength)
	if err := read(raw, 20); err != nil {
		return nil, err
	}
	doc, err := readCompositionJSON(ctx, raw)
	if err != nil {
		return nil, err
	}
	end := 20 + jsonLength
	binLength := int64(0)
	if end != size {
		var chunk [8]byte
		if size-end < int64(len(chunk)) {
			return nil, fmt.Errorf("truncated GLB composition BIN header")
		}
		if err := read(chunk[:], end); err != nil {
			return nil, err
		}
		binLength = int64(binary.LittleEndian.Uint32(chunk[:4]))
		if binary.LittleEndian.Uint32(chunk[4:]) != glbBINChunkType || binLength == 0 || binLength%4 != 0 || binLength != size-end-8 {
			return nil, fmt.Errorf("invalid GLB composition BIN chunk or trailing content")
		}
	}
	bufferLength, err := validateCompositionResources(doc, binLength)
	if err != nil {
		return nil, err
	}
	// Read the end even when there is no padding, so an observed size cannot hide
	// truncation. Never walk the binary payload to discover more dependencies.
	var tail [3]byte
	padding := binLength - bufferLength
	if padding > 0 {
		if err := read(tail[:padding], size-padding); err != nil {
			return nil, err
		}
		for _, b := range tail[:padding] {
			if b != 0 {
				return nil, fmt.Errorf("invalid GLB BIN padding")
			}
		}
	} else if err := read(tail[:1], size-1); err != nil {
		return nil, err
	}
	return []string{}, nil
}

// Token decoding rejects duplicate keys before a generic JSON map can discard
// their first values. Shared metadata structs intentionally do not do this job.
func readCompositionJSON(ctx context.Context, raw []byte) (map[string]any, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid GLB composition UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	tokens := 0
	var value func(int) (any, error)
	next := func() (json.Token, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tokens++
		if tokens > maxCompositionTokens {
			return nil, fmt.Errorf("GLB composition JSON token budget exceeded")
		}
		return decoder.Token()
	}
	value = func(depth int) (any, error) {
		if depth > maxCompositionDepth {
			return nil, fmt.Errorf("GLB composition JSON depth budget exceeded")
		}
		token, err := next()
		if err != nil {
			return nil, err
		}
		switch token {
		case json.Delim('{'):
			object := map[string]any{}
			for decoder.More() {
				key, err := next()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid GLB composition JSON key")
				}
				if _, exists := object[name]; exists {
					return nil, fmt.Errorf("duplicate GLB composition JSON key")
				}
				child, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				if name == "uri" {
					return nil, fmt.Errorf("GLB composition does not accept URI fields")
				}
				if name == "extensions" {
					extensions, ok := child.(map[string]any)
					if !ok || len(extensions) != 0 {
						return nil, fmt.Errorf("GLB composition extensions are unsupported")
					}
				}
				object[name] = child
			}
			if end, err := next(); err != nil || end != json.Delim('}') {
				if err == nil {
					err = fmt.Errorf("unclosed GLB composition JSON object")
				}
				return nil, err
			}
			return object, nil
		case json.Delim('['):
			array := []any{}
			for decoder.More() {
				child, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if end, err := next(); err != nil || end != json.Delim(']') {
				if err == nil {
					err = fmt.Errorf("unclosed GLB composition JSON array")
				}
				return nil, err
			}
			return array, nil
		default:
			if _, isDelimiter := token.(json.Delim); isDelimiter {
				return nil, fmt.Errorf("invalid GLB composition JSON delimiter")
			}
			return token, nil
		}
	}
	decoded, err := value(0)
	if err != nil {
		return nil, fmt.Errorf("parse GLB composition JSON: %w", err)
	}
	doc, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("GLB composition JSON must be an object")
	}
	for _, b := range raw[decoder.InputOffset():] {
		if b != ' ' {
			return nil, fmt.Errorf("invalid GLB composition JSON padding or trailing document")
		}
	}
	return doc, nil
}

func validateCompositionResources(doc map[string]any, binLength int64) (int64, error) {
	asset, ok := doc["asset"].(map[string]any)
	if !ok || asset["version"] != "2.0" {
		return 0, fmt.Errorf("GLB composition requires asset.version 2.0")
	}
	if minimum, exists := asset["minVersion"]; exists && minimum != "2.0" {
		return 0, fmt.Errorf("unsupported GLB composition minimum asset version")
	}
	for _, name := range []string{"extensionsUsed", "extensionsRequired"} {
		if declarations, exists := doc[name]; exists {
			array, ok := declarations.([]any)
			if !ok || len(array) != 0 {
				return 0, fmt.Errorf("GLB composition extensions are unsupported")
			}
		}
	}
	buffers, err := compositionEntries(doc, "buffers")
	if err != nil {
		return 0, err
	}
	views, err := compositionEntries(doc, "bufferViews")
	if err != nil {
		return 0, err
	}
	images, err := compositionEntries(doc, "images")
	if err != nil {
		return 0, err
	}
	if binLength == 0 {
		if len(buffers)+len(views)+len(images) != 0 {
			return 0, fmt.Errorf("GLB resources require the embedded BIN buffer")
		}
		return 0, nil
	}
	if len(buffers) != 1 {
		return 0, fmt.Errorf("GLB composition requires one embedded buffer")
	}
	length, ok := compositionInteger(buffers[0]["byteLength"])
	if !ok || length <= 0 || length > binLength || binLength-length > 3 {
		return 0, fmt.Errorf("invalid GLB embedded buffer length")
	}
	for _, view := range views {
		buffer, ok := compositionInteger(view["buffer"])
		if !ok || buffer != 0 {
			return 0, fmt.Errorf("GLB bufferView must reference embedded buffer 0")
		}
		offset := int64(0)
		if declared, exists := view["byteOffset"]; exists {
			offset, ok = compositionInteger(declared)
			if !ok {
				return 0, fmt.Errorf("invalid GLB bufferView offset")
			}
		}
		count, ok := compositionInteger(view["byteLength"])
		if !ok || count <= 0 || offset > length || count > length-offset {
			return 0, fmt.Errorf("GLB bufferView exceeds embedded buffer")
		}
	}
	for _, image := range images {
		index, ok := compositionInteger(image["bufferView"])
		if !ok || index >= int64(len(views)) {
			return 0, fmt.Errorf("invalid GLB image bufferView")
		}
		if mime := image["mimeType"]; mime != "image/png" && mime != "image/jpeg" {
			return 0, fmt.Errorf("GLB composition image must declare PNG or JPEG")
		}
	}
	return length, nil
}

func compositionEntries(doc map[string]any, name string) ([]map[string]any, error) {
	value, exists := doc[name]
	if !exists {
		return nil, nil
	}
	array, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid GLB composition resource array")
	}
	entries := make([]map[string]any, len(array))
	for i, entry := range array {
		entries[i], ok = entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid GLB composition resource entry")
		}
	}
	return entries, nil
}

func compositionInteger(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	// glTF integer-valued numbers may use decimal/exponent notation. Normalize
	// them exactly without floating point rounding or allocating a huge power.
	literal := number.String()
	negative := strings.HasPrefix(literal, "-")
	literal = strings.TrimPrefix(literal, "-")
	exponentText := "0"
	if at := strings.IndexAny(literal, "eE"); at >= 0 {
		exponentText, literal = literal[at+1:], literal[:at]
	}
	fraction := 0
	if at := strings.IndexByte(literal, '.'); at >= 0 {
		fraction = len(literal) - at - 1
		literal = literal[:at] + literal[at+1:]
	}
	digits := strings.TrimLeft(literal, "0")
	if digits == "" {
		return 0, true
	}
	if negative {
		return 0, false
	}
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	// Any nonzero number this far beyond its available digits is either too
	// large or fractional; guard subtraction and all subsequent allocations.
	bound := int64(len(literal)) + 19
	if err != nil || exponent < -bound || exponent > bound {
		return 0, false
	}
	power := exponent - int64(fraction)
	if power < 0 {
		zeros := len(digits) - len(strings.TrimRight(digits, "0"))
		if -power > int64(zeros) {
			return 0, false
		}
		digits = digits[:int64(len(digits))+power]
	} else {
		if int64(len(digits))+power > 19 {
			return 0, false
		}
		digits += strings.Repeat("0", int(power))
	}
	integer, err := strconv.ParseInt(digits, 10, 64)
	return integer, err == nil
}
