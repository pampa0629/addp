package spatial_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/spatial"
	"github.com/gogama/flatgeobuf/flatgeobuf"
	"github.com/gogama/flatgeobuf/flatgeobuf/flat"
	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/twpayne/go-geom"
)

type testFlatGeobufFeatureReader struct {
	features []spatial.FlatGeobufFeature
	index    int
}

func TestFlatGeobufBatchFeatureReaderAppliesRowBudgetAcrossBufferedAndFetchedRows(t *testing.T) {
	t.Parallel()

	fields := []datatype.FieldInfo{
		{Name: "shape", Type: datatype.FieldTypeGeometry},
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "active", Type: datatype.FieldTypeBool},
		{Name: "count", Type: datatype.FieldTypeBigInt},
		{Name: "ratio", Type: datatype.FieldTypeDecimal},
		{Name: "payload", Type: datatype.FieldTypeBytes},
		{Name: "metadata", Type: datatype.FieldTypeJSON},
	}
	spatialInfo := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2)
	bufferedRows := []map[string]interface{}{
		{"SHAPE": []byte{1}, "name": "buffered", "active": true, "count": int64(1)},
		{"shape": nil, "name": "null geometry"},
	}
	var requestedLimits []int
	reader, opts := spatial.NewFlatGeobufBatchFeatureReader(
		func(_ context.Context, limit int) ([]map[string]interface{}, error) {
			requestedLimits = append(requestedLimits, limit)
			return []map[string]interface{}{
				{"shape": []byte{2}, "name": "fetched", "ratio": 1.5, "metadata": map[string]interface{}{"kind": "test"}},
				{"shape": []byte{3}, "name": "last", "payload": []byte("value")},
				{"shape": []byte{4}, "name": "over budget"},
			}, nil
		},
		bufferedRows,
		"shape",
		fields,
		spatialInfo,
		4,
	)

	if opts.SRID != 4326 || opts.GeometryType != "Point" || opts.DefaultEncoding != string(spatial.GeometryEncodingEWKB) {
		t.Fatalf("FlatGeobuf options = %+v", opts)
	}
	wantColumnTypes := map[string]spatial.FlatGeobufPropertyType{
		"name":     spatial.FlatGeobufPropertyString,
		"active":   spatial.FlatGeobufPropertyBool,
		"count":    spatial.FlatGeobufPropertyInt64,
		"ratio":    spatial.FlatGeobufPropertyFloat64,
		"payload":  spatial.FlatGeobufPropertyBinary,
		"metadata": spatial.FlatGeobufPropertyJSON,
	}
	if len(opts.Columns) != len(wantColumnTypes) {
		t.Fatalf("FlatGeobuf columns = %+v, want %d properties", opts.Columns, len(wantColumnTypes))
	}
	for _, column := range opts.Columns {
		if want := wantColumnTypes[column.Name]; column.Type != want {
			t.Fatalf("column %q type = %q, want %q", column.Name, column.Type, want)
		}
	}

	var names []string
	for {
		feature, err := reader.NextFlatGeobufFeature(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextFlatGeobufFeature() error = %v", err)
		}
		if feature.GeometryEncoding != string(spatial.GeometryEncodingEWKB) || feature.GeometrySRID != 4326 {
			t.Fatalf("feature geometry contract = %+v", feature)
		}
		if _, ok := feature.Properties["shape"]; ok {
			t.Fatalf("feature properties unexpectedly include geometry: %+v", feature.Properties)
		}
		names = append(names, feature.Properties["name"].(string))
	}
	if strings.Join(names, ",") != "buffered,fetched,last" {
		t.Fatalf("feature names = %v", names)
	}
	if len(requestedLimits) != 1 || requestedLimits[0] != 2 {
		t.Fatalf("requested batch limits = %v, want [2]", requestedLimits)
	}
}

