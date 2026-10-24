package avro

import (
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/golang/snappy"
	goavro "github.com/linkedin/goavro/v2"
)

const (
	maxHeaderBytes = 1 << 20
	maxBlockBytes  = 16 << 20
	maxBlockRows   = 1_000_000
	maxFields      = 1024
)

// The codec decodes a validated flat record. OCF framing and decompression stay
// here so no library-global limits or unbounded decompression affect requests.
type tableReader struct {
	input            *bufio.Reader
	codec            *goavro.Codec
	schema           map[string]interface{}
	fields, selected []datatype.FieldInfo
	compression      string
	sync             [16]byte
	block            []byte
	remaining        int64
	closed           bool
	failed           error
}

type contextReader struct {
	ctx   context.Context
	input io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(p)
}

func newReader(ctx context.Context, input io.Reader) (*tableReader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, fmt.Errorf("Avro input is required")
	}
	r := &tableReader{input: bufio.NewReader(contextReader{ctx, input})}
	header := &io.LimitedReader{R: r.input, N: maxHeaderBytes}
	var magic [4]byte
	if _, err := io.ReadFull(header, magic[:]); err != nil {
		return nil, fmt.Errorf("Avro header: %w", err)
	}
	if magic != [4]byte{'O', 'b', 'j', 1} {
		return nil, fmt.Errorf("Avro requires an Object Container File")
	}
	metadata := make(map[string][]byte)
	for blocks := 0; ; blocks++ {
		count, err := readLong(header)
		if err != nil {
			return nil, fmt.Errorf("Avro header: %w", err)
		}
		if count == 0 {
			break
		}
		if blocks >= maxFields {
			return nil, fmt.Errorf("Avro metadata exceeds block budget")
		}
		var declared int64 = -1
		if count < 0 {
			if count < -maxFields {
				return nil, fmt.Errorf("Avro metadata exceeds entry budget")
			}
			count = -count
			declared, err = readLong(header)
			if err != nil || declared < 0 || declared > header.N {
				return nil, fmt.Errorf("invalid Avro metadata block size")
			}
		}
		if count > maxFields || count > int64(maxFields-len(metadata)) {
			return nil, fmt.Errorf("Avro metadata exceeds entry budget")
		}
		start := header.N
		for i := int64(0); i < count; i++ {
			key, err := readBytes(header)
			if err != nil {
				return nil, fmt.Errorf("Avro metadata key: %w", err)
			}
			if !utf8.Valid(key) {
				return nil, fmt.Errorf("invalid Avro metadata key")
			}
			value, err := readBytes(header)
			if err != nil {
				return nil, fmt.Errorf("Avro metadata value: %w", err)
			}
			if _, exists := metadata[string(key)]; exists {
				return nil, fmt.Errorf("duplicate Avro metadata key")
			}
			metadata[string(key)] = value
		}
		if declared >= 0 && start-header.N != declared {
			return nil, fmt.Errorf("Avro metadata block size mismatch")
		}
	}
	if _, err := io.ReadFull(header, r.sync[:]); err != nil {
		return nil, fmt.Errorf("Avro header sync: %w", err)
	}
	compression, hasCompression := metadata["avro.codec"]
	r.compression = string(compression)
	if !hasCompression {
		r.compression = "null"
	}
	if r.compression != "null" && r.compression != "deflate" && r.compression != "snappy" {
		return nil, fmt.Errorf("unsupported Avro codec %q", r.compression)
	}
	raw := metadata["avro.schema"]
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid Avro schema text")
	}
	var err error
	r.fields, r.schema, err = recordSchema(raw)
	if err != nil {
		return nil, err
	}
	r.codec, err = goavro.NewCodec(string(raw))
	if err != nil {
		return nil, fmt.Errorf("Avro schema: %w", err)
	}
	r.selected = r.fields
	return r, nil
}

