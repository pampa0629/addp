package parquet

import (
	"bytes"
	"context"
	"github.com/addp/common/datatype"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	"github.com/addp/common/resume"
	parquetgo "github.com/parquet-go/parquet-go"
	parquetfmt "github.com/parquet-go/parquet-go/format"
)

type testParquetRow struct {
	ID   int64  `parquet:"id"`
	Name string `parquet:"name"`
}

type partitionColumnParquetRow struct {
	ID int64  `parquet:"id"`
	DT string `parquet:"dt"`
}

func TestParquetPluginImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin()
	var _ format.FormatPlugin = plugin
	var _ format.TableInfoProvider = plugin
	var _ format.TableSampleReader = plugin
	var _ format.ScopeTableInfoProvider = plugin
	var _ format.ScopeTableSampleReader = plugin
	var _ format.ScopeTableReaderProvider = plugin
	var _ format.TableReaderProvider = plugin
	var _ format.TableWriterProvider = plugin
}

func TestParquetPluginDescribeAndSampleTable(t *testing.T) {
	data := buildDefaultTestParquetData(t)
	plugin := NewPlugin()

	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.Table.RowCount == nil || *info.Table.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.Table.RowCount)
	}
	if len(info.Table.Fields) != 2 {
		t.Fatalf("fields = %#v, want 2 fields", info.Table.Fields)
	}

	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 1, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "Bob" {
		t.Fatalf("rows = %#v, want Bob", rows)
	}
}

func TestParquetPluginSampleTableSeeksDeepOffset(t *testing.T) {
	plugin := NewPlugin()
	rowsData := make([]testParquetRow, 0, 256)
	for i := 0; i < 256; i++ {
		rowsData = append(rowsData, testParquetRow{ID: int64(i + 1), Name: "row"})
	}
	data := buildParquetRows(t, rowsData...)

	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 199, 2, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["id"] != int64(200) || rows[1]["id"] != int64(201) {
		t.Fatalf("rows = %#v, want ids 200 and 201", rows)
	}
}

func TestParquetPluginSampleTableSkipsRowGroupsByOffset(t *testing.T) {
	plugin := NewPlugin()
	data := buildParquetRowsWithMaxRowsPerRowGroup(t, 2,
		testParquetRow{ID: 1, Name: "Alice"},
		testParquetRow{ID: 2, Name: "Bob"},
		testParquetRow{ID: 3, Name: "Carol"},
		testParquetRow{ID: 4, Name: "Dan"},
		testParquetRow{ID: 5, Name: "Eve"},
		testParquetRow{ID: 6, Name: "Frank"},
	)
	file, err := parquetgo.OpenFile(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if len(file.RowGroups()) != 3 {
		t.Fatalf("row groups = %d, want 3", len(file.RowGroups()))
	}

	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 4, 2, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if got, want := rowNames(rows), []string{"Eve", "Frank"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %#v, want names %v", rows, want)
	}
}

func TestParquetPluginSampleTableSeeksWithinRowGroupAndReadsNextGroup(t *testing.T) {
	plugin := NewPlugin()
	data := buildParquetRowsWithMaxRowsPerRowGroup(t, 2,
		testParquetRow{ID: 1, Name: "Alice"},
		testParquetRow{ID: 2, Name: "Bob"},
		testParquetRow{ID: 3, Name: "Carol"},
		testParquetRow{ID: 4, Name: "Dan"},
		testParquetRow{ID: 5, Name: "Eve"},
		testParquetRow{ID: 6, Name: "Frank"},
	)

	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 3, 2, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if got, want := rowNames(rows), []string{"Dan", "Eve"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %#v, want names %v", rows, want)
	}
}

func TestParquetPluginOpenTableReader(t *testing.T) {
	plugin := NewPlugin()
	data := buildParquetRows(t,
		testParquetRow{ID: 1, Name: "Alice"},
		testParquetRow{ID: 2, Name: "Bob"},
		testParquetRow{ID: 3, Name: "Carol"},
	)

	reader, err := plugin.OpenTableReader(context.Background(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	defer reader.Close(context.Background())

	fields := reader.Fields()
	if len(fields) != 2 || fields[0].Name != "id" || fields[1].Name != "name" {
		t.Fatalf("fields = %#v, want id,name", fields)
	}
	first, err := reader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows first batch failed: %v", err)
	}
	if len(first) != 2 || first[0]["name"] != "Alice" || first[1]["name"] != "Bob" {
		t.Fatalf("first batch = %#v, want Alice/Bob", first)
	}
	second, err := reader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows second batch failed: %v", err)
	}
	if len(second) != 1 || second[0]["name"] != "Carol" {
		t.Fatalf("second batch = %#v, want Carol", second)
	}
	empty, err := reader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows EOF batch failed: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("EOF rows = %#v, want empty", empty)
	}
}

