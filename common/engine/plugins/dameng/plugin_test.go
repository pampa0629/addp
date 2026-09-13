package dameng

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonquery "github.com/addp/common/query"
)

func TestPluginControlPlaneContract(t *testing.T) {
	p := &Plugin{}
	if p.Type() != "dameng" || p.EngineOrigin() != "general" || p.DefaultPort() != 5236 {
		t.Fatalf("unexpected DM8 plugin identity: type=%q origin=%q port=%d", p.Type(), p.EngineOrigin(), p.DefaultPort())
	}
	if got := p.ConnectionIdentityFields(); strings.Join(got, ",") != "host,port" {
		t.Fatalf("ConnectionIdentityFields() = %v", got)
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
	if p.SQLDialect() != commonquery.DialectDameng {
		t.Fatalf("SQLDialect() = %q", p.SQLDialect())
	}
}

func TestBuildDSNUsesOfficialDriverSyntax(t *testing.T) {
	dsn, err := (&Plugin{}).BuildDSN(plugin.ConnectionInfo{
		"host": "127.0.0.1", "port": 5236, "user": "ADDP_BUSINESS", "password": "AddpBusiness8X",
	})
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	if dsn != "dm://ADDP_BUSINESS:AddpBusiness8X@127.0.0.1:5236" {
		t.Fatalf("BuildDSN() = %q", dsn)
	}
}

func TestBuildDSNRejectsReservedCredentialCharacters(t *testing.T) {
	_, err := (&Plugin{}).BuildDSN(plugin.ConnectionInfo{
		"host": "127.0.0.1", "port": 5236, "user": "ADDP_BUSINESS", "password": "Addp@DM8!",
	})
	if err == nil || !strings.Contains(err.Error(), "reserved DSN character") {
		t.Fatalf("BuildDSN() error = %v", err)
	}
}

func TestDataPlaneFailsClosedWithoutOfficialDriver(t *testing.T) {
	if driverLoaded() {
		t.Skip("official DM driver is loaded by this test process")
	}
	err := (&Plugin{}).TestConnection(t.Context(), plugin.ConnectionInfo{
		"host": "127.0.0.1", "port": 5236, "user": "ADDP_BUSINESS", "password": "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "Linux ARM64 Docker boundary") {
		t.Fatalf("TestConnection() error = %v", err)
	}
}

func TestMergeSQLUsesDM8QuestionParameters(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "ID", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
		{Name: "NAME", Type: datatype.FieldTypeString, Size: 128},
		{Name: "AMOUNT", Type: datatype.FieldTypeDecimal, Precision: 18, Scale: 2},
	}
	statement, err := mergeSQL("ADDP_BUSINESS", "TARGET", fields, []string{"ID"})
	if err != nil {
		t.Fatalf("mergeSQL() error = %v", err)
	}
	wantParts := []string{
		`MERGE INTO "ADDP_BUSINESS"."TARGET" target`,
		`CAST(? AS BIGINT) AS "ID"`,
		`CAST(? AS VARCHAR(128)) AS "NAME"`,
		`CAST(? AS DECIMAL(18,2)) AS "AMOUNT"`,
		`FROM DUAL`,
		`target."ID" = source."ID"`,
		`WHEN MATCHED THEN UPDATE SET target."NAME" = source."NAME"`,
		`WHEN NOT MATCHED THEN INSERT`,
	}
	for _, part := range wantParts {
		if !strings.Contains(statement, part) {
			t.Fatalf("mergeSQL() = %q, missing %q", statement, part)
		}
	}
}

func TestCursorPredicateBuildsLexicographicBoundary(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "UPDATED_AT", Type: datatype.FieldTypeTimestamp},
		{Name: "ID", Type: datatype.FieldTypeBigInt},
	}
	predicate, args, err := cursorPredicate(commonquery.ForDialect(commonquery.DialectDameng), fields, []string{"2026-09-13T08:00:00Z", "7"}, ">")
	if err != nil {
		t.Fatalf("cursorPredicate() error = %v", err)
	}
	want := `(("UPDATED_AT" > CAST(? AS TIMESTAMP(6))) OR ("UPDATED_AT" = CAST(? AS TIMESTAMP(6)) AND "ID" > CAST(? AS BIGINT)))`
	if predicate != want || len(args) != 3 {
		t.Fatalf("cursorPredicate() = %q args=%v", predicate, args)
	}
}
