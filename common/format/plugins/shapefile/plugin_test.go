package shapefile

import (
	"bytes"
	"context"
	"errors"
	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	geojsonformat "github.com/addp/common/format/plugins/geojson"
	"github.com/addp/common/resume"
	"github.com/jonas-p/go-shp"
	"golang.org/x/text/encoding/simplifiedchinese"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDescribeRefsUsesRefFormatFacts(t *testing.T) {
	descriptors := DescribeRefs([]format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef("roads"+extSHP, contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef("roads"+extDBF, roleAttributes), true, false),
		format.NewRelatedRef(contentio.NewRef("roads"+extPRJ, roleProjection), false, false),
		format.NewRelatedRef(contentio.NewRef("roads"+extMetadataXML, roleMetadata), false, false),
	})

	byRole := map[string]format.RefDescriptor{}
	for _, descriptor := range descriptors {
		byRole[descriptor.Role] = descriptor
	}
	if got := byRole["main"].Format; got != format.FormatUnknown {
		t.Fatalf("main ref format = %s, want unknown ref file format", got)
	}
	if got := byRole[roleAttributes].Format; got != format.FormatUnknown {
		t.Fatalf("attributes ref format = %s, want unknown ref file format", got)
	}
	if got := byRole[roleProjection].Format; got != format.FormatText {
		t.Fatalf("projection format = %s, want text", got)
	}
	if got := byRole[roleMetadata].Format; got != format.FormatXML {
		t.Fatalf("metadata format = %s, want xml", got)
	}
}

func TestPluginImplementsOnlyMultiTableProviders(t *testing.T) {
	descriptor := NewPlugin(nil).Descriptor()
	if descriptor.DataType != datatype.Table {
		t.Fatalf("descriptor data type = %q, want table", descriptor.DataType)
	}
	plugin := NewPlugin(nil)
	if _, ok := any(plugin).(format.MultiTableInfoProvider); !ok {
		t.Fatal("shapefile should implement MultiTableInfoProvider")
	}
	if _, ok := any(plugin).(format.MultiTableSampleReader); !ok {
		t.Fatal("shapefile should implement MultiTableSampleReader")
	}
	if _, ok := any(plugin).(format.MultiTableReaderProvider); !ok {
		t.Fatal("shapefile should implement MultiTableReaderProvider")
	}
	if _, ok := any(plugin).(format.TableInfoProvider); ok {
		t.Fatal("shapefile must not implement single TableInfoProvider")
	}
}

func TestSpatialEncodingCapabilityIncludesNativeShape(t *testing.T) {
	t.Parallel()

	capability := NewPlugin(nil).SpatialEncodingCapability()
	if capability.DefaultReadEncoding != format.GeometryEncodingWKT ||
		capability.DefaultWriteEncoding != format.GeometryEncodingWKT {
		t.Fatalf("default encoding = %q/%q, want wkt", capability.DefaultReadEncoding, capability.DefaultWriteEncoding)
	}
	if capability.NativeReadEncoding != format.GeometryEncodingShapefileShape ||
		capability.NativeWriteEncoding != format.GeometryEncodingShapefileShape {
		t.Fatalf("native encoding = %q/%q, want shapefile_shape", capability.NativeReadEncoding, capability.NativeWriteEncoding)
	}
	if !geometryEncodingContains(capability.GeometryReadEncodings, format.GeometryEncodingShapefileShape) {
		t.Fatalf("read encodings = %#v, want shapefile_shape", capability.GeometryReadEncodings)
	}
	if !geometryEncodingContains(capability.GeometryWriteEncodings, format.GeometryEncodingShapefileShape) {
		t.Fatalf("write encodings = %#v, want shapefile_shape", capability.GeometryWriteEncodings)
	}
}

func TestRelatedRefSpecsIncludeProjectionSidecars(t *testing.T) {
	t.Parallel()

	seen := map[string]format.RelatedRefSpec{}
	for _, spec := range RelatedRefSpecs() {
		seen[format.NormalizeExtension(spec.Extension)] = spec
	}
	for _, ext := range []string{extPRJ, extQPJ} {
		spec, ok := seen[ext]
		if !ok {
			t.Fatalf("RelatedRefSpecs() missing %s", ext)
		}
		if spec.Required || spec.Role != roleProjection {
			t.Fatalf("spec %s = %#v, want optional projection sidecar", ext, spec)
		}
	}
}