func TestFlatGeobufBatchFeatureReaderCapsInitialBufferAtRowLimit(t *testing.T) {
	t.Parallel()

	readCalls := 0
	reader, _ := spatial.NewFlatGeobufBatchFeatureReader(
		func(context.Context, int) ([]map[string]interface{}, error) {
			readCalls++
			return nil, nil
		},
		[]map[string]interface{}{
			{"shape": []byte{1}, "name": "first"},
			{"shape": []byte{2}, "name": "over budget"},
		},
		"shape",
		[]datatype.FieldInfo{
			{Name: "shape", Type: datatype.FieldTypeGeometry},
			{Name: "name", Type: datatype.FieldTypeString},
		},
		datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2),
		1,
	)

	feature, err := reader.NextFlatGeobufFeature(context.Background())
	if err != nil || feature.Properties["name"] != "first" {
		t.Fatalf("first feature = %+v, error = %v", feature, err)
	}
	if _, err := reader.NextFlatGeobufFeature(context.Background()); err != io.EOF {
		t.Fatalf("second read error = %v, want io.EOF", err)
	}
	if readCalls != 0 {
		t.Fatalf("readBatch calls = %d, want 0", readCalls)
	}
}

func (r *testFlatGeobufFeatureReader) NextFlatGeobufFeature(context.Context) (*spatial.FlatGeobufFeature, error) {
	if r.index >= len(r.features) {
		return nil, nil
	}
	feature := r.features[r.index]
	r.index++
	return &feature, nil
}

