package oracle

import (
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

type TypeMapper struct{}

func (m *TypeMapper) Name() string {
	return "oracle"
}

func (m *TypeMapper) ToCommon(nativeType string) datatype.FieldType {
	normalized := strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(nativeType)), " "))
	base, precision, scale, hasPrecision := parseParameterizedType(normalized)

	switch base {
	case "CHAR", "VARCHAR2", "NCHAR", "NVARCHAR2", "CLOB", "NCLOB", "LONG", "ROWID", "UROWID":
		return datatype.FieldTypeString
	case "NUMBER", "DECIMAL", "NUMERIC":
		if hasPrecision && scale == 0 {
			switch {
			case precision <= 9:
				return datatype.FieldTypeInt
			case precision <= 18:
				return datatype.FieldTypeBigInt
			}
		}
		return datatype.FieldTypeDecimal
	case "INTEGER", "INT", "SMALLINT":
		return datatype.FieldTypeBigInt
	case "BINARY_FLOAT":
		return datatype.FieldTypeFloat
	case "BINARY_DOUBLE", "FLOAT", "DOUBLE PRECISION", "REAL":
		return datatype.FieldTypeDouble
	case "BOOLEAN":
		return datatype.FieldTypeBool
	case "DATE":
		return datatype.FieldTypeTimestamp
	case "TIMESTAMP", "TIMESTAMP WITH TIME ZONE", "TIMESTAMP WITH LOCAL TIME ZONE":
		return datatype.FieldTypeTimestamp
	case "RAW", "LONG RAW", "BLOB":
		return datatype.FieldTypeBytes
	case "JSON":
		return datatype.FieldTypeJSON
	case "MDSYS.SDO_GEOMETRY", "SDO_GEOMETRY":
		return datatype.FieldTypeGeometry
	default:
		return datatype.FieldTypeUnknown
	}
}

func (m *TypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int) {
	switch commonType {
	case datatype.FieldTypeString:
		return "VARCHAR2", 4000, 0
	case datatype.FieldTypeInt:
		return "NUMBER", 9, 0
	case datatype.FieldTypeBigInt:
		return "NUMBER", 18, 0
	case datatype.FieldTypeFloat:
		return "BINARY_FLOAT", 0, 0
	case datatype.FieldTypeDouble:
		return "BINARY_DOUBLE", 0, 0
	case datatype.FieldTypeDecimal:
		return "NUMBER", 38, 10
	case datatype.FieldTypeBool:
		return "BOOLEAN", 0, 0
	case datatype.FieldTypeDate, datatype.FieldTypeTimestamp:
		return "TIMESTAMP", 0, 0
	case datatype.FieldTypeBytes:
		return "BLOB", 0, 0
	case datatype.FieldTypeJSON:
		return "JSON", 0, 0
	case datatype.FieldTypeUUID:
		return "VARCHAR2", 36, 0
	default:
		return "", 0, 0
	}
}

func parseParameterizedType(nativeType string) (string, int, int, bool) {
	open := strings.IndexByte(nativeType, '(')
	if open < 0 {
		return normalizeOracleTypeBase(nativeType), 0, 0, false
	}
	close := strings.IndexByte(nativeType[open+1:], ')')
	if close < 0 {
		return normalizeOracleTypeBase(nativeType), 0, 0, false
	}
	close += open + 1
	base := normalizeOracleTypeBase(strings.TrimSpace(nativeType[:open]) + " " + strings.TrimSpace(nativeType[close+1:]))
	parts := strings.Split(nativeType[open+1:close], ",")
	precision, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return base, 0, 0, false
	}
	scale := 0
	if len(parts) > 1 {
		scale, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return base, 0, 0, false
		}
	}
	return base, precision, scale, true
}

func normalizeOracleTypeBase(value string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}

func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
