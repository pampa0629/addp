package mysql

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestMySQLCatalogFieldTypeMapsNativeTypes(t *testing.T) {
	if mysqlCatalogFactsDialect.MapFieldType == nil {
		t.Fatal("MySQL CatalogFacts dialect must declare its field type mapper")
	}
	tests := map[string]datatype.FieldType{
		"int":           datatype.FieldTypeInt,
		"bigint":        datatype.FieldTypeBigInt,
		"decimal(18,2)": datatype.FieldTypeDecimal,
		"varchar(255)":  datatype.FieldTypeString,
		"tinyint(1)":    datatype.FieldTypeBool,
		"geometry":      datatype.FieldTypeGeometry,
	}
	for nativeType, want := range tests {
		if got := mysqlCatalogFactsDialect.MapFieldType(nativeType); got != want {
			t.Fatalf("mysqlCatalogFieldType(%q) = %q, want %q", nativeType, got, want)
		}
	}
}
