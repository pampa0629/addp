package avro

import (
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/addp/common/datatype"
)

func wireLong(raw []byte) (int64, []byte, error) {
	var value uint64
	for i := 0; i < 10; i++ {
		if i >= len(raw) {
			return 0, nil, fmt.Errorf("truncated Avro integer")
		}
		b := raw[i]
		if i == 9 && b > 1 {
			return 0, nil, fmt.Errorf("Avro integer overflow")
		}
		value |= uint64(b&127) << uint(7*i)
		if b < 128 {
			if i > 0 && b == 0 {
				return 0, nil, fmt.Errorf("noncanonical Avro integer")
			}
			return int64(value>>1) ^ -int64(value&1), raw[i+1:], nil
		}
	}
	return 0, nil, fmt.Errorf("Avro integer overflow")
}

// Validate source framing before the codec allocates arrays/maps. Unlike whole-datum
// re-encoding, this accepts legal collection block representations and verifies
// the negative-block byte length that the fixed upstream codec discards.
func (n *valueSchema) scan(raw []byte, visits *int, structured bool) ([]byte, error) {
	if structured {
		*visits++
		if *visits > maxStructureValues {
			return nil, fmt.Errorf("Avro structure exceeds value budget")
		}
	}
	if n.nullBranch >= 0 {
		branch, rest, err := wireLong(raw)
		if err != nil {
			return nil, err
		}
		raw = rest
		if branch < 0 || branch > 1 {
			return nil, fmt.Errorf("invalid Avro union branch")
		}
		if branch == int64(n.nullBranch) {
			return raw, nil
		}
	}
	if n.field.Enum != nil {
		// Framing enforces the canonical integer representation. The fixed codec
		// alone checks symbol membership when decoding this index.
		_, rest, err := wireLong(raw)
		return rest, err
	}
	if n.fixedSize > 0 {
		if len(raw) < n.fixedSize {
			return nil, fmt.Errorf("truncated Avro fixed")
		}
		return raw[n.fixedSize:], nil
	}
	switch n.field.Type {
	case datatype.FieldTypeJSON:
		if n.field.MapValue != nil {
			return n.scanCollection(raw, visits)
		}
		for _, child := range n.children {
			before := len(raw)
			complex := child.field.Type == datatype.FieldTypeJSON || child.field.Type == datatype.FieldTypeArray
			rest, err := child.scan(raw, visits, structured || complex)
			if err != nil {
				return nil, err
			}
			if complex && !structured && before-len(rest) > maxStructureJSON {
				return nil, fmt.Errorf("Avro structure exceeds encoded value budget")
			}
			raw = rest
		}
		return raw, nil
	case datatype.FieldTypeArray:
		return n.scanCollection(raw, visits)
	case datatype.FieldTypeBool:
		if len(raw) == 0 || raw[0] > 1 {
			return nil, fmt.Errorf("invalid Avro boolean")
		}
		return raw[1:], nil
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeDate, datatype.FieldTypeTimestamp:
		value, rest, err := wireLong(raw)
		if err != nil {
			return nil, err
		}
		if (n.field.NativeType == "int") && (value < math.MinInt32 || value > math.MaxInt32) {
			return nil, fmt.Errorf("Avro int overflow")
		}
		return rest, nil
	case datatype.FieldTypeFloat:
		if len(raw) < 4 {
			return nil, fmt.Errorf("truncated Avro float")
		}
		return raw[4:], nil
	case datatype.FieldTypeDouble:
		if len(raw) < 8 {
			return nil, fmt.Errorf("truncated Avro double")
		}
		return raw[8:], nil
	case datatype.FieldTypeBytes, datatype.FieldTypeString, datatype.FieldTypeDecimal:
		size, rest, err := wireLong(raw)
		if err != nil {
			return nil, err
		}
		if size < 0 || size > int64(len(rest)) {
			return nil, fmt.Errorf("invalid Avro byte length")
		}
		if n.field.Type == datatype.FieldTypeString && !utf8.Valid(rest[:int(size)]) {
			return nil, fmt.Errorf("invalid Avro UTF-8 string")
		}
		return rest[int(size):], nil
	}
	return nil, fmt.Errorf("unsupported Avro wire type")
}

// scanCollection validates array/map block framing before codec allocation.
func (n *valueSchema) scanCollection(raw []byte, visits *int) ([]byte, error) {
	for {
		count, rest, err := wireLong(raw)
		if err != nil {
			return nil, err
		}
		raw = rest
		if count == 0 {
			return raw, nil
		}
		end := -1
		if count < 0 {
			if count == math.MinInt64 {
				return nil, fmt.Errorf("Avro collection count exceeds budget")
			}
			count = -count
			size, rest, err := wireLong(raw)
			if err != nil {
				return nil, err
			}
			if size < 0 || size > int64(len(rest)) {
				return nil, fmt.Errorf("invalid Avro collection block size")
			}
			raw = rest
			end = len(raw) - int(size)
		}
		remaining := maxStructureValues - *visits
		if n.field.MapValue != nil {
			remaining /= 2 // Each map entry visits both a key and a value.
		}
		if count > int64(remaining) {
			return nil, fmt.Errorf("Avro collection count exceeds value budget")
		}
		for i := int64(0); i < count; i++ {
			if n.field.MapValue != nil {
				key := valueSchema{field: datatype.FieldInfo{Type: datatype.FieldTypeString}, nullBranch: -1}
				raw, err = key.scan(raw, visits, true)
				if err != nil {
					return nil, err
				}
			}
			raw, err = n.item.scan(raw, visits, true)
			if err != nil {
				return nil, err
			}
		}
		if end >= 0 && len(raw) != end {
			return nil, fmt.Errorf("Avro collection block size mismatch")
		}
	}
}
