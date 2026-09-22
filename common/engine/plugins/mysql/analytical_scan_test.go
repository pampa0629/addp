package mysql

import (
	"errors"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/shared/analytical"
	"github.com/addp/common/query/sqlcompile/conformance"
	"testing"
)

func TestMySQLAnalyticalScanContracts(t *testing.T) {
	cases := []conformance.ScanTypeCase{
		{"tinyint", datatype.FieldTypeInt, 0, 0, true}, {"smallint", datatype.FieldTypeInt, 0, 0, true}, {"mediumint", datatype.FieldTypeInt, 0, 0, true}, {"int", datatype.FieldTypeInt, 0, 0, true}, {"bigint", datatype.FieldTypeBigInt, 0, 0, true}, {"tinyint(1)", datatype.FieldTypeBool, 0, 0, true}, {"varchar(80)", datatype.FieldTypeString, 0, 0, true}, {"text", datatype.FieldTypeString, 0, 0, true}, {"date", datatype.FieldTypeDate, 0, 0, true},
		{"decimal(38,18)", datatype.FieldTypeDecimal, 38, 18, true}, {"decimal(10,2)", datatype.FieldTypeDecimal, 10, 2, true}, {"decimal(20,0)", datatype.FieldTypeDecimal, 20, 0, true},
		{"decimal(38,17)", datatype.FieldTypeDecimal, 38, 17, false}, {"decimal(38,19)", datatype.FieldTypeDecimal, 38, 19, false}, {"decimal(10,2)", datatype.FieldTypeDecimal, 38, 18, false}, {"decimal(20,0) unsigned", datatype.FieldTypeDecimal, 20, 0, false}, {"bigint unsigned", datatype.FieldTypeBigInt, 0, 0, false}, {"int unsigned", datatype.FieldTypeInt, 0, 0, false},
		{"char(80)", datatype.FieldTypeString, 0, 0, false}, {"binary(80)", datatype.FieldTypeString, 0, 0, false}, {"json", datatype.FieldTypeString, 0, 0, false}, {"timestamp", datatype.FieldTypeDate, 0, 0, false}, {"float", datatype.FieldTypeDecimal, 0, 0, false}, {"tinyint(1)", datatype.FieldTypeInt, 0, 0, false}, {"varchar(80)); SELECT 1", datatype.FieldTypeString, 0, 0, false},
	}
	conformance.ScanContracts(t, analytical.MySQLCompatibleScanDialect{CatalogModel: (&MySQLPlugin{}).EngineCatalogModel(), IsSystemSchema: (&MySQLPlugin{}).isSystemSchema}, (&MySQLPlugin{}).EngineCatalogModel(), analytical.MySQLCompatibleExpressionDialect{}.QuoteIdentifier, cases)
	if err := (analytical.MySQLCompatibleScanDialect{CatalogModel: (&MySQLPlugin{}).EngineCatalogModel(), IsSystemSchema: (&MySQLPlugin{}).isSystemSchema}).ValidateSchema([]datatype.FieldInfo{{Name: "A", Type: datatype.FieldTypeInt}, {Name: "a", Type: datatype.FieldTypeInt}}); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatal(err)
	}
}