func TestRelatedRefSpecsIncludeMetadataXMLSidecar(t *testing.T) {
	t.Parallel()
	for _, spec := range RelatedRefSpecs() {
		if format.NormalizeExtension(spec.Extension) != extMetadataXML {
			continue
		}
		if spec.Required || spec.Role != roleMetadata {
			t.Fatalf("metadata spec = %#v, want optional metadata sidecar", spec)
		}
		return
	}
	t.Fatalf("RelatedRefSpecs() missing %s", extMetadataXML)
}

func TestOpenMultiTableWriterWritesReadableShapefile(t *testing.T) {
	plugin := NewPlugin(nil)
	target := contentio.NewRef("exports/cities"+extSHP, contentio.RoleMain)
	refs := format.SameBasenameRelatedRefs(target.Path, RelatedRefSpecs())
	output := newMemoryRefStore()
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}
	opts := &format.WriteOptions{SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0)}

	writer, err := plugin.OpenMultiTableWriter(context.Background(), output, refs, tableInfo, opts)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	err = writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geom": "POINT (120 30)"},
		{"id": 2, "name": "Beta", "geom": "POINT (121 31)"},
	})
	if err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	for _, path := range []string{"exports/cities" + extSHP, "exports/cities" + extSHX, "exports/cities" + extDBF, "exports/cities" + extCPG} {
		if len(output.files[path]) == 0 {
			t.Fatalf("ref %s was not written", path)
		}
	}
	rows, err := plugin.SampleMultiTable(context.Background(), output, refs, 0, 10, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("sample row count = %d, want 2", len(rows))
	}
	if got := rows[0]["NAME"]; got != "Alpha" {
		t.Fatalf("first NAME = %#v, want Alpha", got)
	}
	if got := rows[1]["ID"]; got != int64(2) {
		t.Fatalf("second ID = %#v, want int64(2)", got)
	}
}

func TestOpenMultiTableReaderAndWriterRejectResumeMarker(t *testing.T) {
	plugin := NewPlugin(nil)
	target := contentio.NewRef("exports/cities"+extSHP, contentio.RoleMain)
	refs := format.SameBasenameRelatedRefs(target.Path, RelatedRefSpecs())
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}

	parseOpts := format.DefaultParseOptions()
	parseOpts.ResumeMarker = &resume.Marker{Version: resume.MarkerVersionV1}
	if _, err := plugin.OpenMultiTableReader(context.Background(), newMemoryRefStore(), refs, parseOpts); err == nil {
		t.Fatal("OpenMultiTableReader succeeded with resume marker, want explicit unsupported error")
	}

	writeOpts := format.DefaultWriteOptions()
	writeOpts.ResumeMarker = &resume.Marker{Version: resume.MarkerVersionV1}
	writeOpts.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0)
	if _, err := plugin.OpenMultiTableWriter(context.Background(), newMemoryRefStore(), refs, tableInfo, writeOpts); err == nil {
		t.Fatal("OpenMultiTableWriter succeeded with resume marker, want explicit unsupported error")
	}
}

func TestOpenMultiTableWriterRequiresRefs(t *testing.T) {
	plugin := NewPlugin(nil)
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}

	if _, err := plugin.OpenMultiTableWriter(context.Background(), newMemoryRefStore(), nil, tableInfo, nil); err == nil {
		t.Fatal("OpenMultiTableWriter succeeded without refs")
	}
}

func TestOpenMultiTableWriterRequiresPrimaryRef(t *testing.T) {
	plugin := NewPlugin(nil)
	refs := []format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef("exports/cities"+extDBF, roleAttributes), true, false),
	}
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}

	if _, err := plugin.OpenMultiTableWriter(context.Background(), newMemoryRefStore(), refs, tableInfo, nil); err == nil {
		t.Fatal("OpenMultiTableWriter succeeded without primary ref")
	}
}

