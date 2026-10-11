package postgresql

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/datatype"
	"github.com/lib/pq"
)

// The current array mapping is TEXT[], not an arbitrary PostgreSQL array.
func validatePostgresArrayFields(fields []datatype.FieldInfo) error {
	for _, field := range postgresWriteFields(fields) {
		if datatype.ParseFieldType(string(field.Type)) == datatype.FieldTypeArray && field.ElementType != "" && field.ElementType != datatype.FieldTypeString {
			return fmt.Errorf("postgresql write column %q: TEXT[] requires string elements; declared element_type %q is unsupported", field.Name, field.ElementType)
		}
	}
	return nil
}

func postgresArrayWriteValue(value interface{}) (interface{}, error) {
	if value == nil {
		return nil, nil
	}
	// PostgreSQL reads return native array text. Parse it before binding, rather
	// than treating a JSON document or a malformed literal as a native array.
	switch v := value.(type) {
	case string:
		return postgresArrayTextWriteValue(v)
	case []byte:
		if v == nil {
			return nil, nil
		}
		return postgresArrayTextWriteValue(string(v))
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice {
		return nil, fmt.Errorf("TEXT[] requires a one-dimensional string array, got %T", value)
	}
	if rv.Kind() == reflect.Slice && rv.IsNil() {
		return nil, nil
	}
	elements := make([]interface{}, rv.Len())
	for i := range elements {
		elements[i] = rv.Index(i).Interface()
	}
	if err := validatePostgresTextArrayElements(elements); err != nil {
		return nil, err
	}
	// Encode native Go containers before the driver sees them, including COPY.
	return pq.Array(elements).Value()
}

func postgresArrayTextWriteValue(text string) (interface{}, error) {
	var parsed []sql.NullString
	if err := pq.Array(&parsed).Scan(text); err != nil {
		return nil, fmt.Errorf("TEXT[] requires valid one-dimensional PostgreSQL array text without dimension prefixes")
	}
	elements := make([]interface{}, len(parsed))
	for i, element := range parsed {
		if element.Valid {
			elements[i] = element.String
		}
	}
	if err := validatePostgresTextArrayElements(elements); err != nil {
		return nil, err
	}
	// Keep already encoded PostgreSQL text intact: its parser owns unquoted
	// whitespace, backslash escapes and case-insensitive NULL tokens. Re-encoding
	// pq's scanned strings would change those valid literal semantics.
	return text, nil
}

func validatePostgresTextArrayElements(elements []interface{}) error {
	for i, element := range elements {
		if element == nil {
			continue
		}
		text, ok := element.(string)
		if !ok {
			return fmt.Errorf("TEXT[] element %d requires string or NULL, got %T", i, element)
		}
		if !utf8.ValidString(text) || strings.ContainsRune(text, '\x00') {
			return fmt.Errorf("TEXT[] element %d requires UTF-8 text without NUL", i)
		}
	}
	return nil
}
