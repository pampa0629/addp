package oracle

import (
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
	go_ora "github.com/sijms/go-ora/v2"
	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/ewkb"
	"github.com/twpayne/go-geom/encoding/wkb"
)

func TestBuildOracleInsertSQLUsesFrozenSpatialConstructor(t *testing.T) {
	srid, dimension := 3424, 2
	fields := []datatype.FieldInfo{
		{Name: "OBJECTID", Type: datatype.FieldTypeBigInt},
		{Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: true},
	}
	spatialInfo := &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{
		Name: "SHAPE", GeometryType: string(datatype.GeometryTypeMultiPolygon), SRID: &srid, Dimension: &dimension,
	}}}
	statement, err := buildOracleInsertSQL("BUSINESS", "UTIL_SOLAR_COUNTY", fields, spatialInfo)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`INSERT INTO "BUSINESS"."UTIL_SOLAR_COUNTY"`,
		`"OBJECTID", "SHAPE"`,
		`SDO_UTIL.FROM_WKBGEOMETRY(:2)`,
		`MDSYS.SDO_GEOMETRY`,
		`2007`,
		`3424`,
	} {
		if !strings.Contains(statement, expected) {
			t.Fatalf("insert SQL %q missing %q", statement, expected)
		}
	}
}

func TestOracleTableWriteValueConvertsEWKBToStandardWKB(t *testing.T) {
	srid, dimension := 4326, 2
	field := datatype.FieldInfo{Name: "SHAPE", Type: datatype.FieldTypeGeometry, Nullable: true}
	spatialInfo := &datatype.SpatialInfo{GeometryColumns: []datatype.GeometryColumnInfo{{
		Name: "SHAPE", GeometryType: string(datatype.GeometryTypePoint), SRID: &srid, Dimension: &dimension,
	}}}
	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9}).SetSRID(srid)
	encoded, err := ewkb.Marshal(point, ewkb.NDR)
	if err != nil {
		t.Fatal(err)
	}
	converted, err := oracleTableWriteValue(field, encoded, spatialInfo)
	if err != nil {
		t.Fatal(err)
	}
	blob, ok := converted.(go_ora.Blob)
	if !ok {
		t.Fatalf("converted geometry type=%T", converted)
	}
	decoded, err := wkb.Unmarshal(blob.Data)
	if err != nil || decoded.SRID() != 0 {
		t.Fatalf("decoded standard WKB=%v err=%v", decoded, err)
	}
	if _, err := oracleTableWriteValue(datatype.FieldInfo{Name: "ID", Type: datatype.FieldTypeBigInt}, nil, nil); err == nil {
		t.Fatal("non-null Oracle field accepted nil")
	}
}

func TestOracleSpatialBoundsDerivesToleranceAndExpandsPoint(t *testing.T) {
	extent := datatype.NewBoundingBox(10, 20, 10, 20)
	minX, minY, maxX, maxY, tolerance, err := oracleSpatialBounds(&datatype.SpatialInfo{Extent: &extent})
	if err != nil {
		t.Fatal(err)
	}
	if !(minX < 10 && maxX > 10 && minY < 20 && maxY > 20) || tolerance <= 0 {
		t.Fatalf("bounds=(%v,%v,%v,%v) tolerance=%v", minX, minY, maxX, maxY, tolerance)
	}
	if _, _, _, _, _, err := oracleSpatialBounds(&datatype.SpatialInfo{}); err == nil {
		t.Fatal("missing frozen extent unexpectedly accepted")
	}
}

func TestOracleTableWriteSessionRejectsResumeMarker(t *testing.T) {
	_, err := (&OraclePlugin{}).OpenTableWriteSession(nil, nil, plugin.EngineCatalogPath{}, plugin.TableWriteSessionOptions{
		ResumeMarker: &resume.Marker{Version: resume.MarkerVersionV1},
	})
	if err == nil || !strings.Contains(err.Error(), "resume") {
		t.Fatalf("resume marker error=%v", err)
	}
}

func TestOracleTableWriteSessionCommitMarker(t *testing.T) {
	session := &oracleTableWriteSession{
		schema: "BUSINESS", table: "UTIL_SOLAR_COUNTY",
		fields:      []datatype.FieldInfo{{Name: "OBJECTID"}, {Name: "SHAPE"}},
		rowsWritten: 21, batchesWritten: 2,
	}
	if session.CommitMarker() != nil {
		t.Fatal("commit marker must be nil before commit")
	}
	marker := session.buildCommitMarker()
	if marker.Provider != oracleTableWriteSessionMarkerProvider || marker.PositionUnit != oracleTableWriteSessionMarkerPositionUnit {
		t.Fatalf("marker=%#v", marker)
	}
	if marker.CommitPosition["rows_committed"] != int64(21) || marker.Fingerprint["method"] != "oracle_insert" {
		t.Fatalf("marker=%#v", marker)
	}
}
