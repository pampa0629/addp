package plan

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/datatype"
)

var symbolPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func Symbol(s string) bool { return symbolPattern.MatchString(s) }
func supportedType(t datatype.FieldType) bool {
	switch t {
	case datatype.FieldTypeString, datatype.FieldTypeBool, datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeDecimal, datatype.FieldTypeDate:
		return true
	}
	return false
}
func (v Literal) Canonical() (Literal, error) {
	if !supportedType(v.Type) || len(v.Text) > MaxBytes || !utf8.ValidString(v.Text) {
		return v, fmt.Errorf("invalid literal type or encoding")
	}
	if v.Null {
		if v.Text != "" {
			return v, fmt.Errorf("null has a value")
		}
		return v, nil
	}
	switch v.Type {
	case datatype.FieldTypeBool:
		if v.Text != "true" && v.Text != "false" {
			return v, fmt.Errorf("invalid boolean")
		}
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt:
		bits := 64
		if v.Type == datatype.FieldTypeInt {
			bits = 32
		}
		n, err := strconv.ParseInt(v.Text, 10, bits)
		if err != nil {
			return v, fmt.Errorf("invalid integer")
		}
		v.Text = strconv.FormatInt(n, 10)
	case datatype.FieldTypeDecimal:
		if !decimalPattern.MatchString(v.Text) {
			return v, fmt.Errorf("invalid decimal")
		}
		parts := strings.Split(strings.TrimPrefix(v.Text, "-"), ".")
		if len(parts[0]) > 20 || (len(parts) > 1 && len(parts[1]) > 18) {
			return v, fmt.Errorf("decimal exceeds precision")
		}
		if len(parts) > 1 {
			v.Text = strings.TrimRight(strings.TrimRight(v.Text, "0"), ".")
		}
		if v.Text == "-0" {
			v.Text = "0"
		}
	case datatype.FieldTypeDate:
		t, err := time.Parse("2006-01-02", v.Text)
		if err != nil || t.Format("2006-01-02") != v.Text || t.Year() < 1 {
			return v, fmt.Errorf("invalid date")
		}
	}
	return v, nil
}
func field(name string, t datatype.FieldType, nullable bool) datatype.FieldInfo {
	f := datatype.FieldInfo{Name: name, Type: t, Nullable: nullable}
	if t == datatype.FieldTypeDecimal {
		f.Precision = 38
		f.Scale = 18
	}
	return f
}
func validateFields(fields []datatype.FieldInfo) error {
	if len(fields) == 0 || len(fields) > 512 {
		return fmt.Errorf("invalid field count")
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !Symbol(f.Name) || seen[f.Name] || !supportedType(f.Type) {
			return fmt.Errorf("invalid or duplicate field")
		}
		seen[f.Name] = true
		// Shared FieldInfo is reused, but native/default/generated expressions are not part of a logical plan.
		if !reflect.DeepEqual(f, field(f.Name, f.Type, f.Nullable)) {
			return fmt.Errorf("unexpected field metadata")
		}
	}
	return nil
}
