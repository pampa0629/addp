package jsonformat

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestJSONPluginImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin(nil)
	var _ format.FormatPlugin = plugin
	var _ format.FormatInfoProvider = plugin
	var _ format.DocumentInfoProvider = plugin
	var _ format.DocumentTextReader = plugin
	var _ format.TableInfoProvider = plugin
	var _ format.TableSampleReader = plugin
	var _ format.TableReaderProvider = plugin
	var _ format.TableWriterProvider = plugin
}

func TestJSONPluginReadDocumentText(t *testing.T) {
	plugin := NewPlugin(nil)

	got, truncated, err := plugin.ReadDocumentText(context.Background(), strings.NewReader("\ufeff{\"name\":\"测试\",\"enabled\":true}"), 1024, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText failed: %v", err)
	}
	if truncated {
		t.Fatal("ReadDocumentText truncated = true, want false")
	}
	if got != `{"name":"测试","enabled":true}` {
		t.Fatalf("ReadDocumentText = %q", got)
	}
}

func TestJSONPluginReadDocumentTextTruncates(t *testing.T) {
	plugin := NewPlugin(nil)

	got, truncated, err := plugin.ReadDocumentText(context.Background(), strings.NewReader(`{"name":"abcdef"}`), 8, nil)
	if err != nil {
		t.Fatalf("ReadDocumentText failed: %v", err)
	}
	if !truncated {
		t.Fatal("ReadDocumentText truncated = false, want true")
	}
	if got != `{"name":` {
		t.Fatalf("ReadDocumentText = %q", got)
	}
}

func TestJSONPluginDescribeAndSampleGeoJSON(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A","count":1}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[3,4]},"properties":{"name":"B","count":2}}
		]
	}`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
	if info.GetSpatialInfo() == nil {
		t.Fatalf("spatial extension missing")
	}
	if field := info.GetField("geometry"); field == nil || field.Type != format.FieldTypeGeometry {
		t.Fatalf("geometry field = %#v", field)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "B" {
		t.Fatalf("rows = %#v, want second feature", rows)
	}
}

func TestJSONPluginDoesNotInventSpatialInfoWithoutGeometry(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","properties":{"name":"A","count":1}},
			{"type":"Feature","properties":{"name":"B","count":2}}
		]
	}`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.GetSpatialInfo() != nil {
		t.Fatalf("spatial extension should be absent: %#v", info.GetSpatialInfo())
	}
	if field := info.GetField("geometry"); field != nil {
		t.Fatalf("geometry field should be absent: %#v", field)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if _, ok := rows[0]["geometry"]; ok {
		t.Fatalf("row should not contain geometry: %#v", rows[0])
	}
}

func TestJSONPluginDescribeFormatDistinguishesDocumentAndFeatureCollection(t *testing.T) {
	plugin := NewPlugin(nil)

	docInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(`{"name":"A"}`), nil)
	if err != nil {
		t.Fatalf("DescribeFormat(document) failed: %v", err)
	}
	if docInfo["structure"] != StructureDocument {
		t.Fatalf("document structure = %#v", docInfo)
	}

	fcInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{}}]}`), nil)
	if err != nil {
		t.Fatalf("DescribeFormat(feature collection) failed: %v", err)
	}
	if fcInfo["structure"] != StructureGeoJSONFeatureSet || fcInfo["has_geometry"] != true {
		t.Fatalf("feature collection info = %#v", fcInfo)
	}
}

func TestJSONPluginDescribeAndSampleObjectArray(t *testing.T) {
	data := `[
		{"id":"1","name":"A","area":"356.16704388138885"},
		{"id":"2","name":"B","area":"129.1114944814742"}
	]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
	if info.GetSpatialInfo() != nil {
		t.Fatalf("spatial extension should be absent: %#v", info.GetSpatialInfo())
	}
	for _, name := range []string{"id", "name", "area"} {
		if field := info.GetField(name); field == nil {
			t.Fatalf("field %q missing: %#v", name, info.Fields)
		}
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "B" {
		t.Fatalf("rows = %#v, want second object", rows)
	}
}