func TestParquetPluginOpenTableReaderAppliesFieldSelection(t *testing.T) {
	plugin := NewPlugin()
	data := buildParquetRows(t,
		testParquetRow{ID: 1, Name: "Alice"},
		testParquetRow{ID: 2, Name: "Bob"},
	)
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"name"}}

	reader, err := plugin.OpenTableReader(context.Background(), bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("OpenTableReader failed: %v", err)
	}
	defer reader.Close(context.Background())

	fields := reader.Fields()
	if len(fields) != 1 || fields[0].Name != "name" {
		t.Fatalf("fields = %#v, want only name", fields)
	}
	rows, err := reader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Alice" || rows[0]["id"] != nil {
		t.Fatalf("rows = %#v, want only name field", rows)
	}
}

func TestParquetPluginRejectsResumeMarker(t *testing.T) {
	plugin := NewPlugin()
	data := buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"})
	parseOpts := format.DefaultParseOptions()
	parseOpts.ResumeMarker = &resume.Marker{Version: resume.MarkerVersionV1}
	if _, err := plugin.OpenTableReader(context.Background(), bytes.NewReader(data), parseOpts); err == nil {
		t.Fatal("OpenTableReader succeeded with resume marker, want explicit unsupported error")
	}

	writeOpts := format.DefaultWriteOptions()
	writeOpts.ResumeMarker = &resume.Marker{Version: resume.MarkerVersionV1}
	if _, err := plugin.OpenTableWriter(context.Background(), &bytes.Buffer{}, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}},
	}, writeOpts); err == nil {
		t.Fatal("OpenTableWriter succeeded with resume marker, want explicit unsupported error")
	}

	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/part-000.parquet": data,
	}}
	if _, err := plugin.OpenTableScopeReader(context.Background(), reader, contentio.NewRef("dataset", contentio.RoleScope), parseOpts); err == nil {
		t.Fatal("OpenTableScopeReader succeeded with resume marker, want explicit unsupported error")
	}
}

func TestParquetPluginFieldSelectionMissingFieldPolicies(t *testing.T) {
	plugin := NewPlugin()
	data := buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"})
	errorOpts := format.DefaultParseOptions()
	errorOpts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"missing"}}

	if _, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data), errorOpts); err == nil {
		t.Fatal("expected missing field error")
	}

	ignoreOpts := format.DefaultParseOptions()
	ignoreOpts.FieldSelection = &format.FieldSelectionOptions{
		Include:            []string{"name", "missing"},
		MissingFieldPolicy: format.MissingFieldIgnore,
	}
	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(data), 0, 1, ignoreOpts)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "Alice" || rows[0]["missing"] != nil || rows[0]["id"] != nil {
		t.Fatalf("rows = %#v, want existing selected field only", rows)
	}
}

