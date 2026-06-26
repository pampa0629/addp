package postgresql

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
)

func TestPostgreSQLCapabilitiesDeclareTableWritePrepare(t *testing.T) {
	caps := (&PostgreSQLPlugin{}).Capabilities()
	if caps.Storage == nil || caps.Storage.Store == nil || !caps.Storage.Store.TableWritePrepare {
		t.Fatalf("postgresql capabilities do not declare table_write_prepare: %#v", caps.Storage)
	}
	if !caps.Storage.Store.TableReadSession {
		t.Fatalf("postgresql capabilities do not declare table_read_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.TableWriteSession {
		t.Fatalf("postgresql capabilities do not declare table_write_session: %#v", caps.Storage.Store)
	}
	if !caps.Storage.Store.Delete {
		t.Fatalf("postgresql capabilities do not declare delete: %#v", caps.Storage.Store)
	}
	spatialEncoding := caps.Storage.Store.TableSpatialEncoding
	if spatialEncoding == nil || !spatialEncoding.ReadTransform || !spatialEncoding.NativeSpatialFunctions {
		t.Fatalf("postgresql capabilities do not declare table spatial encoding transform support: %#v", spatialEncoding)
	}
	if !plugin.Contains(spatialEncoding.GeometryReadEncodings, "ewkb") || !plugin.Contains(spatialEncoding.GeometryReadEncodings, "geojson") {
		t.Fatalf("postgresql read encodings = %#v, want ewkb and geojson", spatialEncoding.GeometryReadEncodings)
	}
	if !plugin.Contains(spatialEncoding.GeometryWriteEncodings, "ewkb") {
		t.Fatalf("postgresql write encodings = %#v, want ewkb", spatialEncoding.GeometryWriteEncodings)
	}
	if err := plugin.ValidatePluginCapabilities(&PostgreSQLPlugin{}); err != nil {
		t.Fatalf("ValidatePluginCapabilities failed: %v", err)
	}
}

func TestPostgresSQLTypeForField(t *testing.T) {
	tests := []struct {
		name        string
		field       datatype.FieldInfo
		spatialInfo *datatype.SpatialInfo
		want        string
	}{
		{name: "spatial info geometry type and srid", field: datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}, spatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "MultiPolygon", 4326, 0), want: "GEOMETRY(MultiPolygon,4326)"},
		{name: "spatial info dimension z", field: datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}, spatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 3), want: "GEOMETRY(PointZ,4326)"},
		{name: "common int", field: datatype.FieldInfo{Name: "id", Type: "int"}, want: "INTEGER"},
		{name: "unknown defaults text", field: datatype.FieldInfo{Name: "x", Type: "unknown"}, want: "TEXT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := postgresSQLTypeForField(tt.field, tt.spatialInfo); got != tt.want {
				t.Fatalf("postgresSQLTypeForField() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPostgresSchemaEvolutionStatementsAddsMissingColumns(t *testing.T) {
	statements, err := postgresSchemaEvolutionStatements("public", "roads", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt},
		{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		{Name: "geom", Type: datatype.FieldTypeGeometry, Nullable: true},
		{Name: "name", Type: datatype.FieldTypeString},
	}, datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 3), []postgresColumnInfo{
		{Name: "id", DataType: "bigint", NativeType: "bigint"},
	})
	if err != nil {
		t.Fatalf("postgresSchemaEvolutionStatements failed: %v", err)
	}
	want := []string{
		`ALTER TABLE "public"."roads" ADD COLUMN "name" TEXT`,
		`ALTER TABLE "public"."roads" ADD COLUMN "geom" GEOMETRY(PointZ,4326)`,
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

func TestPostgresSchemaEvolutionStatementsRejectsTypeConflict(t *testing.T) {
	_, err := postgresSchemaEvolutionStatements("public", "target", []datatype.FieldInfo{
		{Name: "amount", Type: datatype.FieldTypeDouble},
	}, nil, []postgresColumnInfo{
		{Name: "amount", DataType: "text", NativeType: "text"},
	})
	if err == nil {
		t.Fatal("postgresSchemaEvolutionStatements succeeded with conflicting type, want error")
	}
}

func TestPostgresSchemaEvolutionStatementsRejectsMissingPrimaryKeyColumn(t *testing.T) {
	_, err := postgresSchemaEvolutionStatements("public", "target", []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true},
	}, nil, nil)
	if err == nil {
		t.Fatal("postgresSchemaEvolutionStatements succeeded with missing primary key column, want error")
	}
}

func TestPostgresSchemaEvolutionStatementsRejectsMissingNonNullColumnWithoutDefault(t *testing.T) {
	_, err := postgresSchemaEvolutionStatements("public", "target", []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString, Nullable: false},
	}, nil, nil)
	if err == nil {
		t.Fatal("postgresSchemaEvolutionStatements succeeded with missing non-null column without default, want error")
	}
}

func TestPostgresSchemaEvolutionStatementsAddsMissingNonNullColumnWithDefault(t *testing.T) {
	statements, err := postgresSchemaEvolutionStatements("public", "target", []datatype.FieldInfo{
		{Name: "status", Type: datatype.FieldTypeString, Nullable: false, DefaultExpression: "'new'"},
	}, nil, nil)
	if err != nil {
		t.Fatalf("postgresSchemaEvolutionStatements failed: %v", err)
	}
	want := `ALTER TABLE "public"."target" ADD COLUMN "status" TEXT DEFAULT 'new' NOT NULL`
	if len(statements) != 1 || statements[0] != want {
		t.Fatalf("statements = %#v, want [%q]", statements, want)
	}
}

func TestPostgresColumnCompatibleWithFieldAcceptsMatchingSpatialFacts(t *testing.T) {
	column := postgresColumnInfo{Name: "geom", DataType: "USER-DEFINED", UDTName: "geometry", NativeType: "geometry(Point,4326)"}
	field := datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}
	if !postgresColumnCompatibleWithField(column, field, datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0)) {
		t.Fatal("postgresColumnCompatibleWithField rejected matching spatial facts")
	}
}

func TestPostgresColumnCompatibleWithFieldRejectsSpatialFactMismatch(t *testing.T) {
	column := postgresColumnInfo{Name: "geom", DataType: "USER-DEFINED", UDTName: "geometry", NativeType: "geometry(Point,4326)"}
	field := datatype.FieldInfo{Name: "geom", Type: datatype.FieldTypeGeometry}
	if postgresColumnCompatibleWithField(column, field, datatype.NewSingleGeometrySpatialInfo("geom", "Polygon", 4326, 0)) {
		t.Fatal("postgresColumnCompatibleWithField accepted geometry type mismatch")
	}
	if postgresColumnCompatibleWithField(column, field, datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 0)) {
		t.Fatal("postgresColumnCompatibleWithField accepted SRID mismatch")
	}
}
