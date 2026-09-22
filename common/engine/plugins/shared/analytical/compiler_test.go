package analytical

import (
	"errors"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/query/plan"
	"github.com/addp/common/query/sqlcompile"
	"github.com/addp/common/query/sqlcompile/conformance"
)

func TestMySQLCompatibleCompilerContract(t *testing.T) {
	compiler := NewMySQLCompatibleCompiler(MySQLCompatibleCompilerOptions{
		CompilerID:     plugin.CompilerIdentity{ID: "shared.mysql_compatible", Version: "1"},
		CatalogModel:   plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase),
		IsSystemSchema: func(name string) bool { return name == "information_schema" },
	})
	conformance.CompilerContract(t, compiler)
}

func TestMySQLCompatibleScanDialect(t *testing.T) {
	dialect := MySQLCompatibleScanDialect{
		CatalogModel:   plugin.TabularCatalogModel(plugin.EngineCatalogTermDatabase),
		IsSystemSchema: func(name string) bool { return name == "information_schema" },
	}
	cases := []conformance.ScanTypeCase{
		{Native: "tinyint", Type: datatype.FieldTypeInt, Supported: true},
		{Native: "bigint", Type: datatype.FieldTypeBigInt, Supported: true},
		{Native: "tinyint(1)", Type: datatype.FieldTypeBool, Supported: true},
		{Native: "varchar(80)", Type: datatype.FieldTypeString, Supported: true},
		{Native: "text", Type: datatype.FieldTypeString, Supported: true},
		{Native: "date", Type: datatype.FieldTypeDate, Supported: true},
		{Native: "decimal(20,2)", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 2, Supported: true},
		{Native: "timestamp", Type: datatype.FieldTypeDate},
		{Native: "bigint unsigned", Type: datatype.FieldTypeBigInt},
		{Native: "decimal(38,19)", Type: datatype.FieldTypeDecimal, Precision: 38, Scale: 19},
	}
	conformance.ScanContracts(t, dialect, dialect.CatalogModel, MySQLCompatibleExpressionDialect{}.QuoteIdentifier, cases)
}

func TestMySQLCompatibleExpressionDialectContracts(t *testing.T) {
	d := MySQLCompatibleExpressionDialect{}
	var _ sqlcompile.ExpressionDialect = d
	if got := d.QuoteIdentifier("a`b"); got != "`a``b`" {
		t.Fatalf("QuoteIdentifier() = %q", got)
	}
	if got := d.Contains("value", "needle"); !strings.Contains(got, "LOCATE") {
		t.Fatalf("Contains() = %q", got)
	}
	if got, err := d.Literal(planLiteralString("中文")); err != nil || !strings.Contains(got, "CONVERT") {
		t.Fatalf("Literal() = %q, %v", got, err)
	}
	if _, err := d.Value("x", datatype.FieldTypeUnknown); !errors.Is(err, plugin.ErrAnalyticalUnsupported) {
		t.Fatalf("unknown value type error = %v", err)
	}
}

func planLiteralString(value string) plan.Literal {
	return plan.Literal{Type: datatype.FieldTypeString, Text: value}
}