func recordSchema(raw []byte) ([]datatype.FieldInfo, map[string]interface{}, error) {
	var schema map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil {
		return nil, nil, fmt.Errorf("Avro schema: %w", err)
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		return nil, nil, fmt.Errorf("Avro schema has trailing data")
	}
	list, ok := schema["fields"].([]interface{})
	if schema["type"] != "record" || !ok || len(list) == 0 || len(list) > maxFields {
		return nil, nil, fmt.Errorf("Avro requires a flat record with 1 to %d fields", maxFields)
	}
	if _, exists := schema["logicalType"]; exists {
		return nil, nil, fmt.Errorf("Avro logical types are unsupported")
	}
	fields := make([]datatype.FieldInfo, 0, len(list))
	seen := map[string]bool{}
	for i, item := range list {
		field, ok := item.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("invalid Avro field")
		}
		name, _ := field["name"].(string)
		if name == "" || seen[name] {
			return nil, nil, fmt.Errorf("invalid or duplicate Avro field name")
		}
		seen[name] = true
		value := field["type"]
		nullable := false
		if union, ok := value.([]interface{}); ok {
			if len(union) != 2 {
				return nil, nil, fmt.Errorf("Avro only supports nullable primitive unions")
			}
			left := primitiveName(union[0])
			right := primitiveName(union[1])
			if left == "null" {
				value = union[1]
			} else if right == "null" {
				value = union[0]
			} else {
				return nil, nil, fmt.Errorf("Avro only supports nullable primitive unions")
			}
			nullable = true
		}
		if obj, ok := value.(map[string]interface{}); ok {
			if _, exists := obj["logicalType"]; exists {
				return nil, nil, fmt.Errorf("Avro logical types are unsupported")
			}
			value = obj["type"]
		}
		native, _ := value.(string)
		kind := map[string]datatype.FieldType{"boolean": datatype.FieldTypeBool, "int": datatype.FieldTypeInt, "long": datatype.FieldTypeBigInt, "float": datatype.FieldTypeFloat, "double": datatype.FieldTypeDouble, "bytes": datatype.FieldTypeBytes, "string": datatype.FieldTypeString}[native]
		if kind == "" {
			return nil, nil, fmt.Errorf("unsupported Avro field type for %q", name)
		}
		comment, _ := field["doc"].(string)
		fields = append(fields, datatype.FieldInfo{Name: name, Type: kind, NativeType: native, Nullable: nullable, Comment: comment, OrdinalPosition: i + 1})
	}
	return fields, schema, nil
}

func primitiveName(value interface{}) string {
	if object, ok := value.(map[string]interface{}); ok {
		if _, exists := object["logicalType"]; exists {
			return ""
		}
		value = object["type"]
	}
	name, _ := value.(string)
	return name
}

func readLong(input io.Reader) (int64, error) {
	var result uint64
	for i := 0; i < 10; i++ {
		var b [1]byte
		if _, err := io.ReadFull(input, b[:]); err != nil {
			if i > 0 && err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return 0, err
		}
		if i == 9 && b[0] > 1 {
			return 0, fmt.Errorf("Avro integer overflow")
		}
		result |= uint64(b[0]&0x7f) << (7 * i)
		if b[0] < 128 {
			return int64(result>>1) ^ -int64(result&1), nil
		}
	}
	return 0, fmt.Errorf("Avro integer overflow")
}

func readBytes(input *io.LimitedReader) ([]byte, error) {
	n, err := readLong(input)
	if err != nil {
		return nil, err
	}
	if n < 0 || n > input.N {
		return nil, fmt.Errorf("Avro header length exceeds budget")
	}
	data := make([]byte, int(n))
	_, err = io.ReadFull(input, data)
	return data, err
}