func TestJSONPluginOpenTableReaderObjectArray(t *testing.T) {
	data := `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`
	plugin := NewPlugin(nil)

	reader, err := plugin.OpenTableReader(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	defer reader.Close(context.Background())

	rows, err := reader.ReadRows(context.Background(), 1)
	if err != nil {
		t.Fatalf("ReadRows first batch failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "A" {
		t.Fatalf("first rows = %#v, want A", rows)
	}
	rows, err = reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows second batch failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "B" {
		t.Fatalf("second rows = %#v, want B", rows)
	}
	rows, err = reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows EOF batch failed: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("EOF rows = %#v, want empty", rows)
	}
}

func TestJSONPluginOpenTableReaderLines(t *testing.T) {
	data := "{\"id\":1,\"name\":\"A\"}\n{\"id\":2,\"name\":\"B\"}\n"
	plugin := NewPlugin(nil)

	reader, err := plugin.OpenTableReader(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	defer reader.Close(context.Background())

	rows, err := reader.ReadRows(context.Background(), 10)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["name"] != "A" || rows[1]["name"] != "B" {
		t.Fatalf("rows = %#v, want A/B", rows)
	}
}

func TestJSONPluginOpenTableWriterArray(t *testing.T) {
	plugin := NewPlugin(nil)
	schema := &format.TableInfo{Fields: []format.FieldInfo{
		{Name: "id", Type: format.FieldTypeInt},
		{Name: "name", Type: format.FieldTypeString},
	}}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, schema, nil)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "A", "extra": "ignored"},
		{"id": 2, "name": "B"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("unmarshal output failed: %v; output=%s", err, buf.String())
	}
	if len(rows) != 2 || rows[0]["name"] != "A" || rows[1]["name"] != "B" {
		t.Fatalf("rows = %#v, want A/B", rows)
	}
	if _, ok := rows[0]["extra"]; ok {
		t.Fatalf("schema field filtering failed: %#v", rows[0])
	}
}

func TestJSONPluginOpenTableWriterLines(t *testing.T) {
	plugin := NewPlugin(nil)
	opts := format.DefaultWriteOptions()
	opts.ExtraParams = map[string]interface{}{"json_mode": "jsonl"}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, nil, opts)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "A"},
		{"id": 2, "name": "B"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	decoder := json.NewDecoder(strings.NewReader(buf.String()))
	names := []string{}
	for decoder.More() {
		var row map[string]interface{}
		if err := decoder.Decode(&row); err != nil {
			t.Fatalf("decode JSON line failed: %v; output=%s", err, buf.String())
		}
		names = append(names, row["name"].(string))
	}
	if strings.Join(names, ",") != "A,B" {
		t.Fatalf("names = %#v, want A/B", names)
	}
}

func TestJSONPluginOpenTableWriterGeoJSON(t *testing.T) {
	plugin := NewPlugin(nil)
	opts := format.DefaultWriteOptions()
	opts.ExtraParams = map[string]interface{}{
		"spatial.target_encoding": "geojson",
		"geometry_field":          "geom",
	}
	schema := &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString},
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{GeometryColumn: "geom", GeometryType: "Point", SRID: 4326},
	}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, schema, opts)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "A", "geom": map[string]interface{}{"type": "Point", "coordinates": []interface{}{float64(1), float64(2)}}},
		{"id": 2, "name": "B", "geom": `{"type":"Point","coordinates":[3,4]}`},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	var collection map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &collection); err != nil {
		t.Fatalf("unmarshal GeoJSON failed: %v; output=%s", err, buf.String())
	}
	if collection["type"] != "FeatureCollection" {
		t.Fatalf("collection type = %#v, want FeatureCollection", collection["type"])
	}
	features, ok := collection["features"].([]interface{})
	if !ok || len(features) != 2 {
		t.Fatalf("features = %#v, want 2 features", collection["features"])
	}
	first := features[0].(map[string]interface{})
	if first["type"] != "Feature" || first["id"].(float64) != 1 {
		t.Fatalf("first feature = %#v", first)
	}
	props := first["properties"].(map[string]interface{})
	if props["name"] != "A" {
		t.Fatalf("properties = %#v, want name A", props)
	}
	if _, ok := props["geom"]; ok {
		t.Fatalf("geometry field leaked into properties: %#v", props)
	}
	geom := first["geometry"].(map[string]interface{})
	if geom["type"] != "Point" {
		t.Fatalf("geometry = %#v, want Point", geom)
	}
}

