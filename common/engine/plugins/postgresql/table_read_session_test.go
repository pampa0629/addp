package postgresql

import (
	"database/sql"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
)

func TestPostgresOpenTableReadSessionRejectsResumeMarker(t *testing.T) {
	postgresPlugin := &PostgreSQLPlugin{}
	_, err := postgresPlugin.OpenTableReadSession(nil, nil, plugin.EngineCatalogPath{}, plugin.TableReadSessionOptions{
		ResumeMarker: &resume.Marker{Version: resume.MarkerVersionV1},
	})
	if err == nil {
		t.Fatal("OpenTableReadSession succeeded with resume marker, want explicit unsupported error")
	}
}

func TestPostgresFieldInfoFromColumnKeepsSpatialNativeType(t *testing.T) {
	field := postgresFieldInfoFromColumn(postgresColumnInfo{
		Name:       "SmGeometry",
		DataType:   "USER-DEFINED",
		UDTName:    "geometry",
		NativeType: "geometry(MultiPolygon,4326)",
	})

	if field.Name != "SmGeometry" {
		t.Fatalf("field name = %q, want SmGeometry", field.Name)
	}
	if field.Type != "geometry" {
		t.Fatalf("field type = %q, want geometry", field.Type)
	}
	if field.NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("native type = %q, want geometry(MultiPolygon,4326)", field.NativeType)
	}
	spatialInfo := postgresSpatialInfoFromFields([]datatype.FieldInfo{field})
	if spatialInfo.PrimaryGeometryType() != "MultiPolygon" || spatialInfo.PrimarySRIDValue() != 4326 {
		t.Fatalf("spatial info = %#v, want standard spatial facts", spatialInfo)
	}
}

func TestPostgresFieldInfoFromColumnMapsNativeTypesToCanonicalTypes(t *testing.T) {
	tests := []struct {
		name       string
		column     postgresColumnInfo
		wantType   datatype.FieldType
		wantNative string
	}{
		{name: "integer", column: postgresColumnInfo{DataType: "integer", UDTName: "int4", NativeType: "integer"}, wantType: datatype.FieldTypeInt, wantNative: "integer"},
		{name: "numeric", column: postgresColumnInfo{DataType: "numeric", UDTName: "numeric", NativeType: "numeric(18,2)"}, wantType: datatype.FieldTypeDecimal, wantNative: "numeric(18,2)"},
		{name: "text", column: postgresColumnInfo{DataType: "text", UDTName: "text", NativeType: "text"}, wantType: datatype.FieldTypeString, wantNative: "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.column.Name = tt.name
			field := postgresFieldInfoFromColumn(tt.column)
			if field.Type != tt.wantType || field.NativeType != tt.wantNative {
				t.Fatalf("field = %#v, want type %q and native type %q", field, tt.wantType, tt.wantNative)
			}
		})
	}
}

func TestPostgresFieldInfoFromColumnKeepsDecimalPrecisionAndScale(t *testing.T) {
	field := postgresFieldInfoFromColumn(postgresColumnInfo{
		Name:             "amount",
		DataType:         "numeric",
		UDTName:          "numeric",
		NativeType:       "numeric(18,2)",
		NumericPrecision: sql.NullInt64{Int64: 18, Valid: true},
		NumericScale:     sql.NullInt64{Int64: 2, Valid: true},
	})

	if field.Precision != 18 || field.Scale != 2 {
		t.Fatalf("decimal precision/scale = %d/%d, want 18/2", field.Precision, field.Scale)
	}
}

func TestPostgresReadBatchFieldsKeepsTableFieldFactsInColumnOrder(t *testing.T) {
	fields := postgresReadBatchFields([]string{"id", "SmGeometry"}, []datatype.FieldInfo{
		{Name: "SmGeometry", Type: "geometry", NativeType: "geometry(MultiPolygon,4326)"},
		{Name: "id", Type: "bigint"},
	})

	if len(fields) != 2 {
		t.Fatalf("fields length = %d, want 2", len(fields))
	}
	if fields[0].Name != "id" || fields[0].Type != "bigint" {
		t.Fatalf("first field = %#v, want id bigint", fields[0])
	}
	if fields[1].Name != "SmGeometry" || fields[1].Type != "geometry" || fields[1].NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("second field = %#v, want spatial field facts", fields[1])
	}
}

