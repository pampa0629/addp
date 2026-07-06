package executor

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
)

func TestSpatialReprojectTransformBatchInvokesProviderAndKeepsEWKBBytes(t *testing.T) {
	sourceGeometry := mustWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{120, 30}))
	provider := &fakeGeometryBatchReprojecter{
		result: [][]byte{
			{0x01, 0x02},
			nil,
		},
	}
	transform, err := newSpatialReprojectTransform(SpatialReprojectTransformPlan{
		GeometryColumn: "geom",
		SourceCRS:      "EPSG:3857",
		TargetCRS:      "EPSG:4326",
		Reproject:      true,
	}, provider)
	if err != nil {
		t.Fatalf("newSpatialReprojectTransform failed: %v", err)
	}

	batch := &engineplugin.BatchData{
		Rows: []map[string]interface{}{
			{"id": 1, "geom": sourceGeometry},
			{"id": 2, "geom": sourceGeometry},
		},
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
		Spatial: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 2),
	}
	batch.Spatial.Extent = &datatype.BoundingBox{1000000, 2000000, 1100000, 2100000}
	hasSpatialIndex := true
	batch.Spatial.HasSpatialIndex = &hasSpatialIndex
	batch.Spatial.IndexName = "geom_idx"

	next, err := transform.TransformBatch(context.Background(), batch)
	if err != nil {
		t.Fatalf("TransformBatch failed: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.calls)
	}
	if provider.sourceCRS != "EPSG:3857" || provider.targetCRS != "EPSG:4326" || provider.geometryColumn != "geom" {
		t.Fatalf("provider args = %#v, want spatial batch args", provider)
	}
	got, ok := next.Rows[0]["geom"].([]byte)
	if !ok {
		t.Fatalf("first geometry type = %T, want []byte EWKB", next.Rows[0]["geom"])
	}
	if string(got) != string([]byte{0x01, 0x02}) {
		t.Fatalf("first geometry = %#v, want provider output bytes", got)
	}
	if next.Rows[1]["geom"] != nil {
		t.Fatalf("second geometry = %#v, want nil", next.Rows[1]["geom"])
	}
	if next.Spatial == nil || next.Spatial.PrimaryCRSRef() != "EPSG:4326" || next.Spatial.PrimaryGeometryName() != "geom" {
		t.Fatalf("spatial info = %#v, want target CRS and geometry column", next.Spatial)
	}
	if next.Spatial.Extent != nil || next.Spatial.HasSpatialIndex != nil || next.Spatial.IndexName != "" {
		t.Fatalf("spatial derived facts = extent %#v index %#v/%q, want cleared after reproject", next.Spatial.Extent, next.Spatial.HasSpatialIndex, next.Spatial.IndexName)
	}
}

func TestFieldMappingTransformPreservesCRSDefinitionForMappedGeometryColumn(t *testing.T) {
	transform, err := newFieldMappingTransform(FieldMappingTransformPlan{
		Mode: FieldMappingModeProject,
		Fields: []FieldMappingFieldPlan{
			{Source: "SmID", Target: "SmID", TargetType: string(datatype.FieldTypeInt)},
			{Source: "SmGeometry", Target: "SmGeometry", TargetType: string(datatype.FieldTypeGeometry)},
		},
	})
	if err != nil {
		t.Fatalf("newFieldMappingTransform failed: %v", err)
	}
	sourceSpatial := datatype.NewSingleGeometrySpatialInfo("SmGeometry", "MultiPolygon", 4549, 2)
	sourceSpatial.GeometryColumns[0].CRSRef = datatype.EPSGCRSRef(4549)
	sourceSpatial.CRSDefinitions = []datatype.CRSDefinition{{
		ID:                 datatype.EPSGCRSRef(4549),
		DefinitionEncoding: datatype.CRSDefinitionEncodingWKT,
		Definition:         `PROJCS["CGCS2000 / 3-degree Gauss-Kruger CM 120E"]`,
		Source:             datatype.CRSDefinitionSourcePostGISSpatialRefSys,
	}}

	nextInfo, nextSpatial, err := transform.TransformTableInfo(&datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "SmID", Type: datatype.FieldTypeInt},
		{Name: "SmGeometry", Type: datatype.FieldTypeGeometry},
	}}, sourceSpatial)
	if err != nil {
		t.Fatalf("TransformTableInfo failed: %v", err)
	}
	if nextInfo.GetField("SmGeometry") == nil {
		t.Fatalf("table info fields = %#v, want mapped geometry field", nextInfo.Fields)
	}
	if nextSpatial == nil || nextSpatial.PrimaryGeometryName() != "SmGeometry" || nextSpatial.PrimaryGeometryType() != "MultiPolygon" {
		t.Fatalf("spatial info = %#v, want mapped MultiPolygon geometry", nextSpatial)
	}
	if nextSpatial.PrimaryCRSRef() != datatype.EPSGCRSRef(4549) {
		t.Fatalf("primary CRS = %q, want EPSG:4549", nextSpatial.PrimaryCRSRef())
	}
	if definition := nextSpatial.CRSDefinitionByID(datatype.EPSGCRSRef(4549)); definition == nil || definition.Definition == "" {
		t.Fatalf("CRS definitions = %#v, want EPSG:4549 WKT definition", nextSpatial.CRSDefinitions)
	}
}

type fakeGeometryBatchReprojecter struct {
	calls          int
	sourceCRS      string
	targetCRS      string
	geometryColumn string
	result         [][]byte
}

func (f *fakeGeometryBatchReprojecter) ReprojectGeometryBatch(_ context.Context, geometries [][]byte, sourceCRS, targetCRS, geometryColumn string) ([][]byte, error) {
	f.calls++
	f.sourceCRS = sourceCRS
	f.targetCRS = targetCRS
	f.geometryColumn = geometryColumn
	return append([][]byte(nil), f.result...), nil
}

func mustWKBGeometry(t *testing.T, geometry geom.T) []byte {
	t.Helper()
	data, err := commonSpatial.GeomToWKB(geometry)
	if err != nil {
		t.Fatalf("GeomToWKB failed: %v", err)
	}
	return data
}
