package avro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/addp/common/datatype"
)

const maxDecimalPrecision = 1000

func recordNamespace(schema map[string]interface{}) string {
	name, _ := schema["name"].(string)
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		return name[:index]
	}
	namespace, _ := schema["namespace"].(string)
	return namespace
}

func qualifiedName(name, namespace string) string {
	if namespace == "" || strings.Contains(name, ".") {
		return name
	}
	return namespace + "." + name
}

func fieldDefinition(value interface{}, namespace string, named map[string]datatype.FieldInfo) (datatype.FieldInfo, error) {
	var result datatype.FieldInfo
	object, isObject := value.(map[string]interface{})
	if isObject {
		value = object["type"]
	}
	native, _ := value.(string)
	result.NativeType = native
	logical, hasLogical := object["logicalType"]
	if !hasLogical {
		result.Type = map[string]datatype.FieldType{"boolean": datatype.FieldTypeBool, "int": datatype.FieldTypeInt, "long": datatype.FieldTypeBigInt, "float": datatype.FieldTypeFloat, "double": datatype.FieldTypeDouble, "bytes": datatype.FieldTypeBytes, "string": datatype.FieldTypeString}[native]
		if result.Type != "" {
			return result, nil
		}
		if definition, ok := named[qualifiedName(native, namespace)]; ok {
			return definition, nil
		}
		return result, fmt.Errorf("unsupported Avro field type")
	}
	if logical == "date" && native == "int" {
		result.Type = datatype.FieldTypeDate
		return result, nil
	}
	if name, ok := logical.(string); ok && native == "long" {
		if unit := map[string]string{"timestamp-millis": "ms", "timestamp-micros": "us", "timestamp-nanos": "ns"}[name]; unit != "" {
			result.Type = datatype.FieldTypeTimestamp
			result.Temporal = &datatype.TemporalInfo{Unit: unit, Timezone: "UTC"}
			return result, nil
		}
	}
	if logical != "decimal" || (native != "bytes" && native != "fixed") {
		return result, fmt.Errorf("unsupported Avro logical type")
	}
	precision, ok := object["precision"].(json.Number)
	p, err := precision.Int64()
	if !ok || err != nil || p < 1 || p > maxDecimalPrecision {
		return result, fmt.Errorf("Avro decimal precision must be an integer in 1..%d", maxDecimalPrecision)
	}
	var scale int64
	if raw, present := object["scale"]; present {
		number, ok := raw.(json.Number)
		scale, err = number.Int64()
		if !ok || err != nil || scale < 0 || scale > p {
			return result, fmt.Errorf("Avro decimal scale must be an integer in 0..precision")
		}
	}
	result.Type, result.Precision, result.Scale = datatype.FieldTypeDecimal, int(p), int(scale)
	if native == "fixed" {
		size, ok := object["size"].(json.Number)
		n, err := size.Int64()
		if !ok || err != nil || n < 1 || n > maxBlockBytes {
			return result, fmt.Errorf("Avro decimal fixed size exceeds the supported block budget")
		}
		// For larger fixed widths every supported precision already fits. Keep
		// schema validation bounded rather than constructing a block-sized integer.
		if n < maxDecimalPrecision {
			capacity := new(big.Int).Lsh(big.NewInt(1), uint(n*8-1))
			capacity.Sub(capacity, big.NewInt(1))
			if int(p) > len(capacity.String())-1 {
				return result, fmt.Errorf("Avro decimal precision exceeds fixed capacity")
			}
		}
		name, _ := object["name"].(string)
		if name == "" {
			return result, fmt.Errorf("Avro decimal fixed requires a name")
		}
		if declared, present := object["namespace"]; present {
			var ok bool
			namespace, ok = declared.(string)
			if !ok {
				return result, fmt.Errorf("invalid Avro fixed namespace")
			}
		}
		result.NativeType = qualifiedName(name, namespace)
		if _, duplicate := named[result.NativeType]; duplicate {
			return result, fmt.Errorf("duplicate Avro fixed declaration")
		}
		named[result.NativeType] = result
	}
	return result, nil
}

// Logical types have exactly the encoding of their underlying Avro type. Use
// the existing codec once, on a copy of the physical schema, retaining the
// untouched source declaration for metadata. This preserves legal redundant
// decimal sign bytes through the reader's canonical framing check.
func physicalRecordSchema(schema map[string]interface{}) (string, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	var physical map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&physical); err != nil {
		return "", err
	}
	for _, item := range physical["fields"].([]interface{}) {
		field := item.(map[string]interface{})
		values := []interface{}{field["type"]}
		if union, ok := field["type"].([]interface{}); ok {
			values = union
		}
		for _, value := range values {
			if object, ok := value.(map[string]interface{}); ok && object["logicalType"] != nil {
				delete(object, "logicalType")
				delete(object, "precision")
				delete(object, "scale")
			}
		}
	}
	raw, err = json.Marshal(physical)
	return string(raw), err
}

func logicalFieldValue(value interface{}, field datatype.FieldInfo) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	switch field.Type {
	case datatype.FieldTypeDate:
		days := value.(int32)
		// Calendar days are not a time.Duration (which would overflow for valid
		// historical dates), and do not acquire a local timezone.
		date := time.Unix(int64(days)*86400, 0).UTC()
		if date.Year() < 1 || date.Year() > 9999 {
			return nil, fmt.Errorf("Avro date is outside years 0001..9999")
		}
		return date.Format(time.DateOnly), nil
	case datatype.FieldTypeDecimal:
		data := value.([]byte)
		if len(data) == 0 {
			return nil, fmt.Errorf("Avro decimal requires a signed integer byte sequence")
		}
		for len(data) > 1 && ((data[0] == 0 && data[1]&0x80 == 0) || (data[0] == 255 && data[1]&0x80 != 0)) {
			data = data[1:]
		}
		maxValue := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(field.Precision)), nil)
		if len(data) > (maxValue.BitLen()+1+7)/8 {
			return nil, fmt.Errorf("Avro decimal value exceeds precision")
		}
		integer := new(big.Int).SetBytes(data)
		negative := data[0]&0x80 != 0
		if negative {
			integer.Sub(integer, new(big.Int).Lsh(big.NewInt(1), uint(len(data)*8)))
			integer.Neg(integer)
		}
		digits := integer.String()
		if len(digits) > field.Precision {
			return nil, fmt.Errorf("Avro decimal value exceeds precision")
		}
		if field.Scale > 0 {
			if len(digits) <= field.Scale {
				digits = strings.Repeat("0", field.Scale-len(digits)+1) + digits
			}
			point := len(digits) - field.Scale
			digits = digits[:point] + "." + digits[point:]
		}
		if negative {
			digits = "-" + digits
		}
		return digits, nil
	default:
		return value, nil
	}
}