func TestWriteFlatGeobufWritesEWKBFeatures(t *testing.T) {
	t.Parallel()

	point := geom.NewPointFlat(geom.XY, []float64{116.4, 39.9})
	ewkbValue, err := spatial.GeomToEWKB(point, 4326)
	if err != nil {
		t.Fatalf("GeomToEWKB() error = %v", err)
	}
	source := &testFlatGeobufFeatureReader{features: []spatial.FlatGeobufFeature{
		{
			Geometry: ewkbValue,
			Properties: map[string]interface{}{
				"name":   "Beijing",
				// JSON-backed workflow batches decode integer JSON numbers as float64.
				"rank":   float64(1),
				"area":   "43854.25",
				"active": true,
			},
		},
	}}

	var output bytes.Buffer
	err = spatial.WriteFlatGeobuf(context.Background(), &output, source, spatial.FlatGeobufOptions{
		Name:         "cities",
		SRID:         4326,
		GeometryType: "Point",
		Columns: []spatial.FlatGeobufColumn{
			{Name: "name", Type: spatial.FlatGeobufPropertyString},
			{Name: "rank", Type: spatial.FlatGeobufPropertyInt64},
			{Name: "area", Type: spatial.FlatGeobufPropertyFloat64},
			{Name: "active", Type: spatial.FlatGeobufPropertyBool},
		},
	})
	if err != nil {
		t.Fatalf("WriteFlatGeobuf() error = %v", err)
	}
	if output.Len() == 0 {
		t.Fatal("WriteFlatGeobuf() wrote empty output")
	}

	reader := flatgeobuf.NewFileReader(bytes.NewReader(output.Bytes()))
	defer reader.Close()
	header, err := reader.Header()
	if err != nil {
		t.Fatalf("reader.Header() error = %v", err)
	}
	if string(header.Name()) != "cities" {
		t.Fatalf("header name = %q, want cities", header.Name())
	}
	if header.GeometryType() != flat.GeometryTypePoint {
		t.Fatalf("geometry type = %s, want Point", header.GeometryType())
	}
	if header.IndexNodeSize() != 0 {
		t.Fatalf("index node size = %d, want 0", header.IndexNodeSize())
	}
	if crs := header.Crs(nil); crs == nil || crs.Code() != 4326 || string(crs.Org()) != "EPSG" {
		t.Fatalf("CRS = %#v, want EPSG:4326", crs)
	}

	features, err := reader.DataRem()
	if err != nil && err != io.EOF {
		t.Fatalf("reader.DataRem() error = %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("features length = %d, want 1", len(features))
	}
	var geometry flat.Geometry
	if features[0].Geometry(&geometry) == nil {
		t.Fatal("feature geometry is nil")
	}
	if geometry.Type() != flat.GeometryTypePoint {
		t.Fatalf("feature geometry type = %s, want Point", geometry.Type())
	}
	if geometry.XyLength() != 2 || geometry.Xy(0) != 116.4 || geometry.Xy(1) != 39.9 {
		t.Fatalf("feature xy = [%v,%v], length=%d", geometry.Xy(0), geometry.Xy(1), geometry.XyLength())
	}

	propReader := flatgeobuf.NewPropReader(bytes.NewReader(features[0].PropertiesBytes()))
	props, err := propReader.ReadSchema(header)
	if err != nil {
		t.Fatalf("ReadSchema() error = %v", err)
	}
	if len(props) != 4 {
		t.Fatalf("props length = %d, want 4", len(props))
	}
	if string(props[0].Col.Name()) != "name" || props[0].Value != "Beijing" {
		t.Fatalf("name prop = %#v", props[0])
	}
	if string(props[1].Col.Name()) != "rank" || props[1].Value != int64(1) {
		t.Fatalf("rank prop = %#v", props[1])
	}
	if string(props[2].Col.Name()) != "area" || props[2].Value != 43854.25 {
		t.Fatalf("area prop = %#v", props[2])
	}
	if string(props[3].Col.Name()) != "active" || props[3].Value != true {
		t.Fatalf("active prop = %#v", props[3])
	}
}

func TestWriteFlatGeobufPromotesPolygonFeatureForMultiPolygonHeader(t *testing.T) {
	t.Parallel()

	polygon := geom.NewPolygonFlat(geom.XY, []float64{
		0, 0,
		10, 0,
		10, 10,
		0, 10,
		0, 0,
		2, 2,
		8, 2,
		8, 8,
		2, 8,
		2, 2,
	}, []int{10, 20})
	ewkbValue, err := spatial.GeomToEWKB(polygon, 4326)
	if err != nil {
		t.Fatalf("GeomToEWKB() error = %v", err)
	}
	source := &testFlatGeobufFeatureReader{features: []spatial.FlatGeobufFeature{
		{
			Geometry: ewkbValue,
			Properties: map[string]interface{}{
				"name": "with-hole",
			},
		},
	}}

	var output bytes.Buffer
	err = spatial.WriteFlatGeobuf(context.Background(), &output, source, spatial.FlatGeobufOptions{
		Name:         "polygons",
		SRID:         4326,
		GeometryType: "MultiPolygon",
		Columns: []spatial.FlatGeobufColumn{
			{Name: "name", Type: spatial.FlatGeobufPropertyString},
		},
	})
	if err != nil {
		t.Fatalf("WriteFlatGeobuf() error = %v", err)
	}

	reader := flatgeobuf.NewFileReader(bytes.NewReader(output.Bytes()))
	defer reader.Close()
	header, err := reader.Header()
	if err != nil {
		t.Fatalf("reader.Header() error = %v", err)
	}
	if header.GeometryType() != flat.GeometryTypeMultiPolygon {
		t.Fatalf("header geometry type = %s, want MultiPolygon", header.GeometryType())
	}
	features, err := reader.DataRem()
	if err != nil && err != io.EOF {
		t.Fatalf("reader.DataRem() error = %v", err)
	}
	if len(features) != 1 {
		t.Fatalf("features length = %d, want 1", len(features))
	}
	var geometry flat.Geometry
	if features[0].Geometry(&geometry) == nil {
		t.Fatal("feature geometry is nil")
	}
	if geometry.Type() != flat.GeometryTypeMultiPolygon {
		t.Fatalf("feature geometry type = %s, want MultiPolygon", geometry.Type())
	}
	if geometry.PartsLength() != 1 {
		t.Fatalf("feature parts length = %d, want 1", geometry.PartsLength())
	}
	var part flat.Geometry
	if !geometry.Parts(&part, 0) {
		t.Fatal("feature part[0] is nil")
	}
	if part.Type() != flat.GeometryTypePolygon {
		t.Fatalf("part geometry type = %s, want Polygon", part.Type())
	}
	if part.XyLength() != 20 {
		t.Fatalf("part xy length = %d, want 20", part.XyLength())
	}
	if part.EndsLength() != 2 || part.Ends(0) != 5 || part.Ends(1) != 10 {
		t.Fatalf("part ends = [%d,%d], length=%d; want [5,10]", part.Ends(0), part.Ends(1), part.EndsLength())
	}
}

func TestWriteFlatGeobufAlignsFeatureDataForArrayReaders(t *testing.T) {
	t.Parallel()

	output := writeTestMultiPolygonFlatGeobuf(t)
	dataStart := flatGeobufDataStart(t, output)
	if dataStart%flatbuffers.SizeFloat64 != 0 {
		t.Fatalf("data section starts at byte %d; want %d-byte alignment for JS array deserializers", dataStart, flatbuffers.SizeFloat64)
	}
	featureStart := dataStart
	xyOffset := firstFeatureXYVectorOffset(t, output, featureStart)
	if xyOffset%flatbuffers.SizeFloat64 != 0 {
		t.Fatalf("feature xy vector starts at byte %d; want %d-byte alignment for Float64Array", xyOffset, flatbuffers.SizeFloat64)
	}
}

func TestWriteFlatGeobufMaintainsAlignmentAcrossVariableHeadersAndFeatures(t *testing.T) {
	t.Parallel()

	wktBase := `PROJCS["WGS_1984_UTM_Zone_50N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",117.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
	for i := 0; i < 16; i++ {
		i := i
		t.Run(fmt.Sprintf("variant_%02d", i), func(t *testing.T) {
			t.Parallel()

			columns := []spatial.FlatGeobufColumn{
				{Name: "id", Type: spatial.FlatGeobufPropertyInt64},
				{Name: "name", Type: spatial.FlatGeobufPropertyString},
				{Name: "area", Type: spatial.FlatGeobufPropertyFloat64},
			}
			for j := 0; j < i%7; j++ {
				columns = append(columns, spatial.FlatGeobufColumn{
					Name: fmt.Sprintf("extra_%02d_%s", j, strings.Repeat("x", (i+j)%5)),
					Type: spatial.FlatGeobufPropertyString,
				})
			}
			output := writeTestMultiPolygonFlatGeobufWithOptions(t, spatial.FlatGeobufOptions{
				Name:         "quick_view",
				SRID:         32650,
				CRSName:      "EPSG:32650",
				CRSWKT:       wktBase + strings.Repeat(" ", i),
				GeometryType: "MultiPolygon",
				Columns:      columns,
			}, []map[string]interface{}{
				{"id": int64(1), "name": strings.Repeat("a", i), "area": 100.5},
				{"id": int64(2), "name": strings.Repeat("b", i+3), "area": 200.5},
			})

			dataStart := flatGeobufDataStart(t, output)
			if dataStart%flatbuffers.SizeFloat64 != 0 {
				t.Fatalf("data section starts at byte %d; want %d-byte alignment", dataStart, flatbuffers.SizeFloat64)
			}
			for _, featureStart := range flatGeobufFeatureStarts(t, output, dataStart) {
				if featureStart%flatbuffers.SizeFloat64 != 0 {
					t.Fatalf("feature starts at byte %d; want %d-byte alignment", featureStart, flatbuffers.SizeFloat64)
				}
				xyOffset := firstFeatureXYVectorOffset(t, output, featureStart)
				if xyOffset%flatbuffers.SizeFloat64 != 0 {
					t.Fatalf("feature xy vector starts at byte %d; want %d-byte alignment", xyOffset, flatbuffers.SizeFloat64)
				}
			}
		})
	}
}

func TestWriteFlatGeobufIsReadableByFrontendArrayDeserializer(t *testing.T) {
	t.Parallel()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not available")
	}
	modulePath, err := filepath.Abs("../manager/frontend/node_modules/flatgeobuf/lib/mjs/geojson.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(modulePath); err != nil {
		t.Skipf("frontend flatgeobuf package is not available: %v", err)
	}

	fgbPath := filepath.Join(t.TempDir(), "multipolygon.fgb")
	if err := os.WriteFile(fgbPath, writeTestMultiPolygonFlatGeobuf(t), 0o600); err != nil {
		t.Fatalf("write test FlatGeobuf fixture: %v", err)
	}
	moduleURL := (&url.URL{Scheme: "file", Path: modulePath}).String()
	script := fmt.Sprintf(`
import { deserialize } from %q
import fs from 'node:fs/promises'

const bytes = new Uint8Array(await fs.readFile(process.argv[1]))
const features = []
for await (const feature of deserialize(bytes)) {
  features.push(feature)
}
if (features.length !== 1) {
  throw new Error("feature length = " + features.length + ", want 1")
}
const geometry = features[0].geometry
if (!geometry || geometry.type !== "MultiPolygon") {
  throw new Error("geometry type = " + (geometry && geometry.type) + ", want MultiPolygon")
}
if (!geometry.coordinates?.[0]?.[0]?.length) {
  throw new Error("decoded MultiPolygon coordinates are empty")
}
`, moduleURL)
	cmd := exec.Command(node, "--input-type=module", "-", fgbPath)
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("frontend flatgeobuf array deserialize failed: %v\n%s", err, output)
	}
}

func writeTestMultiPolygonFlatGeobuf(t *testing.T) []byte {
	t.Helper()
	return writeTestMultiPolygonFlatGeobufWithOptions(t, spatial.FlatGeobufOptions{
		Name:         "quick_view",
		SRID:         32650,
		CRSName:      "EPSG:32650",
		CRSWKT:       `PROJCS["WGS_1984_UTM_Zone_50N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",117.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`,
		GeometryType: "MultiPolygon",
		Columns: []spatial.FlatGeobufColumn{
			{Name: "id", Type: spatial.FlatGeobufPropertyInt64},
			{Name: "name", Type: spatial.FlatGeobufPropertyString},
			{Name: "area", Type: spatial.FlatGeobufPropertyFloat64},
		},
	}, []map[string]interface{}{
		{"id": int64(1), "name": "with-hole", "area": 100.5},
	})
}

func writeTestMultiPolygonFlatGeobufWithOptions(t *testing.T, opts spatial.FlatGeobufOptions, properties []map[string]interface{}) []byte {
	t.Helper()
	polygon := geom.NewPolygonFlat(geom.XY, []float64{
		0, 0,
		10, 0,
		10, 10,
		0, 10,
		0, 0,
		2, 2,
		8, 2,
		8, 8,
		2, 8,
		2, 2,
	}, []int{10, 20})
	ewkbValue, err := spatial.GeomToEWKB(polygon, 4326)
	if err != nil {
		t.Fatalf("GeomToEWKB() error = %v", err)
	}
	features := make([]spatial.FlatGeobufFeature, 0, len(properties))
	for _, props := range properties {
		features = append(features, spatial.FlatGeobufFeature{
			Geometry:   ewkbValue,
			Properties: props,
		})
	}
	source := &testFlatGeobufFeatureReader{features: features}

	var output bytes.Buffer
	err = spatial.WriteFlatGeobuf(context.Background(), &output, source, opts)
	if err != nil {
		t.Fatalf("WriteFlatGeobuf() error = %v", err)
	}
	return output.Bytes()
}

func flatGeobufDataStart(t *testing.T, data []byte) int {
	t.Helper()
	if len(data) < 12 {
		t.Fatalf("FlatGeobuf data length = %d, want at least 12", len(data))
	}
	headerLength := int(binary.LittleEndian.Uint32(data[8:12]))
	dataStart := 8 + flatbuffers.SizeUint32 + headerLength
	if dataStart > len(data) {
		t.Fatalf("header length = %d exceeds data length %d", headerLength, len(data))
	}
	return dataStart
}

func flatGeobufFeatureStarts(t *testing.T, data []byte, dataStart int) []int {
	t.Helper()
	starts := []int{}
	for offset := dataStart; offset < len(data); {
		if offset+flatbuffers.SizeUint32 > len(data) {
			t.Fatalf("feature length at offset %d exceeds data length %d", offset, len(data))
		}
		starts = append(starts, offset)
		featureLength := int(binary.LittleEndian.Uint32(data[offset : offset+flatbuffers.SizeUint32]))
		if featureLength < flatbuffers.SizeUOffsetT {
			t.Fatalf("feature length = %d at offset %d, want at least %d", featureLength, offset, flatbuffers.SizeUOffsetT)
		}
		offset += flatbuffers.SizeUint32 + featureLength
	}
	return starts
}

func firstFeatureXYVectorOffset(t *testing.T, data []byte, featureStart int) int {
	t.Helper()
	if featureStart+flatbuffers.SizeUint32 > len(data) {
		t.Fatalf("feature start = %d exceeds data length %d", featureStart, len(data))
	}
	featureLength := int(binary.LittleEndian.Uint32(data[featureStart : featureStart+flatbuffers.SizeUint32]))
	featureEnd := featureStart + flatbuffers.SizeUint32 + featureLength
	if featureEnd > len(data) {
		t.Fatalf("feature length = %d exceeds data length %d at start %d", featureLength, len(data), featureStart)
	}
	featureRoot := featureStart + flatbuffers.SizeUint32 + int(binary.LittleEndian.Uint32(data[featureStart+flatbuffers.SizeUint32:featureStart+flatbuffers.SizeUint32+flatbuffers.SizeUOffsetT]))
	geometryRelative := tableIndirectFieldOffset(t, data, featureRoot, 4)
	if geometryRelative == 0 {
		t.Fatal("feature geometry is missing")
	}
	geometryRoot := featureRoot + geometryRelative + int(binary.LittleEndian.Uint32(data[featureRoot+geometryRelative:featureRoot+geometryRelative+flatbuffers.SizeUOffsetT]))
	if geometryRoot >= featureEnd {
		t.Fatalf("geometry root = %d exceeds feature end %d", geometryRoot, featureEnd)
	}
	return firstGeometryXYVectorOffset(t, data, geometryRoot)
}

func firstGeometryXYVectorOffset(t *testing.T, data []byte, geometryRoot int) int {
	t.Helper()
	if xyOffset := tableVectorFieldDataOffset(t, data, geometryRoot, 6); xyOffset > 0 {
		return xyOffset
	}
	partsRelative := tableVectorFieldOffset(t, data, geometryRoot, 18)
	if partsRelative == 0 {
		t.Fatal("geometry has neither xy nor parts")
	}
	partsVector := geometryRoot + partsRelative + int(binary.LittleEndian.Uint32(data[geometryRoot+partsRelative:geometryRoot+partsRelative+flatbuffers.SizeUOffsetT]))
	if partsVector+flatbuffers.SizeUint32+flatbuffers.SizeUOffsetT > len(data) {
		t.Fatalf("parts vector offset = %d exceeds data length %d", partsVector, len(data))
	}
	firstPartOffsetPosition := partsVector + flatbuffers.SizeUint32
	firstPartRoot := firstPartOffsetPosition + int(binary.LittleEndian.Uint32(data[firstPartOffsetPosition:firstPartOffsetPosition+flatbuffers.SizeUOffsetT]))
	if firstPartRoot >= len(data) {
		t.Fatalf("first part root = %d exceeds data length %d", firstPartRoot, len(data))
	}
	return firstGeometryXYVectorOffset(t, data, firstPartRoot)
}

func tableIndirectFieldOffset(t *testing.T, data []byte, tableRoot int, vtableOffset int) int {
	t.Helper()
	if tableRoot+flatbuffers.SizeSOffsetT > len(data) {
		t.Fatalf("table root = %d exceeds data length %d", tableRoot, len(data))
	}
	vtableStart := tableRoot - int(binary.LittleEndian.Uint32(data[tableRoot:tableRoot+flatbuffers.SizeSOffsetT]))
	if vtableStart < 0 || vtableStart+flatbuffers.SizeVOffsetT > len(data) {
		t.Fatalf("vtable start = %d is outside data length %d", vtableStart, len(data))
	}
	vtableLength := int(binary.LittleEndian.Uint16(data[vtableStart : vtableStart+flatbuffers.SizeVOffsetT]))
	if vtableOffset+flatbuffers.SizeVOffsetT > vtableLength {
		return 0
	}
	return int(binary.LittleEndian.Uint16(data[vtableStart+vtableOffset : vtableStart+vtableOffset+flatbuffers.SizeVOffsetT]))
}

func tableVectorFieldOffset(t *testing.T, data []byte, tableRoot int, vtableOffset int) int {
	t.Helper()
	return tableIndirectFieldOffset(t, data, tableRoot, vtableOffset)
}

func tableVectorFieldDataOffset(t *testing.T, data []byte, tableRoot int, vtableOffset int) int {
	t.Helper()
	fieldOffset := tableVectorFieldOffset(t, data, tableRoot, vtableOffset)
	if fieldOffset == 0 {
		return 0
	}
	uoffsetPosition := tableRoot + fieldOffset
	if uoffsetPosition+flatbuffers.SizeUOffsetT > len(data) {
		t.Fatalf("vector uoffset position = %d exceeds data length %d", uoffsetPosition, len(data))
	}
	vectorStart := uoffsetPosition + int(binary.LittleEndian.Uint32(data[uoffsetPosition:uoffsetPosition+flatbuffers.SizeUOffsetT]))
	if vectorStart+flatbuffers.SizeUint32 > len(data) {
		t.Fatalf("vector start = %d exceeds data length %d", vectorStart, len(data))
	}
	return vectorStart + flatbuffers.SizeUint32
}
