package clickhouse

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestClickHouseIsSystemSchema(t *testing.T) {
	plugin := &ClickHousePlugin{}

	for _, name := range []string{"system", "information_schema", "INFORMATION_SCHEMA"} {
		if !plugin.isSystemSchema(name) {
			t.Fatalf("isSystemSchema(%q) = false, want true", name)
		}
	}

	if plugin.isSystemSchema("analytics") {
		t.Fatal("isSystemSchema(\"analytics\") = true, want false")
	}
}

func TestClickHouseFieldInfoMapsDefaultExpression(t *testing.T) {
	field := clickhouseFieldInfo(clickhouseColumnRow{
		Name:              "status",
		NativeType:        "String",
		Nullable:          true,
		Comment:           "order status",
		DefaultKind:       "DEFAULT",
		DefaultExpression: "'new'",
	})

	if field.Name != "status" || field.Type != datatype.FieldTypeString || field.NativeType != "String" {
		t.Fatalf("field identity = %#v", field)
	}
	if !field.Nullable || field.Comment != "order status" {
		t.Fatalf("field nullable/comment = %#v", field)
	}
	if field.DefaultExpression != "'new'" || field.Generated || field.GenerationExpression != "" {
		t.Fatalf("field default/generation = %#v", field)
	}
	if field.PrimaryKey {
		t.Fatalf("ClickHouse field should not map native key metadata to ADDP primary_key: %#v", field)
	}
}

func TestClickHouseFieldInfoMapsGeneratedExpression(t *testing.T) {
	tests := []string{"MATERIALIZED", "ALIAS"}
	for _, defaultKind := range tests {
		t.Run(defaultKind, func(t *testing.T) {
			field := clickhouseFieldInfo(clickhouseColumnRow{
				Name:              "total",
				NativeType:        "Int64",
				DefaultKind:       defaultKind,
				DefaultExpression: "price * quantity",
			})

			if !field.Generated || field.GenerationExpression != "price * quantity" {
				t.Fatalf("field generation = %#v", field)
			}
			if field.DefaultExpression != "" {
				t.Fatalf("field default expression = %q, want empty for generated column", field.DefaultExpression)
			}
		})
	}
}
