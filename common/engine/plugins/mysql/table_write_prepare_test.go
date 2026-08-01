package mysql

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestMySQLCapabilitiesDeclareTableWriteProviders(t *testing.T) {
	caps := (&MySQLPlugin{}).Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil {
		t.Fatalf("mysql capabilities missing store: %#v", caps.Storage)
	}
	if !caps.Storage.Store.TableWritePrepare {
		t.Fatalf("mysql capabilities do not declare table_write_prepare: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.TableWriteSession {
		t.Fatalf("mysql capabilities do not declare table_write_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.BatchWrite {
		t.Fatalf("mysql capabilities do not declare batch_write: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.Delete {
		t.Fatalf("mysql capabilities do not declare delete: %#v", caps.Storage.Store)
	}
	if err := plugin.ValidatePluginCapabilities(&MySQLPlugin{}); err != nil {
		t.Fatalf("ValidatePluginCapabilities failed: %v", err)
	}
}

func TestMySQLSQLTypeForField(t *testing.T) {
	tests := []struct {
		name        string
		field       datatype.FieldInfo
		spatialInfo *datatype.SpatialInfo
		want        string
		wantErr     bool
	}{
		{name: "common bigint", field: datatype.FieldInfo{Name: "id", Type: datatype.FieldTypeBigInt}, want: "BIGINT"},
		{name: "common bool", field: datatype.FieldInfo{Name: "active", Type: datatype.FieldTypeBool}, want: "TINYINT(1)"},
		{name: "time preserves fractional seconds", field: datatype.FieldInfo{Name: "business_time", Type: datatype.FieldTypeTime}, want: "TIME(6)"},
		{name: "timestamp preserves fractional seconds", field: datatype.FieldInfo{Name: "changed_at", Type: datatype.FieldTypeTimestamp}, want: "DATETIME(6)"},
		{name: "decimal requires explicit precision", field: datatype.FieldInfo{Name: "amount", Type: datatype.FieldTypeDecimal}, wantErr: true},
		{name: "decimal precision and scale", field: datatype.FieldInfo{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 10}, want: "DECIMAL(20,10)"},
		{name: "unknown defaults text", field: datatype.FieldInfo{Name: "x", Type: datatype.FieldTypeUnknown}, want: "TEXT"},
		{name: "spatial info geometry type", field: datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}, spatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0), want: "POINT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysqlSQLTypeForField(tt.field, tt.spatialInfo)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("mysqlSQLTypeForField() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("mysqlSQLTypeForField() failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("mysqlSQLTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMySQLSQLTypeForFieldRejectsInvalidDecimalBounds(t *testing.T) {
	tests := []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 66, Scale: 2},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 31},
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 4, Scale: 5},
	}
	for _, field := range tests {
		if _, err := mysqlSQLTypeForField(field, nil); err == nil {
			t.Fatalf("mysqlSQLTypeForField(%#v) succeeded, want bounds error", field)
		}
	}
}

func TestMySQLSchemaEvolutionStatementsAddsMissingColumns(t *testing.T) {
	statements, err := mysqlSchemaEvolutionStatements("analytics", "events", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "geom", Type: datatype.FieldTypeGeometry, Nullable: true},
		{Name: "name", Type: datatype.FieldTypeString},
	}, datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0), []mysqlColumnInfo{
		{Name: "id", DataType: "bigint", NativeType: "bigint"},
	})
	if err != nil {
		t.Fatalf("mysqlSchemaEvolutionStatements failed: %v", err)
	}
	want := []string{
		"ALTER TABLE `analytics`.`events` ADD COLUMN `name` TEXT",
		"ALTER TABLE `analytics`.`events` ADD COLUMN `geom` POINT",
	}
	if len(statements) != len(want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
	for i := range want {
		if statements[i] != want[i] {
			t.Fatalf("statement[%d] = %q, want %q", i, statements[i], want[i])
		}
	}
}

func TestMySQLSchemaEvolutionStatementsRejectsTypeConflict(t *testing.T) {
	_, err := mysqlSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDouble},
	}, nil, []mysqlColumnInfo{
		{Name: "amount", DataType: "text", NativeType: "text"},
	})
	if err == nil {
		t.Fatal("mysqlSchemaEvolutionStatements succeeded with conflicting type, want error")
	}
}

func TestMySQLSchemaEvolutionStatementsRejectsDecimalPrecisionConflict(t *testing.T) {
	_, err := mysqlSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 20, Scale: 10},
	}, nil, []mysqlColumnInfo{
		{
			Name:             "amount",
			DataType:         "decimal",
			NativeType:       "decimal(10,2)",
			NumericPrecision: sql.NullInt64{Int64: 10, Valid: true},
			NumericScale:     sql.NullInt64{Int64: 2, Valid: true},
		},
	})
	if err == nil {
		t.Fatal("mysqlSchemaEvolutionStatements succeeded with decimal precision conflict, want error")
	}
}