func TestJSONPluginObjectArrayDetectsVerifiedWKBGeometry(t *testing.T) {
	data := `[
		{
			"id":"1",
			"SmGeometry":"0101000000000000000000F03F0000000000000040",
			"name":"A"
		}
	]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	spatial := info.GetSpatialInfo()
	if spatial == nil {
		t.Fatalf("spatial extension missing")
	}
	if spatial.GeometryColumn != "SmGeometry" || spatial.GeometryType != "Point" {
		t.Fatalf("spatial = %#v", spatial)
	}
	if spatial.SRID != 0 {
		t.Fatalf("plain WKB should not imply SRID, got %d", spatial.SRID)
	}
	if field := info.GetField("SmGeometry"); field == nil || field.Type != format.FieldTypeGeometry {
		t.Fatalf("geometry field = %#v", field)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	geom, ok := rows[0]["SmGeometry"].(map[string]interface{})
	coords, _ := geom["coordinates"].([]interface{})
	if !ok || geom["type"] != "Point" || geom["wkb"] == "" || len(coords) != 2 {
		t.Fatalf("geometry row value = %#v", rows[0]["SmGeometry"])
	}
}

func TestJSONPluginGeoJSONComputesBoundingBoxWithoutFileBBox(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"LineString","coordinates":[[3,4],[-1,7],[5,-2]]},"properties":{"name":"A"}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[8,6]},"properties":{"name":"B"}}
		]
	}`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	spatial := info.GetSpatialInfo()
	if spatial == nil || spatial.BoundingBox == nil {
		t.Fatalf("spatial bbox missing: %#v", spatial)
	}
	if got, want := *spatial.BoundingBox, [4]float64{-1, -2, 8, 7}; got != want {
		t.Fatalf("bbox = %#v, want %#v", got, want)
	}
	if spatial.SRID != 4326 {
		t.Fatalf("GeoJSON SRID = %d, want 4326", spatial.SRID)
	}

	formatInfo, err := plugin.DescribeFormat(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeFormat failed: %v", err)
	}
	if got, want := formatInfo["bbox"], [4]float64{-1, -2, 8, 7}; got != want {
		t.Fatalf("format bbox = %#v, want %#v", got, want)
	}
}