func TestOpenMultiTableWriterRejectsCRSDefinitionID(t *testing.T) {
	plugin := NewPlugin(nil)
	target := contentio.NewRef("exports/cities"+extSHP, contentio.RoleMain)
	refs := format.SameBasenameRelatedRefs(target.Path, RelatedRefSpecs())
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}

	writer, err := plugin.OpenMultiTableWriter(context.Background(), newMemoryRefStore(), refs, tableInfo, &format.WriteOptions{
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0),
		ExtraParams: map[string]interface{}{
			format.CRSDefinitionOptionKey: "EPSG:4326",
		},
	})
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed before CRS definition validation: %v", err)
	}
	err = writer.Close(context.Background())
	if err == nil || !strings.Contains(err.Error(), "must contain CRS definition text") {
		t.Fatalf("Close error = %v, want CRS definition text validation", err)
	}
}

type memoryRefStore struct {
	files map[string][]byte
}

func newMemoryRefStore() *memoryRefStore {
	return &memoryRefStore{
		files: map[string][]byte{},
	}
}

func (w *memoryRefStore) Create(ctx context.Context, ref contentio.Ref) (io.WriteCloser, error) {
	return &memoryWriteCloser{onClose: func(data []byte) {
		w.files[ref.Path] = append([]byte(nil), data...)
	}}, nil
}

func (w *memoryRefStore) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	data, ok := w.files[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (w *memoryRefStore) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

type memoryWriteCloser struct {
	bytes.Buffer
	onClose func([]byte)
}

func (w *memoryWriteCloser) Close() error {
	if w.onClose != nil {
		w.onClose(w.Bytes())
	}
	return nil
}

func geometryEncodingContains(values []format.GeometryEncoding, target format.GeometryEncoding) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestShapefileWriterAcceptsGeoJSONReaderEWKBRows(t *testing.T) {
	t.Parallel()

	source := `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[116.4,39.9]},"properties":{"name":"A"}}]}`
	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingEWKB
	reader, err := geojsonformat.NewPlugin(nil).OpenTableReader(context.Background(), bytes.NewBufferString(source), parseOptions)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	rows, err := reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}

	plugin := NewPlugin(nil)
	store := newMemoryRefStore()
	refs := format.SameBasenameRelatedRefs("roads.shp", plugin.RelatedRefSpecs())
	tableInfo := &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "geometry", Type: datatype.FieldTypeGeometry},
		{Name: "name", Type: datatype.FieldTypeString},
	}}
	opts := format.DefaultWriteOptions()
	opts.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 4326, 2)
	writer, err := plugin.OpenMultiTableWriter(context.Background(), store, refs, tableInfo, opts)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), rows); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if len(store.files) == 0 {
		t.Fatal("shapefile writer produced no refs")
	}
}

func TestShapefileWriterAcceptsNativeShapeRows(t *testing.T) {
	t.Parallel()

	plugin := NewPlugin(nil)
	store := newMemoryRefStore()
	refs := format.SameBasenameRelatedRefs("roads.shp", plugin.RelatedRefSpecs())
	tableInfo := &datatype.TableInfo{Fields: []datatype.FieldInfo{
		{Name: "geom", Type: datatype.FieldTypeGeometry},
		{Name: "name", Type: datatype.FieldTypeString},
	}}
	opts := format.DefaultWriteOptions()
	opts.SpatialInfo = datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 2)
	writer, err := plugin.OpenMultiTableWriter(context.Background(), store, refs, tableInfo, opts)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"geom": &shp.Point{X: 116.4, Y: 39.9}, "name": "A"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	parseOptions := format.DefaultParseOptions()
	parseOptions.GeometryEncoding = format.GeometryEncodingShapefileShape
	parseOptions.ExtraParams = map[string]interface{}{"geometry_field": "geom"}
	reader, err := plugin.OpenMultiTableReader(context.Background(), store, refs, parseOptions)
	if err != nil {
		t.Fatalf("OpenMultiTableReader failed: %v", err)
	}
	defer reader.Close(context.Background())
	rows, err := reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got, ok := rows[0]["geom"].(*shp.Point)
	if !ok {
		t.Fatalf("geometry value = %T, want *shp.Point", rows[0]["geom"])
	}
	if got.X != 116.4 || got.Y != 39.9 {
		t.Fatalf("point = %#v, want 116.4/39.9", got)
	}
}