func TestParquetPluginOpenTableWriter(t *testing.T) {
	plugin := NewPlugin()
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
			{Name: "score", Type: datatype.FieldTypeDouble, Nullable: true},
			{Name: "active", Type: datatype.FieldTypeBool, Nullable: true},
		},
	}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, nil)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": int64(1), "name": "Alice", "score": 9.5, "active": true},
		{"id": int64(2), "name": "Bob", "score": 8.25, "active": false},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.Table.RowCount == nil || *info.Table.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.Table.RowCount)
	}
	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(buf.Bytes()), 0, 2, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Alice" || rows[1]["active"] != false {
		t.Fatalf("rows = %#v, want Alice/Bob", rows)
	}
}

func TestParquetPluginOpenTableWriterUsesWriterOptions(t *testing.T) {
	plugin := NewPlugin()
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeBigInt},
			{Name: "name", Type: datatype.FieldTypeString, Nullable: true},
		},
	}
	opts := format.DefaultWriteOptions()
	opts.ExtraParams = map[string]interface{}{
		ParquetWriterMaxRowsPerRowGroupOption: int64(1),
		ParquetWriterCompressionOption:        "snappy",
	}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, opts)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": int64(1), "name": "Alice"},
		{"id": int64(2), "name": "Bob"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	file, err := parquetgo.OpenFile(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	if len(file.RowGroups()) != 2 {
		t.Fatalf("row groups = %d, want 2", len(file.RowGroups()))
	}
	if got := file.Metadata().RowGroups[0].Columns[0].MetaData.Codec; got != parquetfmt.Snappy {
		t.Fatalf("compression codec = %v, want snappy", got)
	}
}

func TestParquetPluginOpenTableWriterRejectsInvalidWriterOptions(t *testing.T) {
	plugin := NewPlugin()
	tableInfo := &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt}}}
	opts := format.DefaultWriteOptions()
	opts.ExtraParams = map[string]interface{}{
		ParquetWriterMaxRowsPerRowGroupOption: int64(0),
	}
	if _, err := plugin.OpenTableWriter(context.Background(), &bytes.Buffer{}, tableInfo, opts); err == nil {
		t.Fatal("OpenTableWriter succeeded with invalid row group size, want error")
	}

	opts.ExtraParams = map[string]interface{}{
		ParquetWriterCompressionOption: "made-up",
	}
	if _, err := plugin.OpenTableWriter(context.Background(), &bytes.Buffer{}, tableInfo, opts); err == nil {
		t.Fatal("OpenTableWriter succeeded with invalid compression, want error")
	}
}

func TestParquetPluginOpenTableWriterSerializesJSONLikeFields(t *testing.T) {
	plugin := NewPlugin()
	tableInfo := &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "payload", Type: datatype.FieldTypeJSON, Nullable: true},
		},
	}
	var buf bytes.Buffer

	writer, err := plugin.OpenTableWriter(context.Background(), &buf, tableInfo, nil)
	if err != nil {
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "payload": map[string]interface{}{"kind": "demo"}},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	rows, err := plugin.SampleTable(context.Background(), bytes.NewReader(buf.Bytes()), 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTable failed: %v", err)
	}
	if len(rows) != 1 || !strings.Contains(rows[0]["payload"].(string), `"kind":"demo"`) {
		t.Fatalf("rows = %#v, want JSON payload string", rows)
	}
}

func TestParquetPluginDescribeAndSampleScopeAcrossFiles(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}, testParquetRow{ID: 2, Name: "Bob"}),
		"dataset/part-001.parquet": buildParquetRows(t, testParquetRow{ID: 3, Name: "Carol"}, testParquetRow{ID: 4, Name: "Dan"}),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	info, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("DescribeTableScope failed: %v", err)
	}
	if info.Table.RowCount == nil || *info.Table.RowCount != 4 {
		t.Fatalf("row count = %v, want 4", info.Table.RowCount)
	}
	if len(info.Table.Fields) != 2 {
		t.Fatalf("fields = %#v, want 2 fields", info.Table.Fields)
	}
	parquetInfo := InfoFromDescribeResult(info)
	if parquetInfo == nil || len(parquetInfo.Files) != 2 {
		t.Fatalf("parquet info = %#v, want two files", parquetInfo)
	}
	if parquetInfo.Files[0].Path != "dataset/part-000.parquet" || parquetInfo.Files[0].RowCount != 2 {
		t.Fatalf("first parquet object info = %#v, want path and row count", parquetInfo.Files[0])
	}

	rows, err := plugin.SampleTableScope(context.Background(), reader, scope, 1, 3, nil)
	if err != nil {
		t.Fatalf("SampleTableScope failed: %v", err)
	}
	got := rowNames(rows)
	want := []string{"Bob", "Carol", "Dan"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %#v, want names %v", rows, want)
	}
}

