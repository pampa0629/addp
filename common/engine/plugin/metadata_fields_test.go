package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestFieldInfosFromColumnsPreservesNativeType(t *testing.T) {
	fields := FieldInfosFromColumns([]ColumnInfo{{
		ColumnName:   "id",
		DataType:     "int4",
		IsNullable:   false,
		IsPrimaryKey: true,
		Comment:      "identifier",
	}})

	if len(fields) != 1 {
		t.Fatalf("fields = %#v, want one field", fields)
	}
	field := fields[0]
	if field.Name != "id" || field.Type != datatype.FieldTypeUnknown || field.NativeType != "int4" {
		t.Fatalf("field identity = %#v", field)
	}
	if !field.PrimaryKey || field.Nullable || field.Comment != "identifier" {
		t.Fatalf("field facts = %#v", field)
	}
}

func TestColumnInfosFromFieldsPrefersNativeType(t *testing.T) {
	columns := ColumnInfosFromFields([]datatype.FieldInfo{{
		Name:       "id",
		Type:       datatype.FieldTypeInt,
		NativeType: "int4",
		Nullable:   false,
		PrimaryKey: true,
		Comment:    "identifier",
	}})

	if len(columns) != 1 {
		t.Fatalf("columns = %#v, want one column", columns)
	}
	column := columns[0]
	if column.ColumnName != "id" || column.DataType != "int4" {
		t.Fatalf("column identity = %#v", column)
	}
	if !column.IsPrimaryKey || column.IsNullable || column.Comment != "identifier" {
		t.Fatalf("column facts = %#v", column)
	}
}
