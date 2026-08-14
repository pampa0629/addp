package oracle

import (
	"database/sql"
	"reflect"
	"slices"
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
	if p.Capabilities().Storage.Store == nil || !p.Capabilities().Storage.Store.BatchRead || !p.Capabilities().Storage.Store.TableReadSession ||
		!p.Capabilities().Storage.Store.TableWriteSession || !p.Capabilities().Storage.Store.TableWritePrepare || !p.Capabilities().Storage.Store.Delete {
		t.Fatalf("storage capabilities = %#v", p.Capabilities().Storage)
	}
	spatialEncoding := p.Capabilities().Storage.Store.TableSpatialEncoding
	if !p.Capabilities().Storage.Facts.SpatialFacts || spatialEncoding == nil ||
		len(spatialEncoding.GeometryReadEncodings) != 1 || spatialEncoding.GeometryReadEncodings[0] != "ewkb" ||
		len(spatialEncoding.GeometryWriteEncodings) != 1 || spatialEncoding.GeometryWriteEncodings[0] != "ewkb" ||
		!spatialEncoding.NativeSpatialFunctions {
		t.Fatalf("Oracle spatial capabilities = %#v", p.Capabilities().Storage)
	}
	apply := p.Capabilities().Storage.Store.PartitionedTableChangeApply
	if apply == nil || !apply.Supported || !apply.AtomicPositionCommit || !apply.Monotonic ||
		!plugin.Contains(apply.PositionTypes, "kafka_offset/v1") ||
		!plugin.Contains(apply.Operations, plugin.TableChangeOperationUpsert) ||
		!plugin.Contains(apply.Operations, plugin.TableChangeOperationDelete) ||
		!plugin.Contains(apply.Operations, plugin.TableChangeOperationSkip) {
		t.Fatalf("Oracle partitioned change apply capabilities = %#v", apply)
	}
	if p.Capabilities().Storage.Store.ChangeStreamRead != nil || p.Capabilities().Storage.Store.BoundedWatermarkRead ||
		p.Capabilities().Storage.Store.BatchWrite || p.Capabilities().Storage.Store.TableUpsert != nil {
		t.Fatalf("Oracle first phase must not declare CDC or watermark capabilities")
	}
	if !p.Capabilities().Storage.Facts.Indexes || !p.Capabilities().Storage.Facts.Constraints || !p.Capabilities().Storage.Facts.Partitioning {
		t.Fatalf("Oracle catalog detail capabilities = %#v", p.Capabilities().Storage.Facts)
	}
}

func TestOracleDetailedCatalogFacts(t *testing.T) {
	indexes := buildOracleIndexFacts([]oracleIndexRow{
		{Name: "ORDERS_CUSTOMER_IDX", IndexType: "NORMAL", ColumnName: "CUSTOMER_ID", ColumnPosition: 1},
		{Name: "ORDERS_CUSTOMER_IDX", IndexType: "NORMAL", ColumnName: "ORDERED_AT", ColumnPosition: 2},
		{Name: "ORDERS_PK", IndexType: "NORMAL", Uniqueness: "UNIQUE", ColumnName: "ID", ColumnPosition: 1},
	})
	if len(indexes) != 2 || strings.Join(indexes[0].Fields, ",") != "CUSTOMER_ID,ORDERED_AT" || indexes[0].IndexType != "normal" || !indexes[1].IsUnique {
		t.Fatalf("indexes = %#v", indexes)
	}

	constraints := buildOracleConstraintFacts([]oracleConstraintRow{
		{Name: "ORDERS_CUSTOMER_FK", ConstraintType: "R", ColumnName: "CUSTOMER_ID", Position: 1, ReferencedNamespace: sqlNullString("BUSINESS"), ReferencedTable: sqlNullString("CUSTOMERS"), ReferencedColumn: sqlNullString("ID")},
		{Name: "ORDERS_PK", ConstraintType: "P", ColumnName: "ID", Position: 1},
	})
	if len(constraints) != 2 || constraints[0].ConstraintType != plugin.ConstraintTypeForeignKey || constraints[0].ReferencedTable != "CUSTOMERS" || strings.Join(constraints[0].ReferencedFields, ",") != "ID" {
		t.Fatalf("constraints = %#v", constraints)
	}
	if constraints[1].ConstraintType != plugin.ConstraintTypePrimaryKey {
		t.Fatalf("primary key constraint = %#v", constraints[1])
	}

	if got := normalizeOraclePartitionStrategy(" RANGE "); got != "range" {
		t.Fatalf("partition strategy = %q", got)
	}
	if got := normalizeOraclePartitionStrategy("NONE"); got != "" {
		t.Fatalf("NONE strategy = %q", got)
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

func TestOracleCDCPasswordIsSensitiveButNotRequiredOrIdentity(t *testing.T) {
	p := &OraclePlugin{}
	if !reflect.DeepEqual(p.SensitiveFields(), []string{"password", "cdc_password"}) {
		t.Fatalf("SensitiveFields() = %v", p.SensitiveFields())
	}
	if slices.Contains(p.RequiredFields(), "cdc_password") {
		t.Fatalf("CDC credentials must remain optional for ordinary Oracle engines")
	}
	if slices.Contains(p.ConnectionIdentityFields(), "cdc_password") || slices.Contains(p.ConnectionIdentityFields(), "cdc_user") || slices.Contains(p.ConnectionIdentityFields(), "cdc_database_name") {
		t.Fatalf("CDC credentials must not change the Oracle Engine Instance identity")
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
	if got := (&oraclemapping.TypeMapper{}).ToCommon("SDO_GEOMETRY"); got != "geometry" {
		t.Fatalf("SDO_GEOMETRY type = %q, want geometry", got)
	}
}

func sqlNullInt64(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func sqlNullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: true}
}
