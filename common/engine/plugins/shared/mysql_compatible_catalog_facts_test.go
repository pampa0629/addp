package shared

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
)

func TestMySQLCompatibleTableNativeRequiresIncludeEngine(t *testing.T) {
	dialect := MySQLCompatibleCatalogFactsDialect{}
	if got := dialect.tableNative("InnoDB"); got != nil {
		t.Fatalf("tableNative() = %#v, want nil when IncludeEngine is false", got)
	}

	dialect.IncludeEngine = true
	got := dialect.tableNative(" InnoDB ")
	if got["engine"] != "InnoDB" {
		t.Fatalf("tableNative() = %#v, want InnoDB engine", got)
	}
}

func TestMySQLCompatibleColumnsQuerySelectsDecimalFacts(t *testing.T) {
	for _, column := range []string{"numeric_precision", "numeric_scale"} {
		if !strings.Contains(strings.ToLower(mysqlCompatibleColumnsQuery), column) {
			t.Fatalf("columns query must select %s", column)
		}
	}
}

func TestMySQLCompatibleFieldInfoKeepsDecimalPrecisionAndScale(t *testing.T) {
	dialect := MySQLCompatibleCatalogFactsDialect{
		MapFieldType: func(nativeType string) datatype.FieldType {
			if strings.HasPrefix(strings.ToLower(nativeType), "decimal") {
				return datatype.FieldTypeDecimal
			}
			return datatype.FieldTypeString
		},
	}

	field := dialect.fieldInfo(mysqlCompatibleColumnRow{
		Name:             "amount",
		NativeType:       "decimal(20,10)",
		NumericPrecision: sql.NullInt64{Int64: 20, Valid: true},
		NumericScale:     sql.NullInt64{Int64: 10, Valid: true},
	})
	if field.Type != datatype.FieldTypeDecimal || field.Precision != 20 || field.Scale != 10 {
		t.Fatalf("decimal field = %#v, want precision 20 and scale 10", field)
	}
}

func TestMySQLCompatibleFieldInfoDoesNotInventDecimalFacts(t *testing.T) {
	dialect := MySQLCompatibleCatalogFactsDialect{
		MapFieldType: func(nativeType string) datatype.FieldType {
			if strings.HasPrefix(strings.ToLower(nativeType), "decimal") {
				return datatype.FieldTypeDecimal
			}
			return datatype.FieldTypeString
		},
	}

	tests := []mysqlCompatibleColumnRow{
		{Name: "amount", NativeType: "decimal"},
		{
			Name:             "label",
			NativeType:       "varchar(64)",
			NumericPrecision: sql.NullInt64{Int64: 64, Valid: true},
			NumericScale:     sql.NullInt64{Int64: 0, Valid: true},
		},
	}
	for _, row := range tests {
		field := dialect.fieldInfo(row)
		if field.Precision != 0 || field.Scale != 0 {
			t.Fatalf("field %q facts = %d/%d, want no decimal facts", field.Name, field.Precision, field.Scale)
		}
	}
}
