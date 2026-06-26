package executor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	geojsonformat "github.com/addp/common/format/plugins/geojson"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
	commonSpatial "github.com/addp/common/spatial"
	"github.com/twpayne/go-geom"
)

const testWebMercatorCRSDefinition = `PROJCS["WGS 84 / Pseudo-Mercator",GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563]],PRIMEM["Greenwich",0],UNIT["degree",0.0174532925199433]],PROJECTION["Mercator_1SP"],PARAMETER["central_meridian",0],PARAMETER["scale_factor",1],PARAMETER["false_easting",0],PARAMETER["false_northing",0],UNIT["metre",1],AXIS["X",EAST],AXIS["Y",NORTH],AUTHORITY["EPSG","3857"]]`

func TestTableTransferExecutorReprojectsShapefileEWKBBatchBeforeGeoJSONWrite(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	sourcePath := engineplugin.FileItemPath(7, "imports/roads.shp")
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	sourceWriter := contentadapter.NewWriter(source, nil, sourcePath, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(sourcePath.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), sourceWriter, refs, spatialTableInfo("geom"), &format.WriteOptions{
		Encoding:    "utf-8",
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 2),
		ExtraParams: map[string]interface{}{
			"geometry_field":              "geom",
			format.CRSDefinitionOptionKey: testWebMercatorCRSDefinition,
		},
	})
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "geom": "POINT (1113194.9079327357 0)"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(context.Background()); err != nil {
		t.Fatalf("Close shapefile writer failed: %v", err)
	}

	targetGeometry := mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{10, 0}), 4326)
	output := &fakeContentWriter{}
	reprojecter := &fakeGeometryBatchReprojecter{result: [][]byte{targetGeometry}}
	exec := &TableTransferExecutor{
		SourceContentReader:       source,
		SourceMultiReadProvider:   shapefilePlugin,
		TargetContentWriter:       output,
		TargetTableWriterProvider: geojsonformat.NewPlugin(nil),
		GeometryBatchReprojecter:  reprojecter,
	}
	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	parseOptions.CRSDefinition = testWebMercatorCRSDefinition
	parseOptions.ExtraParams = map[string]interface{}{"geometry_field": "geom"}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:         TableEndpointEncoded,
			Path:         sourcePath,
			Format:       format.FormatShapefile,
			ParseOptions: parseOptions,
			RelatedRefs:  refs,
		},
		Target: TableTargetPlan{
			Kind:   TableEndpointEncoded,
			Format: format.FormatGeoJSON,
			FormatOptions: &format.WriteOptions{
				ExtraParams: map[string]interface{}{"geometry_field": "geom"},
			},
		},
		Transforms: []TableTransformPlan{{
			Type: "spatial_reproject",
			SpatialReproject: &SpatialReprojectTransformPlan{
				GeometryColumn: "geom",
				SourceCRS:      "EPSG:3857",
				TargetCRS:      "EPSG:4326",
				Reproject:      true,
			},
		}},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want one transferred row", metrics)
	}
	if reprojecter.calls != 1 {
		t.Fatalf("reprojecter calls = %d, want one shapefile geometry batch call", reprojecter.calls)
	}
	feature := firstGeoJSONFeature(t, output.buf.Bytes())
	geometry := feature["geometry"].(map[string]interface{})
	coords := geometry["coordinates"].([]interface{})
	if coords[0].(float64) != 10 || coords[1].(float64) != 0 {
		t.Fatalf("GeoJSON coordinates = %#v, want [10, 0]", coords)
	}
}

