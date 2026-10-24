package executor

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/kml"
	"github.com/twpayne/go-geom"
	"strings"
	"testing"
)

func TestKMLExportPipelineReprojectsEWKBAndKeepsAllBatches(t *testing.T) {
	reader := spatialBatchReader("route_shape", 3857, []map[string]interface{}{
		{"id": int64(1), "route_shape": mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{1113194.9079327357, 0}), 3857)},
		{"id": int64(2), "route_shape": mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{1113194.9079327357, 0}), 3857)},
	})
	first := reader.batches[0]
	second := *first
	second.Rows = first.Rows[1:]
	second.Offset = 1
	first.Rows = first.Rows[:1]
	reader.batches = append(reader.batches, &second)
	output := &fakeContentWriter{}
	reprojecter := &fakeGeometryBatchReprojecter{result: [][]byte{mustEWKBGeometry(t, geom.NewPointFlat(geom.XY, []float64{10, 0}), 4326)}}
	exec := &TableTransferExecutor{SourceNativeReader: reader, TargetContentWriter: output, TargetTableWriterProvider: kml.NewPlugin(), GeometryBatchReprojecter: reprojecter}
	metrics, err := exec.Execute(t.Context(), TableTransferPlan{Source: TableSourcePlan{Kind: TableEndpointNative, TableInfo: spatialTableInfo("route_shape"), SpatialInfo: datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 3857, 2)}, Target: TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatKML}, Transforms: []TableTransformPlan{{Type: "spatial_reproject", SpatialReproject: &SpatialReprojectTransformPlan{GeometryColumn: "route_shape", SourceCRS: "EPSG:3857", TargetCRS: "EPSG:4326", Reproject: true}}}, BatchSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if metrics.RecordsWritten != 2 || metrics.Batches != 2 || reprojecter.calls != 2 {
		t.Fatalf("metrics=%#v reprojecter=%#v", metrics, reprojecter)
	}
	if strings.Count(output.buf.String(), "<coordinates>10,0</coordinates>") != 2 || !strings.HasSuffix(output.buf.String(), "</kml>") {
		t.Fatal(output.buf.String())
	}
}