func (r *tableReader) loadBlock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	count, err := readLong(r.input)
	if err != nil {
		return err
	}
	if count <= 0 || count > maxBlockRows {
		return fmt.Errorf("Avro block count exceeds supported range")
	}
	size, err := readLong(r.input)
	if err != nil {
		return err
	}
	if size < 0 || size > maxBlockBytes {
		return fmt.Errorf("Avro block size exceeds budget")
	}
	data := make([]byte, int(size))
	if _, err = io.ReadFull(r.input, data); err != nil {
		return err
	}
	switch r.compression {
	case "deflate":
		compressed := bytes.NewReader(data)
		decoder := flate.NewReader(compressed)
		decoded, readErr := io.ReadAll(io.LimitReader(decoder, maxBlockBytes+1))
		closeErr := decoder.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if len(decoded) > maxBlockBytes {
			return fmt.Errorf("Avro decoded block exceeds budget")
		}
		if compressed.Len() != 0 {
			return fmt.Errorf("Avro deflate block has trailing data")
		}
		data = decoded
	case "snappy":
		if len(data) < 4 {
			return fmt.Errorf("Avro Snappy block is missing CRC")
		}
		payload := data[:len(data)-4]
		n, err := snappy.DecodedLen(payload)
		if err != nil {
			return err
		}
		if n > maxBlockBytes {
			return fmt.Errorf("Avro decoded block exceeds budget")
		}
		decoded, err := snappy.Decode(nil, payload)
		if err != nil {
			return err
		}
		if crc32.ChecksumIEEE(decoded) != binary.BigEndian.Uint32(data[len(data)-4:]) {
			return fmt.Errorf("Avro Snappy CRC mismatch")
		}
		data = decoded
	}
	var sync [16]byte
	if _, err = io.ReadFull(r.input, sync[:]); err != nil {
		return err
	}
	if sync != r.sync {
		return fmt.Errorf("Avro block sync mismatch")
	}
	r.remaining = count
	r.block = data
	return nil
}

func (r *tableReader) Fields() []datatype.FieldInfo {
	return append([]datatype.FieldInfo(nil), r.selected...)
}

func (r *tableReader) ReadRows(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.failed != nil {
		return nil, r.failed
	}
	if limit <= 0 {
		return nil, fmt.Errorf("Avro read limit must be positive")
	}
	rows := make([]map[string]interface{}, 0, min(limit, 1024))
	for !r.closed && len(rows) < limit {
		if err := ctx.Err(); err != nil {
			r.failed = err
			return nil, err
		}
		if r.remaining == 0 {
			if err := r.loadBlock(ctx); err != nil {
				if err == io.EOF {
					r.closed = true
					break
				}
				r.failed = fmt.Errorf("Avro block: %w", err)
				return nil, r.failed
			}
		}
		value, rest, err := r.codec.NativeFromBinary(r.block)
		if err != nil {
			r.failed = fmt.Errorf("Avro record: %w", err)
			return nil, r.failed
		}
		// The upstream integer decoder can truncate malformed overlong/range
		// encodings. Require the same codec to reproduce the consumed bytes,
		// preventing silent integer/union/boolean corruption without a fallback.
		consumed := r.block[:len(r.block)-len(rest)]
		canonical, err := r.codec.BinaryFromNative(nil, value)
		if err != nil || !bytes.Equal(canonical, consumed) {
			r.failed = fmt.Errorf("Avro record contains a noncanonical or out-of-range encoding")
			return nil, r.failed
		}
		r.block = rest
		r.remaining--
		if r.remaining == 0 && len(rest) != 0 {
			r.failed = fmt.Errorf("Avro block has trailing data")
			return nil, r.failed
		}
		record, ok := value.(map[string]interface{})
		if !ok {
			r.failed = fmt.Errorf("Avro datum is not a record")
			return nil, r.failed
		}
		row := make(map[string]interface{}, len(r.selected))
		for _, field := range r.fields {
			value := record[field.Name]
			if field.Nullable && value != nil {
				union, ok := value.(map[string]interface{})
				if !ok || len(union) != 1 {
					r.failed = fmt.Errorf("invalid Avro union")
					return nil, r.failed
				}
				value = union[field.NativeType]
			}
			if field.Type == datatype.FieldTypeString && value != nil && !utf8.ValidString(value.(string)) {
				r.failed = fmt.Errorf("invalid Avro UTF-8 string")
				return nil, r.failed
			}
			record[field.Name] = value
		}
		for _, field := range r.selected {
			row[field.Name] = record[field.Name]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *tableReader) Close(context.Context) error { r.closed = true; r.block = nil; return nil }