func TestParquetPluginSampleScopeUsesRowCountHintsToSkipFiles(t *testing.T) {
	plugin := NewPlugin()
	openCounts := map[string]int{}
	reader := parquetMemoryContentReader{
		data: map[string][]byte{
			"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}, testParquetRow{ID: 2, Name: "Bob"}),
			"dataset/part-001.parquet": buildParquetRows(t, testParquetRow{ID: 3, Name: "Carol"}, testParquetRow{ID: 4, Name: "Dan"}),
			"dataset/part-002.parquet": buildParquetRows(t, testParquetRow{ID: 5, Name: "Eve"}, testParquetRow{ID: 6, Name: "Frank"}),
		},
		openCounts: openCounts,
	}
	scope := contentio.NewRef("dataset", contentio.RoleScope)
	opts := format.DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{
		FileRowCountsOption: map[string]int64{
			"dataset/part-000.parquet": 2,
			"dataset/part-001.parquet": 2,
			"dataset/part-002.parquet": 2,
		},
	}

	rows, err := plugin.SampleTableScope(context.Background(), reader, scope, 4, 2, opts)
	if err != nil {
		t.Fatalf("SampleTableScope failed: %v", err)
	}
	got := rowNames(rows)
	want := []string{"Eve", "Frank"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %#v, want names %v", rows, want)
	}
	if openCounts["dataset/part-000.parquet"] != 0 || openCounts["dataset/part-001.parquet"] != 0 {
		t.Fatalf("open counts = %#v, want skipped files not opened", openCounts)
	}
	if openCounts["dataset/part-002.parquet"] != 1 {
		t.Fatalf("part-002 open count = %d, want 1", openCounts["dataset/part-002.parquet"])
	}
}

func TestParquetPluginScopeRecursesPartitionDirs(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/dt=2026-05-06/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 2, Name: "Bob"}),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	info, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("DescribeTableScope failed: %v", err)
	}
	if info.Table.RowCount == nil || *info.Table.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.Table.RowCount)
	}
	if field := info.Table.GetField("dt"); field == nil || field.Type != datatype.FieldTypeString {
		t.Fatalf("partition field dt = %#v, want string field", field)
	}
	if got := parquetPartitionColumns(info.Table.Native["partition_columns"]); strings.Join(got, ",") != "dt" {
		t.Fatalf("table native partition columns = %#v, want dt", info.Table.Native)
	}
	if info.FormatInfo["partition_columns"] != nil {
		t.Fatalf("format info should not contain table native partition columns: %#v", info.FormatInfo)
	}
	parquetInfo := InfoFromDescribeResult(info)
	if parquetInfo == nil || strings.Join(parquetInfo.PartitionColumns, ",") != "dt" {
		t.Fatalf("partition columns = %#v, want dt", parquetInfo)
	}
}

func TestParquetPluginSampleScopeAddsPartitionValues(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/dt=2026-05-06/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 2, Name: "Bob"}),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	rows, err := plugin.SampleTableScope(context.Background(), reader, scope, 0, 2, nil)
	if err != nil {
		t.Fatalf("SampleTableScope failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["dt"] != "2026-05-05" || rows[1]["dt"] != "2026-05-06" {
		t.Fatalf("rows = %#v, want partition dt values", rows)
	}
}