func TestMySQLColumnCompatibleWithFieldAcceptsTinyIntOneAsBool(t *testing.T) {
	column := mysqlColumnInfo{Name: "active", DataType: "tinyint", NativeType: "tinyint(1)"}
	field := datatype.FieldInfo{Name: "active", Type: datatype.FieldTypeBool}
	if !mysqlColumnCompatibleWithField(column, field, nil) {
		t.Fatal("mysqlColumnCompatibleWithField rejected tinyint(1) bool column")
	}
}

func TestMySQLColumnCompatibleWithFieldRequiresLosslessTemporalPrecision(t *testing.T) {
	field := datatype.FieldInfo{Name: "changed_at", Type: datatype.FieldTypeTimestamp}
	if mysqlColumnCompatibleWithField(mysqlColumnInfo{
		Name: "changed_at", DataType: "datetime", NativeType: "datetime", TemporalPrecision: sql.NullInt64{Int64: 0, Valid: true},
	}, field, nil) {
		t.Fatal("mysqlColumnCompatibleWithField accepted zero-precision datetime")
	}
	if !mysqlColumnCompatibleWithField(mysqlColumnInfo{
		Name: "changed_at", DataType: "datetime", NativeType: "datetime(6)", TemporalPrecision: sql.NullInt64{Int64: 6, Valid: true},
	}, field, nil) {
		t.Fatal("mysqlColumnCompatibleWithField rejected microsecond datetime")
	}
}

func TestMySQLColumnCompatibleWithFieldRequiresExactUUIDStorage(t *testing.T) {
	field := datatype.FieldInfo{Name: "ref", Type: datatype.FieldTypeUUID}
	if !mysqlColumnCompatibleWithField(mysqlColumnInfo{Name: "ref", DataType: "varchar", NativeType: "varchar(36)"}, field, nil) {
		t.Fatal("mysqlColumnCompatibleWithField rejected VARCHAR(36) UUID storage")
	}
	if mysqlColumnCompatibleWithField(mysqlColumnInfo{Name: "ref", DataType: "varchar", NativeType: "varchar(255)"}, field, nil) {
		t.Fatal("mysqlColumnCompatibleWithField accepted ambiguous VARCHAR(255) UUID storage")
	}
}

func TestMySQLSQLTypeForGeometryTypeNormalizesUnsupportedDimensions(t *testing.T) {
	if got := mysqlSQLTypeForGeometryType("PointZ"); got != "POINT" {
		t.Fatalf("mysqlSQLTypeForGeometryType(PointZ) = %q, want POINT", got)
	}
	if got := mysqlSQLTypeForGeometryType("MultiPolygonZ"); got != "MULTIPOLYGON" {
		t.Fatalf("mysqlSQLTypeForGeometryType(MultiPolygonZ) = %q, want MULTIPOLYGON", got)
	}
	if got := mysqlSQLTypeForGeometryType("GeometryCollection"); got != "GEOMETRYCOLLECTION" {
		t.Fatalf("mysqlSQLTypeForGeometryType(GeometryCollection) = %q, want GEOMETRYCOLLECTION", got)
	}
}

func TestMySQLSchemaEvolutionStatementsRejectsMissingPrimaryKeyColumn(t *testing.T) {
	_, err := mysqlSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
	}, nil, nil)
	if err == nil {
		t.Fatal("mysqlSchemaEvolutionStatements succeeded with missing primary key column, want error")
	}
}

func TestMySQLSchemaEvolutionStatementsRejectsMissingNonNullColumnWithoutDefault(t *testing.T) {
	_, err := mysqlSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
	}, nil, nil)
	if err == nil {
		t.Fatal("mysqlSchemaEvolutionStatements succeeded with missing non-null column without default, want error")
	}
}

func TestMySQLSchemaEvolutionStatementsAddsMissingNonNullColumnWithDefault(t *testing.T) {
	statements, err := mysqlSchemaEvolutionStatements("analytics", "target", []datatype.FieldInfo{
		{Name: "status", Type: datatype.FieldTypeString, Nullable: false, DefaultExpression: "'new'"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("mysqlSchemaEvolutionStatements failed: %v", err)
	}
	want := "ALTER TABLE `analytics`.`target` ADD COLUMN `status` TEXT DEFAULT 'new' NOT NULL"
	if len(statements) != 1 || statements[0] != want {
		t.Fatalf("statements = %#v, want [%q]", statements, want)
	}
}

func TestMySQLTablePathPartsRequiresDatabaseAndTable(t *testing.T) {
	_, _, err := mysqlTablePathParts(plugin.CatalogPath{})
	if err == nil {
		t.Fatal("mysqlTablePathParts() succeeded, want error")
	}
	if !strings.Contains(err.Error(), "database/table") {
		t.Fatalf("error = %q, want database/table", err)
	}
}