func TestTableTransferExecutorReprojectsSpatialBatchBeforeGeoJSONWrite(t *testing.T) {
	sourceGeometry := mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{1113194.9079327357, 0}), 3857)
	targetGeometry := mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{10, 0}), 4326)
	reader := spatialBatchReader("geom", 3857, []map[string]interface{}{
		{"id": int64(1), "geom": sourceGeometry},
	})
	writer := &fakeContentWriter{}
	reprojecter := &fakeGeometryBatchReprojecter{
		result: [][]byte{targetGeometry},
	}
	exec := &TableTransferExecutor{
		SourceNativeReader:        reader,
		TargetContentWriter:       writer,
		TargetTableWriterProvider: geojsonformat.NewPlugin(nil),
		GeometryBatchReprojecter:  reprojecter,
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:        TableEndpointNative,
			TableInfo:   spatialTableInfo("geom"),
			SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 3857, 2),
		},
		Target: TableTargetPlan{
			Kind:   TableEndpointEncoded,
			Format: format.FormatGeoJSON,
			FormatOptions: &format.WriteOptions{
				ExtraParams: map[string]interface{}{"geometry_field": "geom"},
			},
		},
		Transforms: []TableTransformPlan{{
			Type: "spatial_reproject",
			SpatialReproject: &SpatialReprojectTransformPlan{
				GeometryColumn: "geom",
				SourceCRS:      "EPSG:3857",
				TargetCRS:      "EPSG:4326",
				Reproject:      true,
			},
		}},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want one transferred row", metrics)
	}
	if reprojecter.calls != 1 || reprojecter.sourceCRS != "EPSG:3857" || reprojecter.targetCRS != "EPSG:4326" {
		t.Fatalf("reprojecter = %#v, want one EPSG:3857 -> EPSG:4326 call", reprojecter)
	}
	feature := firstGeoJSONFeature(t, writer.buf.Bytes())
	geometry := feature["geometry"].(map[string]interface{})
	coords := geometry["coordinates"].([]interface{})
	if coords[0].(float64) != 10 || coords[1].(float64) != 0 {
		t.Fatalf("GeoJSON coordinates = %#v, want [10, 0]", coords)
	}
	if feature["id"].(float64) != 1 {
		t.Fatalf("GeoJSON feature = %#v, want top-level id=1", feature)
	}
}

func TestTableTransferExecutorWrites4326EWKBToGeoJSONWithoutReproject(t *testing.T) {
	sourceGeometry := mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{120, 30}), 4326)
	reader := spatialBatchReader("geom", 4326, []map[string]interface{}{
		{"id": int64(1), "geom": sourceGeometry},
	})
	writer := &fakeContentWriter{}
	reprojecter := &fakeGeometryBatchReprojecter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:        reader,
		TargetContentWriter:       writer,
		TargetTableWriterProvider: geojsonformat.NewPlugin(nil),
		GeometryBatchReprojecter:  reprojecter,
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:        TableEndpointNative,
			TableInfo:   spatialTableInfo("geom"),
			SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2),
		},
		Target: TableTargetPlan{
			Kind:   TableEndpointEncoded,
			Format: format.FormatGeoJSON,
			FormatOptions: &format.WriteOptions{
				ExtraParams: map[string]interface{}{"geometry_field": "geom"},
			},
		},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if reprojecter.calls != 0 {
		t.Fatalf("reprojecter calls = %d, want no call for EPSG:4326 source", reprojecter.calls)
	}
	feature := firstGeoJSONFeature(t, writer.buf.Bytes())
	geometry := feature["geometry"].(map[string]interface{})
	coords := geometry["coordinates"].([]interface{})
	if coords[0].(float64) != 120 || coords[1].(float64) != 30 {
		t.Fatalf("GeoJSON coordinates = %#v, want [120, 30]", coords)
	}
}

func spatialBatchReader(geometryColumn string, srid int, rows []map[string]interface{}) *fakeBatchReader {
	return &fakeBatchReader{
		batches: []*engineplugin.BatchData{{
			Fields:  spatialTableInfo(geometryColumn).Fields,
			Rows:    rows,
			Spatial: datatype.NewSingleGeometrySpatialInfo(geometryColumn, "Point", srid, 2),
		}},
	}
}

func spatialTableInfo(geometryColumn string) *datatype.TableInfo {
	return &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "id", Type: datatype.FieldTypeInt},
		{Name: geometryColumn, Type: datatype.FieldTypeGeometry},
	}}
}

func mustEWKBGeometry(t *testing.T, geometry geom.T, srid int) []byte {
	t.Helper()
	data, err := commonSpatial.GeomToEWKB(geometry, srid)
	if err != nil {
		t.Fatalf("GeomToEWKB failed: %v", err)
	}
	return data
}

func firstGeoJSONFeature(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var collection map[string]interface{}
	if err := json.Unmarshal(data, &collection); err != nil {
		t.Fatalf("unmarshal GeoJSON failed: %v; output=%s", err, string(data))
	}
	features, ok := collection["features"].([]interface{})
	if !ok || len(features) != 1 {
		t.Fatalf("features = %#v, want one feature", collection["features"])
	}
	feature, ok := features[0].(map[string]interface{})
	if !ok {
		t.Fatalf("feature = %#v, want object", features[0])
	}
	return feature
}
