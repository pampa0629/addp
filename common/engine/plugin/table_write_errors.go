package plugin

import (
	"errors"

	"github.com/addp/common/datatype"
)

type TableWriteErrorCode string

const (
	TableWriteJSONInvalid  TableWriteErrorCode = "json_invalid"
	TableWriteArrayInvalid TableWriteErrorCode = "array_invalid"
	TableWriteBytesInvalid TableWriteErrorCode = "bytes_invalid"
	TableWriteFailed       TableWriteErrorCode = "table_write_failed"
)

func (code TableWriteErrorCode) Valid() bool {
	switch code {
	case TableWriteJSONInvalid, TableWriteArrayInvalid, TableWriteBytesInvalid, TableWriteFailed:
		return true
	default:
		return false
	}
}

// TableWriteField identifies an actual write column, never a source row or a
// physical table ordinal. It intentionally carries no identifiers or values.
type TableWriteField struct {
	ColumnIndex  int                `json:"column_index"`
	DeclaredType datatype.FieldType `json:"declared_type"`
}

func (f *TableWriteField) Valid(code TableWriteErrorCode) bool {
	if f == nil || f.ColumnIndex < 1 || f.ColumnIndex > 65535 {
		return false
	}
	return code == TableWriteJSONInvalid && f.DeclaredType == datatype.FieldTypeJSON ||
		code == TableWriteArrayInvalid && f.DeclaredType == datatype.FieldTypeArray ||
		code == TableWriteBytesInvalid && f.DeclaredType == datatype.FieldTypeBytes
}

// TableWriteError preserves private driver/encoder evidence through wrapping.
// Only the controlled code and validated Field may reach an observation.
type TableWriteError struct {
	Code  TableWriteErrorCode
	Field *TableWriteField
	Err   error
}

func (e *TableWriteError) Error() string {
	if e.Err == nil {
		return string(e.Code)
	}
	return e.Err.Error()
}
func (e *TableWriteError) Unwrap() error { return e.Err }

func NewTableWriteError(code TableWriteErrorCode, err error) error {
	if err == nil {
		return nil
	}
	// Preserve an already classified inner error and its field.
	var typed *TableWriteError
	if errors.As(err, &typed) && typed != nil {
		return err
	}
	if !code.Valid() {
		code = TableWriteFailed
	}
	return &TableWriteError{Code: code, Err: err}
}

func WithTableWriteField(err error, index int, fieldType datatype.FieldType) error {
	if err == nil {
		return nil
	}
	var typed *TableWriteError
	field := &TableWriteField{ColumnIndex: index, DeclaredType: fieldType}
	if !errors.As(err, &typed) || typed == nil || !field.Valid(typed.Code) {
		return err
	}
	return &TableWriteError{Code: typed.Code, Field: field, Err: err}
}
