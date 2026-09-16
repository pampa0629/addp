package postgresql

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestPostgresAnalyticalScanContracts(t *testing.T) {
	cases := []conformance.ScanTypeCase{
		{"smallint", datatype.FieldTypeInt, 0, 0, true}, {"integer", datatype.FieldTypeInt, 0, 0, true}, {"bigint", datatype.FieldTypeBigInt, 0, 0, true}, {"boolean", datatype.FieldTypeBool, 0, 0, true}, {"text", datatype.FieldTypeString, 0, 0, true}, {"character varying(80)", datatype.FieldTypeString, 0, 0, true}, {"date", datatype.FieldTypeDate, 0, 0, true},
		{"numeric(38,18)", datatype.FieldTypeDecimal, 38, 18, true}, {"numeric(10,2)", datatype.FieldTypeDecimal, 10, 2, true}, {"numeric(20,0)", datatype.FieldTypeDecimal, 20, 0, true},
		{"numeric(38,17)", datatype.FieldTypeDecimal, 38, 17, false}, {"numeric(38,19)", datatype.FieldTypeDecimal, 38, 19, false}, {"numeric(10,2)", datatype.FieldTypeDecimal, 38, 18, false}, {"numeric", datatype.FieldTypeDecimal, 0, 0, false}, {"double precision", datatype.FieldTypeDecimal, 0, 0, false},
		{"character(20)", datatype.FieldTypeString, 0, 0, false}, {"bytea", datatype.FieldTypeString, 0, 0, false}, {"integer[]", datatype.FieldTypeString, 0, 0, false}, {"timestamp without time zone", datatype.FieldTypeDate, 0, 0, false}, {"integer", datatype.FieldTypeBigInt, 0, 0, false}, {"public.money", datatype.FieldTypeDecimal, 10, 2, false}, {"text); SELECT 1", datatype.FieldTypeString, 0, 0, false},
	}
	conformance.ScanContracts(t, analyticalScanDialect{}, (&PostgreSQLPlugin{}).EngineCatalogModel(), analyticalExpressionDialect{}.QuoteIdentifier, cases)
	if err := (analyticalScanDialect{}).ValidateSchema([]datatype.FieldInfo{{Name: "A", Type: datatype.FieldTypeInt}, {Name: "a", Type: datatype.FieldTypeInt}}); err != nil {
		t.Fatal(err)
	}
}
