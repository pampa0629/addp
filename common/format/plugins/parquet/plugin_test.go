package parquet

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	parquetgo "github.com/parquet-go/parquet-go"
)

type testParquetRow struct {
	ID   int64  `parquet:"id"`
	Name string `parquet:"name"`
}

func TestParquetPluginImplementsTargetInterfaces(t *testing.T) {
	plugin := NewPlugin()
	var _ format.FormatPlugin = plugin
	var _ format.TableProvider = plugin
	var _ format.ScopeTableProvider = plugin
}

func TestParquetPluginDescribeAndSampleTable(t *testing.T) {
	data := buildDefaultTestParquetData(t)
	plugin := NewPlugin()

	info, err := plugin.DescribeTable(context.Background(), bytes.NewReader(data), nil)
	if err != nil {
		t.Fatalf("DescribeTable failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
	if len(info.Fields) != 2 {
		t.Fatalf("fields = %#v, want 2 fields", info.Fields)
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

func TestParquetPluginDescribeAndSampleScopeAcrossFiles(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryResourceReader{data: map[string][]byte{
		"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}, testParquetRow{ID: 2, Name: "Bob"}),
		"dataset/part-001.parquet": buildParquetRows(t, testParquetRow{ID: 3, Name: "Carol"}, testParquetRow{ID: 4, Name: "Dan"}),
	}}
	scope := resource.NewResourceRef("dataset", resource.ResourceRoleScope)

	info, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("DescribeTableScope failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 4 {
		t.Fatalf("row count = %v, want 4", info.RowCount)
	}
	if len(info.Fields) != 2 {
		t.Fatalf("fields = %#v, want 2 fields", info.Fields)
	}
	parquetInfo := InfoFromTableInfo(info)
	if parquetInfo == nil || len(parquetInfo.Files) != 2 {
		t.Fatalf("parquet info = %#v, want two files", parquetInfo)
	}
	if parquetInfo.Files[0].Path != "dataset/part-000.parquet" || parquetInfo.Files[0].RowCount != 2 {
		t.Fatalf("first parquet file info = %#v, want path and row count", parquetInfo.Files[0])
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
	reader := parquetMemoryResourceReader{
		data: map[string][]byte{
			"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}, testParquetRow{ID: 2, Name: "Bob"}),
			"dataset/part-001.parquet": buildParquetRows(t, testParquetRow{ID: 3, Name: "Carol"}, testParquetRow{ID: 4, Name: "Dan"}),
			"dataset/part-002.parquet": buildParquetRows(t, testParquetRow{ID: 5, Name: "Eve"}, testParquetRow{ID: 6, Name: "Frank"}),
		},
		openCounts: openCounts,
	}
	scope := resource.NewResourceRef("dataset", resource.ResourceRoleScope)
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
	reader := parquetMemoryResourceReader{data: map[string][]byte{
		"dataset/dt=2026-05-05/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/dt=2026-05-06/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 2, Name: "Bob"}),
	}}
	scope := resource.NewResourceRef("dataset", resource.ResourceRoleScope)

	info, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err != nil {
		t.Fatalf("DescribeTableScope failed: %v", err)
	}
	if info.RowCount == nil || *info.RowCount != 2 {
		t.Fatalf("row count = %v, want 2", info.RowCount)
	}
}

func TestParquetPluginScopeRejectsIncompatibleSchema(t *testing.T) {
	plugin := NewPlugin()
	reader := parquetMemoryResourceReader{data: map[string][]byte{
		"dataset/part-000.parquet": buildParquetRows(t, testParquetRow{ID: 1, Name: "Alice"}),
		"dataset/part-001.parquet": buildAlternateParquetData(t),
	}}
	scope := resource.NewResourceRef("dataset", resource.ResourceRoleScope)

	_, err := plugin.DescribeTableScope(context.Background(), reader, scope, nil)
	if err == nil {
		t.Fatal("expected incompatible schema error")
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

func rowNames(rows []map[string]interface{}) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row["name"].(string))
	}
	return names
}

type parquetMemoryResourceReader struct {
	data       map[string][]byte
	openCounts map[string]int
}

func (r parquetMemoryResourceReader) Open(_ context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	data, ok := r.data[ref.Path]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	if r.openCounts != nil {
		r.openCounts[ref.Path]++
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (r parquetMemoryResourceReader) Stat(_ context.Context, ref resource.ResourceRef) (*resource.ResourceMetadata, error) {
	_, ok := r.data[ref.Path]
	return &resource.ResourceMetadata{Ref: ref, Exists: ok}, nil
}

func (r parquetMemoryResourceReader) List(_ context.Context, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	scopePath := strings.Trim(scope.Path, "/")
	dirs := map[string]bool{}
	files := make([]resource.ResourceRef, 0)
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
		files = append(files, resource.NewResourceRef(trimmed, resource.ResourceRoleMain))
	}
	for dir := range dirs {
		files = append(files, resource.NewResourceRef(dir, resource.ResourceRoleScope))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	if len(files) == 0 {
		return nil, resource.ErrResourceNotFound
	}
	return files, nil
}

var _ resource.ResourceReader = parquetMemoryResourceReader{}