func TestParquetPluginScopeDoesNotOverrideExistingPartitionNamedColumn(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildPartitionColumnParquetRows(t, partitionColumnParquetRow{ID: 1, DT: "from-file"}),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	info, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("DescribeTableScope failed: %v", err)
	}
	if len(info.Table.Fields) != 2 {
		t.Fatalf("fields = %#v, want only file fields without duplicate dt", info.Table.Fields)
	}
	rows, err := plugin.SampleTableScope(context.Background(), reader, scope, 0, 1, nil)
	if err != nil {
		t.Fatalf("SampleTableScope failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["dt"] != "from-file" {
		t.Fatalf("rows = %#v, want dt from file", rows)
	}
}

func TestParquetPluginOpenTableScopeReader(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/dt=2026-05-06/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 2, Name: "Bob"}, testParquetRow{ID: 3, Name: "Carol"}),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	tableReader, err := plugin.OpenTableScopeReader(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("OpenTableScopeReader failed: %v", err)
	}
	defer tableReader.Close(context.Background())
	markerProvider, ok := tableReader.(format.ResumeMarkerProvider)
	if !ok {
		t.Fatal("scope table reader should expose resume markers")
	}
	if marker := markerProvider.ResumeMarker(); marker != nil {
		t.Fatalf("initial resume marker = %#v, want nil before reading rows", marker)
	}

	first, err := tableReader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows first batch failed: %v", err)
	}
	firstMarker := markerProvider.ResumeMarker()
	if firstMarker == nil {
		t.Fatal("resume marker after first batch is nil")
	}
	if firstMarker.Version != resume.MarkerVersionV1 || firstMarker.Provider != "parquet.scope_table_reader" || firstMarker.PositionUnit != "ref_row" {
		t.Fatalf("first marker identity = %#v, want parquet scope ref_row marker", firstMarker)
	}
	if firstMarker.ReadPosition["ref"] != "dataset/dt=2026-05-06/part-000.parquet" ||
		firstMarker.ReadPosition["ref_index"] != 1 ||
		firstMarker.ReadPosition["row_offset"] != int64(1) ||
		firstMarker.ReadPosition["rows_read"] != int64(2) {
		t.Fatalf("first marker read position = %#v, want second ref row offset 1 and total rows 2", firstMarker.ReadPosition)
	}
	if firstMarker.Fingerprint["ref_count"] != 2 {
		t.Fatalf("first marker fingerprint = %#v, want ref_count 2", firstMarker.Fingerprint)
	}
	second, err := tableReader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows second batch failed: %v", err)
	}
	secondMarker := markerProvider.ResumeMarker()
	if secondMarker.ReadPosition["ref"] != "dataset/dt=2026-05-06/part-000.parquet" ||
		secondMarker.ReadPosition["ref_index"] != 1 ||
		secondMarker.ReadPosition["row_offset"] != int64(2) ||
		secondMarker.ReadPosition["rows_read"] != int64(3) {
		t.Fatalf("second marker read position = %#v, want second ref row offset 2 and total rows 3", secondMarker.ReadPosition)
	}
	got := append(rowNames(first), rowNames(second)...)
	want := []string{"Alice", "Bob", "Carol"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %#v, want names %v", append(first, second...), want)
	}
	if first[0]["dt"] != "2026-05-05" || first[1]["dt"] != "2026-05-06" || second[0]["dt"] != "2026-05-06" {
		t.Fatalf("partition values = %#v %#v, want dt from path", first, second)
	}
	tableInfo := &datatype.TableInfo{Fields: tableReader.Fields()}
	if field := tableInfo.GetField("dt"); field == nil || field.Type != datatype.FieldTypeString {
		t.Fatalf("tableInfo partition field dt = %#v, want string field", field)
	}
	empty, err := tableReader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows EOF failed: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("EOF rows = %#v, want empty", empty)
	}
}

