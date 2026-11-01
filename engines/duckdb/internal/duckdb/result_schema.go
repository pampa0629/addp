package duckdb

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/addp/common/datatype"
	commoni18n "github.com/addp/common/middleware/i18n"
	duckdriver "github.com/duckdb/duckdb-go/v2"
)

// DuckDB's driver reports complete native output type declarations, including
// decimal width/scale and recursively quoted STRUCT/LIST members. Do not infer
// output fields from source columns, aliases, ScanType or sampled Go values.
func outputField(column *sql.ColumnType, ordinal int) (datatype.FieldInfo, error) {
	parser := nativeOutputType{rest: column.DatabaseTypeName()}
	field, err := parser.parse(0)
	if err != nil || strings.TrimSpace(parser.rest) != "" {
		return datatype.FieldInfo{}, commoni18n.NewMessageError("engine.query_runtime.output_type_unsupported")
	}
	field.Name, field.OrdinalPosition = column.Name(), ordinal
	field.Nullable = true
	if nullable, known := column.Nullable(); known {
		field.Nullable = nullable
	}
	return field, nil
}

type nativeOutputType struct{ rest string }

func (p *nativeOutputType) consume(s string) bool {
	p.rest = strings.TrimLeftFunc(p.rest, unicode.IsSpace)
	if !strings.HasPrefix(p.rest, s) {
		return false
	}
	p.rest = strings.TrimPrefix(p.rest, s)
	return true
}

func (p *nativeOutputType) parse(depth int) (datatype.FieldInfo, error) {
	fail := func() (datatype.FieldInfo, error) {
		return datatype.FieldInfo{}, fmt.Errorf("unsupported native output type")
	}
	if depth > 16 || len(p.rest) > 65536 {
		return fail()
	}
	p.rest = strings.TrimLeftFunc(p.rest, unicode.IsSpace)
	original := p.rest
	end := strings.IndexAny(p.rest, "(,)[")
	if end < 0 {
		end = len(p.rest)
	}
	name := strings.TrimSpace(p.rest[:end])
	p.rest = p.rest[end:]
	field := datatype.FieldInfo{Nullable: true}
	switch name {
	case "SQLNULL", "NULL":
		field.Type = datatype.FieldTypeUnknown
	case "BOOLEAN":
		field.Type = datatype.FieldTypeBool
	case "TINYINT", "SMALLINT", "INTEGER", "UTINYINT", "USMALLINT":
		field.Type = datatype.FieldTypeInt
	case "BIGINT", "UBIGINT", "UINTEGER":
		field.Type = datatype.FieldTypeBigInt
	case "HUGEINT", "UHUGEINT":
		field.Type, field.Precision = datatype.FieldTypeDecimal, 39
	case "DECIMAL":
		if !p.consume("(") {
			return fail()
		}
		end := strings.IndexByte(p.rest, ')')
		if end < 0 {
			return fail()
		}
		parts := strings.Split(p.rest[:end], ",")
		if len(parts) != 2 {
			return fail()
		}
		precision, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || precision < 1 || precision > 38 {
			return fail()
		}
		scale, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || scale < 0 || scale > precision {
			return fail()
		}
		field.Type, field.Precision, field.Scale = datatype.FieldTypeDecimal, precision, scale
		p.rest = p.rest[end+1:]
	case "FLOAT":
		field.Type = datatype.FieldTypeFloat
	case "DOUBLE":
		field.Type = datatype.FieldTypeDouble
	case "VARCHAR", "ENUM":
		field.Type = datatype.FieldTypeString
	case "BLOB":
		field.Type = datatype.FieldTypeBytes
	case "DATE":
		field.Type = datatype.FieldTypeDate
	case "TIME", "TIME WITH TIME ZONE":
		field.Type = datatype.FieldTypeTime
	case "TIMESTAMP", "TIMESTAMP_S", "TIMESTAMP_MS", "TIMESTAMP_NS", "TIMESTAMP WITH TIME ZONE":
		field.Type = datatype.FieldTypeTimestamp
	case "UUID":
		field.Type = datatype.FieldTypeUUID
	case "JSON":
		field.Type = datatype.FieldTypeJSON
	case "STRUCT":
		field.Type = datatype.FieldTypeJSON
		if !p.consume("(") {
			return fail()
		}
		seen := map[string]bool{}
		for {
			// The pinned driver always quotes names and doubles embedded quotes.
			if !p.consume(`"`) {
				return fail()
			}
			var member strings.Builder
			for {
				quote := strings.IndexByte(p.rest, '"')
				if quote < 0 {
					return fail()
				}
				member.WriteString(p.rest[:quote])
				p.rest = p.rest[quote+1:]
				if !strings.HasPrefix(p.rest, `"`) {
					break
				}
				member.WriteByte('"')
				p.rest = p.rest[1:]
			}
			if seen[member.String()] {
				return fail()
			}
			seen[member.String()] = true
			child, err := p.parse(depth + 1)
			if err != nil {
				return fail()
			}
			child.Name, child.OrdinalPosition = member.String(), len(field.Fields)+1
			field.Fields = append(field.Fields, child)
			if p.consume(")") {
				break
			}
			if !p.consume(",") {
				return fail()
			}
		}
	default:
		return fail()
	}
	field.NativeType = strings.TrimSpace(original[:len(original)-len(p.rest)])
	for p.consume("[") {
		end := strings.IndexByte(p.rest, ']')
		if end < 0 {
			return fail()
		}
		if size := strings.TrimSpace(p.rest[:end]); size != "" {
			if n, err := strconv.ParseUint(size, 10, 64); err != nil || n == 0 {
				return fail()
			}
		}
		p.rest = p.rest[end+1:]
		child := field
		field = datatype.FieldInfo{Type: datatype.FieldTypeArray, ElementType: child.Type,
			Element: &child, Nullable: true, NativeType: strings.TrimSpace(original[:len(original)-len(p.rest)])}
	}
	return field, nil
}

// Reduce driver-specific wrappers to exact native values at this owner boundary.
// The shared HTTP encoder, not the internal Provider, chooses public strings.
func outputValue(value any) any {
	switch value := value.(type) {
	case duckdriver.Decimal:
		return exactDecimal{value}
	case duckdriver.UUID:
		return value.String()
	case []any:
		result := make([]any, len(value))
		for i, item := range value {
			result[i] = outputValue(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(value))
		for name, item := range value {
			result[name] = outputValue(item)
		}
		return result
	default:
		return value
	}
}

type exactDecimal struct{ duckdriver.Decimal }

func (d exactDecimal) MarshalText() ([]byte, error) { return []byte(d.Decimal.String()), nil }
