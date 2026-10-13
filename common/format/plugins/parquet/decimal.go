package parquet

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	parquetgo "github.com/parquet-go/parquet-go"
)

const parquetMaxDecimalPrecision = 1000

var decimalWriterPattern = regexp.MustCompile(`^[+-]?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

func validateParquetDecimalField(field datatype.FieldInfo) error {
	if field.Precision < 1 || field.Precision > parquetMaxDecimalPrecision || field.Scale < 0 || field.Scale > field.Precision {
		return fmt.Errorf("parquet decimal field %q requires explicit precision 1..%d and scale 0..precision", field.Name, parquetMaxDecimalPrecision)
	}
	return nil
}

func parquetDecimalPhysicalType(precision int) parquetgo.Type {
	if precision <= 9 {
		return parquetgo.Int32Type
	}
	if precision <= 18 {
		return parquetgo.Int64Type
	}
	// Include the sign bit in the minimum width for every value of this precision.
	bits := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil).BitLen() + 1
	return parquetgo.FixedLenByteArrayType((bits + 7) / 8)
}

func parquetDecimalWriterValue(value interface{}, field datatype.FieldInfo) (any, error) {
	if value == nil {
		if !field.Nullable {
			return nil, fmt.Errorf("null is not allowed for a required decimal field")
		}
		return nil, nil
	}
	var text string
	switch typed := value.(type) {
	case string:
		text = strings.TrimSpace(typed)
	case json.Number:
		text = typed.String()
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		text = fmt.Sprint(typed)
	default:
		return nil, fmt.Errorf("decimal requires exact text, JSON number or integer, got %T", value)
	}
	unscaled, err := decimalUnscaledInteger(text, field.Precision, field.Scale)
	if err != nil {
		return nil, err
	}
	physical := parquetDecimalPhysicalType(field.Precision)
	switch physical.Kind() {
	case parquetgo.Int32:
		return int32(unscaled.Int64()), nil
	case parquetgo.Int64:
		return unscaled.Int64(), nil
	default:
		width := physical.Length()
		if unscaled.Sign() < 0 {
			unscaled.Add(unscaled, new(big.Int).Lsh(big.NewInt(1), uint(width*8)))
		}
		return unscaled.FillBytes(make([]byte, width)), nil
	}
}

func decimalUnscaledInteger(text string, precision, scale int) (*big.Int, error) {
	if !decimalWriterPattern.MatchString(text) {
		return nil, fmt.Errorf("invalid decimal text")
	}
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimLeft(text, "+-")
	mantissa, exponentText := text, "0"
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		mantissa, exponentText = text[:index], text[index+1:]
	}
	integer, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return new(big.Int), nil
	}
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("decimal exponent cannot fit the declared precision and scale")
	}
	offset := int64(scale) - int64(len(fraction))
	if (exponent > 0 && offset > math.MaxInt64-exponent) || (exponent < 0 && offset < math.MinInt64-exponent) {
		return nil, fmt.Errorf("decimal exponent cannot fit the declared precision and scale")
	}
	shift := exponent + offset
	if shift < 0 {
		if shift < -int64(len(digits)) || strings.Trim(digits[len(digits)+int(shift):], "0") != "" {
			return nil, fmt.Errorf("decimal cannot be represented exactly at scale %d", scale)
		}
		digits = digits[:len(digits)+int(shift)]
	} else {
		if shift > int64(precision)-int64(len(digits)) {
			return nil, fmt.Errorf("decimal exceeds precision %d", precision)
		}
		digits += strings.Repeat("0", int(shift))
	}
	if len(digits) > precision {
		return nil, fmt.Errorf("decimal exceeds precision %d", precision)
	}
	integerValue, _ := new(big.Int).SetString(digits, 10)
	if negative {
		integerValue.Neg(integerValue)
	}
	return integerValue, nil
}