func TestParquetPluginOpenTableScopeReaderUsesRangeReaderWhenSizeIsKnown(t *testing.T) {
	plugin := NewPlugin()
	openCounts := map[string]int{}
	rangeOpenCounts := map[string]int{}
	reader := parquetMemoryContentReader{
		data: map[string][]byte{
			"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}, testParquetRow{ID: 2, Name: "Bob"}),
		},
		openCounts:      openCounts,
		rangeOpenCounts: rangeOpenCounts,
	}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	tableReader, err := plugin.OpenTableScopeReader(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("OpenTableScopeReader failed: %v", err)
	}
	defer tableReader.Close(context.Background())

	rows, err := tableReader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(rows) != 2 || rows[0]["name"] != "Alice" || rows[1]["name"] != "Bob" {
		t.Fatalf("rows = %#v, want Alice/Bob", rows)
	}
	if openCounts["dataset/part-000.parquet"] != 0 {
		t.Fatalf("regular open count = %d, want 0 when range reader is usable", openCounts["dataset/part-000.parquet"])
	}
	if rangeOpenCounts["dataset/part-000.parquet"] == 0 {
		t.Fatalf("range open count = %d, want > 0", rangeOpenCounts["dataset/part-000.parquet"])
	}
}

func TestParquetPluginOpenTableScopeReaderFallsBackWhenSizeIsUnknown(t *testing.T) {
	plugin := NewPlugin()
	openCounts := map[string]int{}
	rangeOpenCounts := map[string]int{}
	reader := parquetMemoryContentReader{
		data: map[string][]byte{
			"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		},
		openCounts:      openCounts,
		rangeOpenCounts: rangeOpenCounts,
		unknownSize:     true,
	}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	tableReader, err := plugin.OpenTableScopeReader(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("OpenTableScopeReader failed: %v", err)
	}
	defer tableReader.Close(context.Background())

	rows, err := tableReader.ReadRows(context.Background(), 1)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	if len(rows) != 1 || rows[0]["name"] != "Alice" {
		t.Fatalf("rows = %#v, want Alice", rows)
	}
	if openCounts["dataset/part-000.parquet"] != 1 {
		t.Fatalf("regular open count = %d, want fallback open", openCounts["dataset/part-000.parquet"])
	}
	if rangeOpenCounts["dataset/part-000.parquet"] != 0 {
		t.Fatalf("range open count = %d, want 0 when size is unknown", rangeOpenCounts["dataset/part-000.parquet"])
	}
}

func TestParquetPluginOpenTableScopeReaderAppliesFieldSelectionToPartitionField(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/dt=2026-05-06/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 2, Name: "Bob"}),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)
	opts := format.DefaultParseOptions()
	opts.FieldSelection = &format.FieldSelectionOptions{Include: []string{"dt", "name"}}

	tableReader, err := plugin.OpenTableScopeReader(context.Background(), reader, scope, opts)
	if err != nil {
		t.Fatalf("OpenTableScopeReader failed: %v", err)
	}
	defer tableReader.Close(context.Background())

	rows, err := tableReader.ReadRows(context.Background(), 2)
	if err != nil {
		t.Fatalf("ReadRows failed: %v", err)
	}
	fields := tableReader.Fields()
	if len(fields) != 2 || fields[0].Name != "dt" || fields[1].Name != "name" {
		t.Fatalf("fields = %#v, want dt,name", fields)
	}
	if len(rows) != 2 || rows[0]["dt"] != "2026-05-05" || rows[0]["name"] != "Alice" || rows[0]["id"] != nil {
		t.Fatalf("rows = %#v, want selected partition and data fields", rows)
	}
}

func TestParquetPluginScopeRejectsIncompatibleSchema(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryContentReader{data: map[string][]byte{
		"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/part-001.parquet": buildAlternateParquetData(t),
	}}
	scope := contentio.NewRef("dataset", contentio.RoleScope)

	_, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err == nil {
		t.Fatal("expected incompatible tableInfo error")
	}
}