func TestShapefileRegistersOnlyMultiTableProviders(t *testing.T) {
	t.Parallel()

	if _, err := format.GetTableInfoProvider(format.FormatShapefile); err == nil {
		t.Fatal("GetTableInfoProvider(shapefile) succeeded, want no single table info provider")
	}
	if _, err := format.GetTableSampleReader(format.FormatShapefile); err == nil {
		t.Fatal("GetTableSampleReader(shapefile) succeeded, want no single table sample reader")
	}
	if _, err := format.GetMultiTableInfoProvider(format.FormatShapefile); err != nil {
		t.Fatalf("GetMultiTableInfoProvider(shapefile) failed: %v", err)
	}
	if _, err := format.GetMultiTableSampleReader(format.FormatShapefile); err != nil {
		t.Fatalf("GetMultiTableSampleReader(shapefile) failed: %v", err)
	}
	if _, err := format.GetMultiTableReaderProvider(format.FormatShapefile); err != nil {
		t.Fatalf("GetMultiTableReaderProvider(shapefile) failed: %v", err)
	}
	if _, err := format.GetMultiTableWriterProvider(format.FormatShapefile); err != nil {
		t.Fatalf("GetMultiTableWriterProvider(shapefile) failed: %v", err)
	}
}

func TestShapefileCapabilitySnapshotMatchesMultiTableContract(t *testing.T) {
	t.Parallel()

	snapshot, ok := format.GetFormatCapabilitySnapshot(format.FormatShapefile)
	if !ok {
		t.Fatal("expected shapefile capability snapshot")
	}
	if !snapshot.Implementations.MultiTableInfoProvider ||
		!snapshot.Implementations.MultiTableSampleReader ||
		!snapshot.Implementations.MultiTableReader ||
		!snapshot.Implementations.MultiTableWriter {
		t.Fatalf("implementations = %#v, want complete multi table implementations", snapshot.Implementations)
	}
	if snapshot.Implementations.TableInfoProvider ||
		snapshot.Implementations.TableSampleReader ||
		snapshot.Implementations.TableReaderProvider ||
		snapshot.Implementations.TableWriterProvider {
		t.Fatalf("implementations = %#v, shapefile must not register single table implementations", snapshot.Implementations)
	}
}

func TestShapefileSequentialReaderUsesCPGForDBFAttributes(t *testing.T) {
	t.Parallel()

	base := createEncodedPointShapefile(t, "GBK", "北京")
	refReader := newOpenOnlyRefReader(base)
	refs := refReader.refs()
	plugin := NewPlugin(nil)
	tableReader, err := plugin.OpenMultiTableReader(context.Background(), refReader, refs, nil)
	if err != nil {
		t.Fatalf("OpenMultiTableReader() error = %v", err)
	}
	defer tableReader.Close(context.Background())

	rows, err := tableReader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if got := rows[0]["NAME"]; got != "北京" {
		t.Fatalf("decoded row value = %#v, want 北京", got)
	}
}

func TestShapefilePluginUsesCPGForRefSamples(t *testing.T) {
	t.Parallel()

	base := createEncodedPointShapefile(t, "GBK", "北京")
	reader := newLocalRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeMultiTable(context.Background(), reader, refs, nil)
	if err != nil {
		t.Fatalf("DescribeMultiTable() error = %v", err)
	}
	if info.Table == nil || info.Table.Native["encoding"] != "gbk" {
		t.Fatalf("shapefile table native = %#v, want gbk encoding", info.Table.Native)
	}
	if info.FormatInfo["encoding"] != nil {
		t.Fatalf("format info should not contain table native encoding: %#v", info.FormatInfo)
	}

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 0, 10, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if got := rows[0]["NAME"]; got != "北京" {
		t.Fatalf("decoded row value = %#v, want 北京", got)
	}
}

