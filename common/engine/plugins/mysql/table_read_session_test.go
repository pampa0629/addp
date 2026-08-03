package mysql

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func TestMySQLSpatialReadExpressionUsesExplicitAxisOrderAndTransform(t *testing.T) {
	column := mysqlColumnInfo{Name: "geom", DataType: "point", SRSID: sql.NullInt64{Int64: 4326, Valid: true}}
	expression, err := mysqlSpatialReadExpression("`geom`", column, map[string]interface{}{
		plugin.TableReadHintGeometryTargetSRID:      3857,
		plugin.TableReadHintGeometryTransformPolicy: "required",
	}, format.GeometryEncodingEWKB)
	if err != nil {
		t.Fatal(err)
	}
	want := "ST_AsWKB(ST_Transform(`geom`, 3857), 'axis-order=long-lat')"
	if expression != want {
		t.Fatalf("expression = %q, want %q", expression, want)
	}
}

func TestMySQLSpatialReadExpressionRejectsRequiredTransformWithoutSourceSRID(t *testing.T) {
	_, err := mysqlSpatialReadExpression("`geom`", mysqlColumnInfo{Name: "geom", DataType: "geometry"}, map[string]interface{}{
		plugin.TableReadHintGeometryTargetSRID:      3857,
		plugin.TableReadHintGeometryTransformPolicy: "required",
	}, format.GeometryEncodingGeoJSON)
	if err == nil || !strings.Contains(err.Error(), "known source SRID") {
		t.Fatalf("error = %v", err)
	}
}

func TestMySQLSpatialInfoFromColumnsAppliesTargetSRID(t *testing.T) {
	columns := []mysqlColumnInfo{{Name: "geom", DataType: "point", NativeType: "point", SRSID: sql.NullInt64{Int64: 4326, Valid: true}}}
	fields := []datatype.FieldInfo{{Name: "geom", Type: datatype.FieldTypeGeometry}}
	info := mysqlSpatialInfoFromColumns(columns, fields, map[string]interface{}{
		plugin.TableReadHintGeometryTargetSRID: 3857,
	}, format.GeometryEncodingEWKB)
	if info == nil || info.GeometryColumns[0].SRID == nil || *info.GeometryColumns[0].SRID != 3857 {
		t.Fatalf("spatial info = %#v", info)
	}
}