func buildDefaultTestParquetData(t *testing.T) []byte {
	t.Helper()
	return buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}, testParquetRow{ID: 2, Name: "Bob"})
}

func buildParquetRows(t *testing.T, rows ...testParquetRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := parquetgo.NewGenericWriter[testParquetRow](&buf)
	if _, err := writer.Write(rows); err != nil {
		t.Fatalf("write parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close parquet writer: %v", err)
	}
	return buf.Bytes()
}

func buildParquetRowsWithMaxRowsPerRowGroup(t *testing.T, maxRowsPerRowGroup int64, rows ...testParquetRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := parquetgo.NewGenericWriter[testParquetRow](&buf, parquetgo.MaxRowsPerRowGroup(maxRowsPerRowGroup))
	if _, err := writer.Write(rows); err != nil {
		t.Fatalf("write parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close parquet writer: %v", err)
	}
	return buf.Bytes()
}

type alternateParquetRow struct {
	ID    int64  `parquet:"id"`
	Title string `parquet:"title"`
}

func buildAlternateParquetData(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := parquetgo.NewGenericWriter[alternateParquetRow](&buf)
	if _, err := writer.Write([]alternateParquetRow{{ID: 1, Title: "Other"}}); err != nil {
		t.Fatalf("write alternate parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close alternate parquet writer: %v", err)
	}
	return buf.Bytes()
}

func buildPartitionColumnParquetRows(t *testing.T, rows ...partitionColumnParquetRow) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := parquetgo.NewGenericWriter[partitionColumnParquetRow](&buf)
	if _, err := writer.Write(rows); err != nil {
		t.Fatalf("write partition column parquet rows: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close partition column parquet writer: %v", err)
	}
	return buf.Bytes()
}

func rowNames(rows []map[string]interface{}) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row["name"].(string))
	}
	return names
}

type parquetMemoryContentReader struct {
	data            map[string][]byte
	openCounts      map[string]int
	rangeOpenCounts map[string]int
	unknownSize     bool
}

func (r parquetMemoryContentReader) Open(_ context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	data, ok := r.data[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	if r.openCounts != nil {
		r.openCounts[ref.Path]++
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (r parquetMemoryContentReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	data, ok := r.data[ref.Path]
	size := int64(len(data))
	if r.unknownSize {
		size = 0
	}
	return &contentio.Stat{Ref: ref, Exists: ok, Size: size}, nil
}

func (r parquetMemoryContentReader) OpenRange(_ context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	data, ok := r.data[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	if r.rangeOpenCounts != nil {
		r.rangeOpenCounts[ref.Path]++
	}
	end := offset + length
	if offset < 0 || length < 0 || offset > int64(len(data)) {
		return nil, contentio.ErrContentNotFound
	}
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return io.NopCloser(bytes.NewReader(data[offset:end])), nil
}

func (r parquetMemoryContentReader) List(_ context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	scopePath := strings.Trim(scope.Path, "/")
	dirs := map[string]bool{}
	files := make([]contentio.Ref, 0)
	for path := range r.data {
		trimmed := strings.Trim(path, "/")
		if !strings.HasPrefix(trimmed, scopePath+"/") {
			continue
		}
		rest := strings.TrimPrefix(trimmed, scopePath+"/")
		if strings.Contains(rest, "/") {
			dir := scopePath + "/" + strings.Split(rest, "/")[0]
			dirs[dir] = true
			continue
		}
		files = append(files, contentio.NewRef(trimmed, contentio.RoleMain))
	}
	for dir := range dirs {
		files = append(files, contentio.NewRef(dir, contentio.RoleScope))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) == 0 {
		return nil, contentio.ErrContentNotFound
	}
	return files, nil
}

var _ contentio.Reader = parquetMemoryContentReader{}
var _ contentio.Lister = parquetMemoryContentReader{}
var _ contentio.RangeReader = parquetMemoryContentReader{}
