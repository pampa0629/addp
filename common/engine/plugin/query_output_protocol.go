package plugin

import (
	"slices"

	"github.com/addp/common/datatype"
)

// SQLQueryOutputInvalid identifies one compiler-generated, private validity
// column. It is never a business field or a protection decision.
type SQLQueryOutputInvalid struct {
	Flag, Column string
	Reject       bool
}

// FinalizeSQLQueryOutput consumes a Provider's closed result protocol. Flag
// decoding is native to each driver; all shape checking and missing-field
// semantics have one implementation. Validation precedes every mutation.
// decodeFlag(field, nil) validates metadata for an empty result. A missing or
// nil row flag is always rejected before decoding.
func FinalizeSQLQueryOutput(result *QueryResult, columns, omitted []string, invalid []SQLQueryOutputInvalid, decodeFlag func(datatype.FieldInfo, any) (bool, bool)) error {
	if result == nil || decodeFlag == nil {
		return ErrQueryOutputTransformUnsupported
	}
	expected := slices.Clone(columns)
	for _, field := range invalid {
		expected = append(expected, field.Flag)
	}
	if !slices.Equal(result.Columns, expected) || len(result.Fields) != len(expected) {
		return ErrQueryOutputTransformUnsupported
	}
	for index, field := range result.Fields {
		if field.Name != expected[index] {
			return ErrQueryOutputTransformUnsupported
		}
		if index >= len(columns) {
			if _, ok := decodeFlag(field, nil); !ok {
				return ErrQueryOutputTransformUnsupported
			}
		}
	}
	invalidRows := make([][]bool, len(result.Rows))
	for rowIndex, row := range result.Rows {
		invalidRows[rowIndex] = make([]bool, len(invalid))
		for index, field := range invalid {
			raw, exists := row[field.Flag]
			if !exists || raw == nil { // nil is reserved for metadata-only validation.
				return ErrQueryOutputTransformUnsupported
			}
			value, ok := decodeFlag(result.Fields[len(columns)+index], raw)
			if !ok || value && field.Reject {
				return ErrQueryOutputTransformUnsupported
			}
			invalidRows[rowIndex][index] = value
		}
	}
	for rowIndex, row := range result.Rows {
		for index, field := range invalid {
			if invalidRows[rowIndex][index] {
				delete(row, field.Column)
			}
			delete(row, field.Flag)
		}
		for _, column := range omitted {
			delete(row, column)
		}
	}
	result.Columns = slices.DeleteFunc(slices.Clone(columns), func(column string) bool { return slices.Contains(omitted, column) })
	result.Fields = slices.DeleteFunc(slices.Clone(result.Fields[:len(columns)]), func(field datatype.FieldInfo) bool { return slices.Contains(omitted, field.Name) })
	return nil
}