func TestPostgresSelectedFieldsFollowsFieldSelectionOrder(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id", Type: "bigint"},
		{Name: "name", Type: "string"},
		{Name: "geom", Type: "geometry"},
	}

	selected, err := postgresSelectedFields(fields, map[string]interface{}{
		format.FieldSelectionOptionKey: &format.FieldSelectionOptions{
			Include: []string{"name", "id", "name"},
		},
	})
	if err != nil {
		t.Fatalf("postgresSelectedFields failed: %v", err)
	}
	if len(selected) != 2 {
		t.Fatalf("selected fields = %#v, want 2 fields", selected)
	}
	if selected[0].Name != "name" || selected[1].Name != "id" {
		t.Fatalf("selected fields = %#v, want name,id", selected)
	}
}

func TestPostgresSelectedFieldsErrorsOnMissingFieldByDefault(t *testing.T) {
	_, err := postgresSelectedFields([]datatype.FieldInfo{{Name: "id"}}, map[string]interface{}{
		format.FieldSelectionOptionKey: &format.FieldSelectionOptions{
			Include: []string{"id", "missing"},
		},
	})
	if err == nil {
		t.Fatal("postgresSelectedFields succeeded, want missing field error")
	}
}

func TestPostgresSelectedFieldsIgnoresMissingFieldWhenConfigured(t *testing.T) {
	selected, err := postgresSelectedFields([]datatype.FieldInfo{{Name: "id"}}, map[string]interface{}{
		format.FieldSelectionOptionKey: format.FieldSelectionOptions{
			Include:            []string{"missing", "id"},
			MissingFieldPolicy: format.MissingFieldIgnore,
		},
	})
	if err != nil {
		t.Fatalf("postgresSelectedFields failed: %v", err)
	}
	if len(selected) != 1 || selected[0].Name != "id" {
		t.Fatalf("selected fields = %#v, want id", selected)
	}
}

func TestPostgresSelectExprForFieldsQuotesSelectedColumns(t *testing.T) {
	expr := postgresSelectExprForFields([]datatype.FieldInfo{
		{Name: "id"},
		{Name: "Road Name"},
	})
	if expr != `"id", "Road Name"` {
		t.Fatalf("select expr = %q, want quoted selected fields", expr)
	}
}

func TestPostgresGeoJSONSelectExprTransformsSpatialColumnToTargetSrid(t *testing.T) {
	expr, err := postgresGeoJSONSelectExpr([]postgresColumnInfo{
		{Name: "geometry", DataType: "USER-DEFINED", UDTName: "geometry", NativeType: "geometry(Polygon,32650)"},
		{Name: "name", DataType: "text"},
	}, map[string]interface{}{
		"geometry_encoding":         "geojson",
		"geometry_field":            "geometry",
		"geometry_target_srid":      4326,
		"geometry_transform_policy": "required",
	}, []datatype.FieldInfo{
		{Name: "geometry", Type: "geometry"},
		{Name: "name", Type: "string"},
	})
	if err != nil {
		t.Fatalf("postgresGeoJSONSelectExpr failed: %v", err)
	}
	want := `ST_AsGeoJSON(ST_Transform("geometry", 4326))::json AS "geometry", "name"`
	if expr != want {
		t.Fatalf("select expr = %q, want %q", expr, want)
	}
}

func TestPostgresGeoJSONSelectExprErrorsWhenSpatialSRIDUnknownAndTransformRequired(t *testing.T) {
	_, err := postgresGeoJSONSelectExpr([]postgresColumnInfo{
		{Name: "geometry", DataType: "USER-DEFINED", UDTName: "geometry", NativeType: "geometry"},
	}, map[string]interface{}{
		"geometry_encoding":         "geojson",
		"geometry_field":            "geometry",
		"geometry_target_srid":      4326,
		"geometry_transform_policy": "required",
	}, []datatype.FieldInfo{
		{Name: "geometry", Type: "geometry"},
	})
	if err == nil {
		t.Fatal("postgresGeoJSONSelectExpr succeeded, want source SRID required error")
	}
}