func TestJSONPluginObjectArrayDetectsEWKBSRID(t *testing.T) {
	data := `[{"id":1,"geom":"` + ewkbPointHex(4326, 1, 2) + `"}]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	spatial := info.GetSpatialInfo()
	if spatial == nil {
		t.Fatalf("spatial extension missing")
	}
	if spatial.GeometryColumn != "geom" || spatial.GeometryType != "Point" || spatial.SRID != 4326 {
		t.Fatalf("spatial = %#v", spatial)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	geom, ok := rows[0]["geom"].(map[string]interface{})
	if !ok || geom["srid"] != int64(4326) {
		t.Fatalf("geometry row value = %#v", rows[0]["geom"])
	}
}

func TestJSONPluginObjectArrayDetectsMultiPolygonWKB(t *testing.T) {
	data := `[{"id":1,"geom":"` + wkbMultiPolygonHex() + `"}]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	spatial := info.GetSpatialInfo()
	if spatial == nil || spatial.GeometryType != "MultiPolygon" {
		t.Fatalf("spatial = %#v", spatial)
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	geom, ok := rows[0]["geom"].(map[string]interface{})
	if !ok || geom["type"] != "MultiPolygon" {
		t.Fatalf("geometry row value = %#v", rows[0]["geom"])
	}
	coords, ok := geom["coordinates"].([]interface{})
	if !ok || len(coords) != 1 {
		t.Fatalf("multipolygon coordinates = %#v", geom["coordinates"])
	}
}

func TestJSONPluginDoesNotTreatArbitraryHexAsGeometry(t *testing.T) {
	data := `[{"id":1,"payload":"0102030405060708090A"}]`
	plugin := NewPlugin(nil)

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if spatial := info.GetSpatialInfo(); spatial != nil {
		t.Fatalf("spatial extension should be absent: %#v", spatial)
	}
	if field := info.GetField("payload"); field == nil || field.Type != format.FieldTypeString {
		t.Fatalf("payload field = %#v", field)
	}
}

func TestJSONPluginDescribeTableBuildsSparseRowIndex(t *testing.T) {
	data := `[
		{"id":1,"name":"A"},
		{"id":2,"name":"B"},
		{"id":3,"name":"C"},
		{"id":4,"name":"D"}
	]`
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ContentIndexStep = 2

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	indexInfo := info.GetContentIndexInfo()
	if indexInfo == nil || indexInfo.Table == nil {
		t.Fatalf("content index missing")
	}
	index := indexInfo.Table
	if index.Kind != format.ContentIndexKindSparseRow || index.RowCount != 4 || len(index.Anchors) != 3 {
		t.Fatalf("index = %#v", index)
	}
	if index.Anchors[1].Row != 2 || index.Anchors[1].ByteOffset <= index.Anchors[0].ByteOffset {
		t.Fatalf("anchors = %#v", index.Anchors)
	}
}

func TestJSONPluginSampleGeoJSONFromPositionedReader(t *testing.T) {
	data := `{
		"type": "FeatureCollection",
		"features": [
			{"type":"Feature","geometry":{"type":"Point","coordinates":[1,2]},"properties":{"name":"A"}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[3,4]},"properties":{"name":"B"}},
			{"type":"Feature","geometry":{"type":"Point","coordinates":[5,6]},"properties":{"name":"C"}}
		]
	}`
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ContentIndexStep = 1

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	index := info.GetContentIndexInfo().Table
	start := index.Anchors[1].ByteOffset
	positioned := format.DefaultParseOptions()
	positioned.TableSample = &format.TableSampleOptions{
		Fields:            info.Fields,
		InputStartsAtRow:  index.Anchors[1].Row,
		InputIsPositioned: true,
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(data[start:]), 2, 1, positioned)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "C" {
		t.Fatalf("rows = %#v, want C", rows)
	}
	if _, ok := rows[0]["properties"]; ok {
		t.Fatalf("positioned row should be flattened properties, got %#v", rows[0])
	}
	if geom, ok := rows[0]["geometry"].(map[string]interface{}); !ok || geom["type"] != "Point" {
		t.Fatalf("positioned row geometry = %#v", rows[0]["geometry"])
	}
}

func ewkbPointHex(srid uint32, x, y float64) string {
	var buf bytes.Buffer
	buf.WriteByte(1)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(0x20000001))
	_ = binary.Write(&buf, binary.LittleEndian, srid)
	_ = binary.Write(&buf, binary.LittleEndian, x)
	_ = binary.Write(&buf, binary.LittleEndian, y)
	return strings.ToUpper(hex.EncodeToString(buf.Bytes()))
}

func wkbMultiPolygonHex() string {
	var buf bytes.Buffer
	buf.WriteByte(1)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(6))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(1))
	buf.WriteByte(1)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(3))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(5))
	for _, point := range [][2]float64{{0, 0}, {1, 0}, {1, 1}, {0, 1}, {0, 0}} {
		_ = binary.Write(&buf, binary.LittleEndian, point[0])
		_ = binary.Write(&buf, binary.LittleEndian, point[1])
	}
	return strings.ToUpper(hex.EncodeToString(buf.Bytes()))
}
func TestJSONPluginSampleTableFromPositionedReader(t *testing.T) {
	data := `[
		{"id":1,"name":"A"},
		{"id":2,"name":"B"},
		{"id":3,"name":"C"},
		{"id":4,"name":"D"}
	]`
	plugin := NewPlugin(nil)
	opts := format.DefaultParseOptions()
	opts.ContentIndexStep = 2

	info, err := plugin.DescribeTable(context.Background(), strings.NewReader(data), opts)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	index := info.GetContentIndexInfo().Table
	start := index.Anchors[1].ByteOffset
	fragment := data[start:]
	positioned := format.DefaultParseOptions()
	positioned.TableSample = &format.TableSampleOptions{
		Fields:            info.Fields,
		InputStartsAtRow:  index.Anchors[1].Row,
		InputIsPositioned: true,
	}

	rows, err := plugin.SampleTable(context.Background(), strings.NewReader(fragment), 3, 1, positioned)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "D" {
		t.Fatalf("rows = %#v, want D", rows)
	}
}