func TestShapefileDescribeCarriesSidecarPRJCRSDefinition(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a"})
	prj := `PROJCS["WGS_1984_UTM_Zone_50N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",117.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
	if err := os.WriteFile(base+extPRJ, []byte(prj), 0o644); err != nil {
		t.Fatalf("write prj failed: %v", err)
	}
	reader := newLocalRefReader(base)
	refs := append(reader.refs(), format.NewRelatedRef(contentio.NewRef(base+extPRJ, roleProjection), false, false))
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeMultiTable(context.Background(), reader, refs, nil)
	if err != nil {
		t.Fatalf("DescribeMultiTable() error = %v", err)
	}
	spatial := info.Spatial
	if spatial == nil || len(spatial.GeometryColumns) != 1 {
		t.Fatalf("spatial info = %#v, want single geometry column", spatial)
	}
	column := spatial.GeometryColumns[0]
	if column.SRID == nil || *column.SRID != 32650 || column.CRSRef != datatype.EPSGCRSRef(32650) {
		t.Fatalf("geometry CRS = srid:%#v ref:%q, want EPSG:32650", column.SRID, column.CRSRef)
	}
	if len(spatial.CRSDefinitions) != 1 {
		t.Fatalf("CRSDefinitions = %#v, want one sidecar PRJ definition", spatial.CRSDefinitions)
	}
	definition := spatial.CRSDefinitions[0]
	if definition.ID != datatype.EPSGCRSRef(32650) ||
		definition.DefinitionEncoding != datatype.CRSDefinitionEncodingESRIWKT ||
		definition.Definition != prj ||
		definition.Source != datatype.CRSDefinitionSourceSidecarPRJ {
		t.Fatalf("CRS definition = %#v, want EPSG:32650 ESRI WKT from sidecar PRJ", definition)
	}
}

func TestShapefilePluginUsesSHXIndexedRefSample(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b", "c"})
	reader := newLocalRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 2, 1, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
	}
	if reader.rangeReads == 0 {
		t.Fatalf("rangeReads = 0, want indexed ref sample path")
	}
	if reader.openReads != 0 {
		t.Fatalf("openReads = %d, want no full ref reads for indexed sample path", reader.openReads)
	}
	if reader.rangeReads > 6 {
		t.Fatalf("rangeReads = %d, want page-level range reads", reader.rangeReads)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if got := rows[0]["NAME"]; got != "c" {
		t.Fatalf("NAME = %#v, want c", got)
	}
	if got := rows[0]["geometry"]; got != "POINT (3 4)" {
		t.Fatalf("geometry = %#v, want POINT (3 4)", got)
	}
}

func TestShapefilePluginUsesSHXIndexedMaterializedSample(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b", "c", "d"})
	reader := newOpenOnlyRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 3, 1, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if got := rows[0]["NAME"]; got != "d" {
		t.Fatalf("NAME = %#v, want d", got)
	}
	if got := rows[0]["geometry"]; got != "POINT (4 5)" {
		t.Fatalf("geometry = %#v, want POINT (4 5)", got)
	}
	if reader.openReads != 4 {
		t.Fatalf("openReads = %d, want one materialization read per ref", reader.openReads)
	}
}

func TestShapefileReadUsesCallGeometryFieldOption(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a"})
	reader := newLocalRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{"geometry_field": "geom"}

	info, err := plugin.DescribeMultiTable(context.Background(), reader, refs, opts)
	if err != nil {
		t.Fatalf("DescribeMultiTable() error = %v", err)
	}
	spatial := info.Spatial
	if spatial == nil || spatial.PrimaryGeometryName() != "geom" {
		t.Fatalf("geometry column = %#v, want geom", spatial)
	}
	if info.Table.Fields[0].Name != "geom" || info.Table.Fields[0].Type != datatype.FieldTypeGeometry {
		t.Fatalf("first field = %#v, want geom geometry field", info.Table.Fields[0])
	}

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 0, 1, opts)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
	}
	if _, ok := rows[0]["geom"]; !ok {
		t.Fatalf("sample row = %#v, want geom field", rows[0])
	}
	if _, ok := rows[0]["geometry"]; ok {
		t.Fatalf("sample row = %#v, did not expect default geometry field", rows[0])
	}
}

func TestShapefileTableReaderUsesCallGeometryFieldOption(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a"})
	refReader := newOpenOnlyRefReader(base)
	refs := refReader.refs()
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{"geometry_field": "geom"}

	tableReader, err := plugin.OpenMultiTableReader(context.Background(), refReader, refs, opts)
	if err != nil {
		t.Fatalf("OpenMultiTableReader() error = %v", err)
	}
	defer tableReader.Close(context.Background())

	spatialProvider, ok := tableReader.(format.TableSpatialInfoProvider)
	if !ok {
		t.Fatal("table reader does not provide spatial info")
	}
	if spatialInfo := spatialProvider.SpatialInfo(); spatialInfo == nil || spatialInfo.PrimaryGeometryName() != "geom" {
		t.Fatalf("reader spatial info = %#v, want geom geometry column", spatialInfo)
	}
	rows, err := tableReader.ReadRows(context.Background(), 1)
	if err != nil {
		t.Fatalf("ReadRows() error = %v", err)
	}
	if _, ok := rows[0]["geom"]; !ok {
		t.Fatalf("row = %#v, want geom field", rows[0])
	}
	if _, ok := rows[0]["geometry"]; ok {
		t.Fatalf("row = %#v, did not expect default geometry field", rows[0])
	}
}

func TestShapefileFieldSelection(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b"})
	reader := newLocalRefReader(base)
	refs := reader.refs()
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"NAME", "geometry"}}

	info, err := plugin.DescribeMultiTable(context.Background(), reader, refs, opts)
	if err != nil {
		t.Fatalf("DescribeMultiTable() error = %v", err)
	}
	if len(info.Table.Fields) != 2 || info.Table.Fields[0].Name != "NAME" || info.Table.Fields[1].Name != "geometry" {
		t.Fatalf("fields = %#v, want NAME,geometry", info.Table.Fields)
	}

	rows, err := plugin.SampleMultiTable(context.Background(), reader, refs, 0, 1, opts)
	if err != nil {
		t.Fatalf("SampleMultiTable() error = %v", err)
	}
	if len(rows) != 1 || len(rows[0]) != 2 || rows[0]["NAME"] != "a" {
		t.Fatalf("rows = %#v, want selected NAME/geometry", rows)
	}

	refReader := newOpenOnlyRefReader(base)
	tableReader, err := plugin.OpenMultiTableReader(context.Background(), refReader, refReader.refs(), opts)
	if err != nil {
		t.Fatalf("OpenMultiTableReader() error = %v", err)
	}
	defer tableReader.Close(context.Background())
	readRows, err := tableReader.ReadRows(context.Background(), 1)
	if err != nil {
		t.Fatalf("ReadRows() error = %v", err)
	}
	if len(readRows) != 1 || len(readRows[0]) != 2 {
		t.Fatalf("read rows = %#v, want selected rows", readRows)
	}
	if _, ok := readRows[0]["NAME"]; !ok {
		t.Fatalf("read rows = %#v, want NAME", readRows)
	}
	if _, ok := readRows[0]["geometry"]; !ok {
		t.Fatalf("read rows = %#v, want geometry", readRows)
	}
}

func TestShapefilePluginDoesNotFallbackWhenIndexedRequiredRefReadFails(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a", "b"})
	reader := newFailingRangeRefReader(base, extDBF)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	if _, err := plugin.SampleMultiTable(context.Background(), reader, refs, 0, 1, nil); err == nil {
		t.Fatal("SampleMultiTable() error = nil, want indexed read error")
	}
	if reader.openReads != 0 {
		t.Fatalf("openReads = %d, want no full ref fallback on indexed read failure", reader.openReads)
	}
}

func TestShapefilePluginReportsMissingRequiredRef(t *testing.T) {
	t.Parallel()

	base := createPointShapefileRows(t, []string{"a"})
	reader := newMissingRefReader(base, extDBF)
	refs := reader.refs()
	plugin := NewPlugin(nil)

	_, err := plugin.DescribeMultiTable(context.Background(), reader, refs, nil)
	if err == nil {
		t.Fatal("DescribeMultiTable() error = nil, want missing required ref error")
	}
	if !strings.Contains(err.Error(), "failed to read required ref") || !strings.Contains(err.Error(), extDBF) {
		t.Fatalf("DescribeMultiTable() error = %v, want missing required .dbf ref", err)
	}
}

func createEncodedPointShapefile(t *testing.T, cpg string, value string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "sample")
	writer, err := shp.Create(base+extSHP, shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	row := writer.Write(&shp.Point{X: 1, Y: 2})
	if err := writer.WriteAttribute(int(row), 0, value); err != nil {
		t.Fatalf("write attribute failed: %v", err)
	}
	writer.Close()
	normalizeGoShpDBFPath(t, base)

	encoded, err := simplifiedchinese.GBK.NewEncoder().String(value)
	if err != nil {
		t.Fatalf("encode GBK failed: %v", err)
	}
	patchFirstDBFAttribute(t, base+extDBF, encoded, 16)
	if err := os.WriteFile(base+extCPG, []byte(cpg), 0o644); err != nil {
		t.Fatalf("write cpg failed: %v", err)
	}
	return base
}

func createPointShapefileRows(t *testing.T, values []string) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), "rows")
	writer, err := shp.Create(base+extSHP, shp.POINT)
	if err != nil {
		t.Fatalf("create shapefile failed: %v", err)
	}
	writer.SetFields([]shp.Field{shp.StringField("NAME", 16)})
	for i, value := range values {
		row := writer.Write(&shp.Point{X: float64(i + 1), Y: float64(i + 2)})
		if err := writer.WriteAttribute(int(row), 0, value); err != nil {
			t.Fatalf("write attribute failed: %v", err)
		}
	}
	writer.Close()
	normalizeGoShpDBFPath(t, base)
	return base
}

func normalizeGoShpDBFPath(t *testing.T, base string) {
	t.Helper()

	if _, err := os.Stat(base + "dbf"); err == nil {
		if err := os.Rename(base+"dbf", base+extDBF); err != nil {
			t.Fatalf("rename dbf failed: %v", err)
		}
	}
}

func patchFirstDBFAttribute(t *testing.T, path string, value string, width int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read dbf failed: %v", err)
	}
	if len(data) < 33 {
		t.Fatalf("dbf too small: %d", len(data))
	}
	headerLength := int(data[8]) | int(data[9])<<8
	if len(data) < headerLength+1+width {
		t.Fatalf("dbf length = %d, need at least %d", len(data), headerLength+1+width)
	}
	field := make([]byte, width)
	copy(field, []byte(value))
	for i := len(value); i < width; i++ {
		field[i] = ' '
	}
	copy(data[headerLength+1:headerLength+1+width], field)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write dbf failed: %v", err)
	}
}

type localRefReader struct {
	base       string
	rangeReads int
	openReads  int
}

func newLocalRefReader(base string) *localRefReader {
	return &localRefReader{base: base}
}

func (r *localRefReader) refs() []format.RelatedRef {
	return []format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef(r.base+extSHP, contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef(r.base+extSHX, roleIndex), true, false),
		format.NewRelatedRef(contentio.NewRef(r.base+extDBF, roleAttributes), true, false),
		format.NewRelatedRef(contentio.NewRef(r.base+extCPG, roleEncoding), false, false),
	}
}

func (r *localRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r *localRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	r.openReads++
	return os.Open(r.base + filepath.Ext(ref.Path))
}

func (r *localRefReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	r.rangeReads++
	file, err := os.Open(r.base + filepath.Ext(ref.Path))
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(file, length),
		Closer: file,
	}, nil
}

type openOnlyRefReader struct {
	base      string
	openReads int
}

func newOpenOnlyRefReader(base string) *openOnlyRefReader {
	return &openOnlyRefReader{base: base}
}

func (r *openOnlyRefReader) refs() []format.RelatedRef {
	return []format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef(r.base+extSHP, contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef(r.base+extSHX, roleIndex), true, false),
		format.NewRelatedRef(contentio.NewRef(r.base+extDBF, roleAttributes), true, false),
		format.NewRelatedRef(contentio.NewRef(r.base+extCPG, roleEncoding), false, false),
	}
}

func (r *openOnlyRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r *openOnlyRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	r.openReads++
	return os.Open(r.base + filepath.Ext(ref.Path))
}

type failingRangeRefReader struct {
	*localRefReader
	failExt string
}

func newFailingRangeRefReader(base string, failExt string) *failingRangeRefReader {
	return &failingRangeRefReader{
		localRefReader: newLocalRefReader(base),
		failExt:        failExt,
	}
}

func (r *failingRangeRefReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(ref.Path), r.failExt) {
		r.rangeReads++
		return nil, contentio.ErrContentNotFound
	}
	return r.localRefReader.OpenRange(ctx, ref, offset, length)
}

type missingRefReader struct {
	*localRefReader
	missingExt string
}

func newMissingRefReader(base string, missingExt string) *missingRefReader {
	return &missingRefReader{
		localRefReader: newLocalRefReader(base),
		missingExt:     missingExt,
	}
}

func (r *missingRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if strings.EqualFold(filepath.Ext(ref.Path), r.missingExt) {
		r.openReads++
		return nil, errors.New("ref missing")
	}
	return r.localRefReader.Open(ctx, ref)
}
