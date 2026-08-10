package oracle

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	oraclemapping "github.com/addp/common/format/mappers/oracle"
)

func TestOraclePluginCapabilitiesAndInterfaces(t *testing.T) {
	p := &OraclePlugin{}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
	if got := p.ConnectionIdentityFields(); strings.Join(got, ",") != "host,port,service_name,user" {
		t.Fatalf("identity fields = %#v", got)
	}
	if p.Capabilities().Storage.Store == nil || !p.Capabilities().Storage.Store.BatchRead || !p.Capabilities().Storage.Store.TableReadSession {
		t.Fatalf("storage capabilities = %#v", p.Capabilities().Storage)
	}
	if p.Capabilities().Storage.Store.TableSpatialEncoding != nil || p.Capabilities().Storage.Facts.SpatialFacts {
		t.Fatalf("Oracle first phase must not declare spatial capabilities")
	}
	if p.Capabilities().Storage.Store.ChangeStreamRead != nil || p.Capabilities().Storage.Store.BoundedWatermarkRead {
		t.Fatalf("Oracle first phase must not declare CDC or watermark capabilities")
	}
}

func TestOracleBuildDSNUsesServiceNameAndDefaultPort(t *testing.T) {
	dsn, err := (&OraclePlugin{}).BuildDSN(plugin.ConnectionInfo{
		"host":         "db.example.com",
		"service_name": "FREEPDB1",
		"user":         "business",
		"password":     "p@ss/word",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"oracle://business:p@ss%2Fword@db.example.com:1521/FREEPDB1", "CONNECTION TIMEOUT=10"} {
		if !strings.Contains(dsn, expected) {
			t.Fatalf("DSN = %q, missing %q", dsn, expected)
		}
	}
}

func TestOracleCatalogTypeAndSystemSchemaPolicy(t *testing.T) {
	p := &OraclePlugin{}
	if !p.isSystemSchema("sde") || !p.isSystemSchema("MDSYS") || p.isSystemSchema("BUSINESS") {
		t.Fatal("unexpected Oracle system schema policy")
	}
	rows := []oracleColumnRow{
		{Name: "ID", DataType: "NUMBER", NumericPrecision: sqlNullInt64(10), NumericScale: sqlNullInt64(0)},
		{Name: "BODY", DataType: "CLOB"},
	}
	if got := oracleColumnNativeType(rows[0]); got != "NUMBER(10,0)" {
		t.Fatalf("native type = %q", got)
	}
	if got := (&oraclemapping.TypeMapper{}).ToCommon("SDO_GEOMETRY"); got != "unknown" {
		t.Fatalf("SDO_GEOMETRY type = %q, want unknown", got)
	}
}

func sqlNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}