func TestPostgresGeometryEncodingHint(t *testing.T) {
	if got := postgresGeometryEncodingHint(map[string]interface{}{plugin.TableReadHintGeometryEncoding: "EWKB"}); got != format.GeometryEncodingEWKB {
		t.Fatalf("geometry encoding hint = %q, want ewkb", got)
	}
	if got := postgresGeometryEncodingHint(map[string]interface{}{plugin.TableReadHintGeometryEncoding: "geojson"}); got != format.GeometryEncodingGeoJSON {
		t.Fatalf("geometry encoding hint = %q, want geojson", got)
	}
	if got := postgresGeometryEncodingHint(map[string]interface{}{plugin.TableReadHintGeometryEncoding: "wkt"}); got != "" {
		t.Fatalf("geometry encoding hint = %q, want empty for unsupported read encoding", got)
	}
}

func TestPostgresEWKBSelectExprTransformsSpatialColumnToTargetSrid(t *testing.T) {
	expr, err := postgresEWKBSelectExpr([]postgresColumnInfo{
		{Name: "geometry", DataType: "USER-DEFINED", UDTName: "geometry", NativeType: "geometry(Polygon,32650)"},
		{Name: "name", DataType: "text"},
	}, map[string]interface{}{
		"geometry_encoding":         "ewkb",
		"geometry_field":            "geometry",
		"geometry_target_srid":      4326,
		"geometry_transform_policy": "required",
	}, []datatype.FieldInfo{
		{Name: "geometry", Type: "geometry"},
		{Name: "name", Type: "string"},
	})
	if err != nil {
		t.Fatalf("postgresEWKBSelectExpr failed: %v", err)
	}
	want := `ST_AsEWKB(ST_Transform("geometry", 4326)) AS "geometry", "name"`
	if expr != want {
		t.Fatalf("select expr = %q, want %q", expr, want)
	}
}

func TestPostgresEWKBSelectExprErrorsWhenSpatialSRIDUnknownAndTransformRequired(t *testing.T) {
	_, err := postgresEWKBSelectExpr([]postgresColumnInfo{
		{Name: "geometry", DataType: "USER-DEFINED", UDTName: "geometry", NativeType: "geometry"},
	}, map[string]interface{}{
		"geometry_encoding":         "ewkb",
		"geometry_field":            "geometry",
		"geometry_target_srid":      4326,
		"geometry_transform_policy": "required",
	}, []datatype.FieldInfo{
		{Name: "geometry", Type: "geometry"},
	})
	if err == nil {
		t.Fatal("postgresEWKBSelectExpr succeeded, want source SRID required error")
	}
}

func TestPostgresSpatialInfoFromFieldsAppliesTargetSRIDWhenReadTransformRequired(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeInt},
		{Name: "geometry", Type: datatype.FieldTypeGeometry, NativeType: "geometry(Point,3857)"},
	}

	spatialInfo := postgresSpatialInfoFromFieldsWithHints(fields, map[string]interface{}{
		plugin.TableReadHintGeometryEncoding:        string(format.GeometryEncodingGeoJSON),
		plugin.TableReadHintGeometryField:           "geometry",
		plugin.TableReadHintGeometryTargetSRID:      4326,
		plugin.TableReadHintGeometryTransformPolicy: "required",
	})

	if spatialInfo == nil || spatialInfo.PrimarySRIDValue() != 4326 || spatialInfo.PrimaryCRSRef() != "EPSG:4326" {
		t.Fatalf("spatial info = %#v, want transformed target CRS", spatialInfo)
	}
}

func TestPostgresKeepsBytesOnlyForEWKBGeometryColumns(t *testing.T) {
	if !isGeometryColumn([]datatype.FieldInfo{{Name: "geom", Type: datatype.FieldTypeGeometry}}, "GEOM") {
		t.Fatal("isGeometryColumn returned false for case-insensitive geometry column")
	}
	if isGeometryColumn([]datatype.FieldInfo{{Name: "payload", Type: datatype.FieldTypeString}}, "payload") {
		t.Fatal("isGeometryColumn returned true for non-spatial column")
	}
}
