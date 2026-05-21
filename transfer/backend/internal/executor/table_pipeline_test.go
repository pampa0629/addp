package executor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
	jsonformat "github.com/addp/common/format/plugins/json"
	parquetformat "github.com/addp/common/format/plugins/parquet"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
)

func TestTableTransferExecutorConvertsEncodedCSVToJSONL(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n3,Carol\n"}
	writer := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceContentReader:     reader,
		TargetContentWriter:     writer,
		SourceTableReadProvider: csvformat.NewPlugin(nil),
		TargetFormatProvider:    jsonformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Target: TableTargetPlan{
			Kind:   TableEndpointEncoded,
			Format: format.FormatJSON,
			FormatOptions: &format.WriteOptions{
				ExtraParams: map[string]interface{}{"json_mode": "jsonl"},
			},
		},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 3 || metrics.RecordsWritten != 3 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 3 read/written and 2 batches", metrics)
	}
	got := strings.TrimSpace(writer.buf.String())
	wantLines := []string{
		`{"id":1,"name":"Alice"}`,
		`{"id":2,"name":"Bob"}`,
		`{"id":3,"name":"Carol"}`,
	}
	for _, line := range wantLines {
		if !strings.Contains(got, line) {
			t.Fatalf("jsonl output = %q, missing %q", got, line)
		}
	}
	if len(reader.opens) != 1 || reader.opens[0] != 0 {
		t.Fatalf("source open offsets = %#v, want one full read", reader.opens)
	}
	if !writer.closed {
		t.Fatal("target content writer was not closed")
	}
}

func TestTableTransferExecutorReadsShapefileRefs(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	target := engineplugin.FileItemPath(7, "imports/cities.shp")
	contentWriter := contentadapter.NewWriter(source, nil, target, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(target.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), contentWriter, refs, &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString, Size: 32},
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geom",
			GeometryType:   "Point",
		},
	}, nil)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geom": "POINT (120 30)"},
		{"id": 2, "name": "Beta", "geom": "POINT (121 31)"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	output := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceContentReader:     source,
		TargetContentWriter:     output,
		SourceMultiReadProvider: shapefilePlugin,
		TargetFormatProvider:    csvformat.NewPlugin(nil),
	}
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Path: target, Format: format.FormatShapefile},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 2 read/written and 2 batches", metrics)
	}
	got := output.buf.String()
	for _, want := range []string{"Alpha", "Beta", "POINT"} {
		if !strings.Contains(got, want) {
			t.Fatalf("csv output = %q, missing %q", got, want)
		}
	}
}

func TestTableTransferExecutorUsesPlannedSourceRelatedRefs(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	actualSourcePath := engineplugin.FileItemPath(7, "imports/roads.shp")
	sourceWriter := contentadapter.NewWriter(source, nil, actualSourcePath, engineplugin.WriteOptions{Overwrite: true})
	sourceRefs := format.SameBasenameRelatedRefs(actualSourcePath.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), sourceWriter, sourceRefs, &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString, Size: 32},
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geom",
			GeometryType:   "Point",
		},
	}, nil)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geom": "POINT (120 30)"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	output := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceContentReader:     source,
		TargetContentWriter:     output,
		SourceMultiReadProvider: shapefilePlugin,
		TargetFormatProvider:    csvformat.NewPlugin(nil),
	}
	staleBasePath := engineplugin.FileItemPath(7, "imports/stale.shp")
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:        TableEndpointEncoded,
			Path:        staleBasePath,
			Format:      format.FormatShapefile,
			RelatedRefs: sourceRefs,
		},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want one row copied", metrics)
	}
	got := output.buf.String()
	if !strings.Contains(got, "Alpha") || !strings.Contains(got, "POINT") {
		t.Fatalf("csv output = %q, want row read via planned source refs", got)
	}
}

func TestTableTransferExecutorRejectsEncodedWholeScopeSourceWithoutProvider(t *testing.T) {
	exec := &TableTransferExecutor{
		SourceContentReader:  &fakeContentReader{},
		TargetContentWriter:  &fakeContentWriter{},
		SourceFormatProvider: csvformat.NewPlugin(nil),
		TargetFormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:   TableEndpointEncoded,
			Format: format.FormatCSV,
			Layout: format.FormatLayoutWhole,
		},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 1,
	})
	if err == nil {
		t.Fatal("Execute succeeded, want whole scope source rejection")
	}
	if !strings.Contains(err.Error(), "whole scope table source requires scope table reader provider") {
		t.Fatalf("error = %q, want explicit whole scope reader limitation", err)
	}
}

func TestTableTransferExecutorReadsParquetWholeScopeSource(t *testing.T) {
	parquetPlugin := parquetformat.NewPlugin()
	source := &fakeContentWriter{files: map[string][]byte{}}
	writeParquetTestFile(t, source, engineplugin.FileItemPath(7, "datasets/orders/part-000.parquet"), parquetPlugin, []map[string]interface{}{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	})
	writeParquetTestFile(t, source, engineplugin.FileItemPath(7, "datasets/orders/dt=2026-05-21/part-001.parquet"), parquetPlugin, []map[string]interface{}{
		{"id": 3, "name": "Carol"},
	})

	output := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceContentReader:     source,
		TargetContentWriter:     output,
		SourceScopeReadProvider: parquetPlugin,
		TargetFormatProvider:    csvformat.NewPlugin(nil),
	}
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:   TableEndpointEncoded,
			Path:   engineplugin.FileDirectoryPath(7, "datasets/orders"),
			Format: format.FormatParquet,
			Layout: format.FormatLayoutWhole,
			Schema: &format.TableInfo{Fields: []format.FieldInfo{
				{Name: "id", Type: format.FieldTypeInt},
				{Name: "name", Type: format.FieldTypeString},
			}},
		},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 3 || metrics.RecordsWritten != 3 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 3 read/written and 2 batches", metrics)
	}
	got := output.buf.String()
	for _, want := range []string{"Alice", "Bob", "Carol"} {
		if !strings.Contains(got, want) {
			t.Fatalf("csv output = %q, missing %q", got, want)
		}
	}
}

func writeParquetTestFile(t *testing.T, storage *fakeContentWriter, path engineplugin.CatalogPath, plugin format.TableWriterProvider, rows []map[string]interface{}) {
	t.Helper()
	writer := contentadapter.NewWriter(storage, nil, path, engineplugin.WriteOptions{Overwrite: true})
	output, err := writer.Create(context.Background(), contentRefFromCatalogPath(path))
	if err != nil {
		t.Fatalf("Create parquet content failed: %v", err)
	}
	tableWriter, err := plugin.OpenTableWriter(context.Background(), output, &format.TableInfo{Fields: []format.FieldInfo{
		{Name: "id", Type: format.FieldTypeInt},
		{Name: "name", Type: format.FieldTypeString},
	}}, nil)
	if err != nil {
		_ = output.Close()
		t.Fatalf("OpenTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(context.Background(), rows); err != nil {
		_ = output.Close()
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(context.Background()); err != nil {
		_ = output.Close()
		t.Fatalf("Close table writer failed: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatalf("Close content failed: %v", err)
	}
}

func TestTableTransferExecutorCopiesShapefileRefsPreservingSpatialInfo(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	sourcePath := engineplugin.FileItemPath(7, "imports/cities_z.shp")
	sourceWriter := contentadapter.NewWriter(source, nil, sourcePath, engineplugin.WriteOptions{Overwrite: true})
	sourceRefs := format.SameBasenameRelatedRefs(sourcePath.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), sourceWriter, sourceRefs, &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString, Size: 32},
			{Name: "geometry", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geometry",
			GeometryType:   "Point",
			SRID:           4326,
			Dimension:      3,
		},
	}, &format.WriteOptions{
		Encoding: "utf-8",
		ExtraParams: map[string]interface{}{
			"spatial_ref_sys": "EPSG:4326",
		},
	})
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geometry": "POINT Z (120 30 99.5)"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	output := &fakeContentWriter{files: map[string][]byte{}}
	targetPath := engineplugin.FileItemPath(8, "exports/cities_z.shp")
	exec := &TableTransferExecutor{
		SourceContentReader:     source,
		TargetContentWriter:     output,
		SourceMultiReadProvider: shapefilePlugin,
		TargetMultiProvider:     shapefilePlugin,
	}
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Path: sourcePath, Format: format.FormatShapefile},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Path: targetPath, Format: format.FormatShapefile},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want 1 read/written and 1 batch", metrics)
	}
	for _, path := range []string{"exports/cities_z.shp", "exports/cities_z.shx", "exports/cities_z.dbf"} {
		if len(output.files[path]) == 0 {
			t.Fatalf("ref %s was not written", path)
		}
	}

	targetReader := contentadapter.NewReader(output, nil, targetPath, engineplugin.ReadOptions{})
	targetRefs := format.SameBasenameRelatedRefs(targetPath.StringPath(), shapefilePlugin.RelatedRefSpecs())
	info, err := shapefilePlugin.DescribeMultiTable(context.Background(), targetReader, targetRefs, format.DefaultParseOptions())
	if err != nil {
		t.Fatalf("DescribeMultiTable failed: %v", err)
	}
	if info.SpatialInfo == nil || info.SpatialInfo.Dimension != 3 || info.SpatialInfo.GeometryType != "Point" {
		t.Fatalf("spatial info = %#v, want Point dimension 3", info.SpatialInfo)
	}
	rows, err := shapefilePlugin.SampleMultiTable(context.Background(), targetReader, targetRefs, 0, 1, format.DefaultParseOptions())
	if err != nil {
		t.Fatalf("SampleMultiTable failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want one row", rows)
	}
	got, ok := rows[0][info.SpatialInfo.GeometryColumn].(string)
	if !ok {
		t.Fatalf("geometry value = %#v, want WKT string", rows[0][info.SpatialInfo.GeometryColumn])
	}
	if got != "POINT Z (120 30 99.5)" {
		t.Fatalf("geometry = %q, want POINT Z", got)
	}
}

func TestTableTransferExecutorPrefersMultiTableProvider(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	target := engineplugin.FileItemPath(7, "imports/cities.shp")
	contentWriter := contentadapter.NewWriter(source, nil, target, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(target.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), contentWriter, refs, &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString, Size: 32},
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geom",
			GeometryType:   "Point",
		},
	}, nil)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	if err := tableWriter.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geom": "POINT (120 30)"},
		{"id": 2, "name": "Beta", "geom": "POINT (121 31)"},
	}); err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := tableWriter.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	output := &fakeContentWriter{}
	sampleOnly := &failingMultiTableProvider{formatType: shapefilePlugin.Format(), specs: shapefilePlugin.RelatedRefSpecs()}
	exec := &TableTransferExecutor{
		SourceContentReader:     source,
		TargetContentWriter:     output,
		SourceMultiReadProvider: shapefilePlugin,
		SourceMultiInfoProvider: shapefilePlugin,
		SourceMultiSampleReader: sampleOnly,
		TargetFormatProvider:    csvformat.NewPlugin(nil),
	}
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Path: target, Format: format.FormatShapefile},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if sampleOnly.sampleCalled {
		t.Fatal("sample multi provider was called; want continuous multi reader path")
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 2 read/written and 2 batches", metrics)
	}
}

func TestNewTableTransferExecutorLoadsEncodedToEncodedProvidersFromRegistry(t *testing.T) {
	source := &fakeContentReader{engineType: "registry_encoded_source"}
	target := &fakeContentWriter{engineType: "registry_encoded_target"}
	engineplugin.Register(source)
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(source.Type())
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewTableTransferExecutor(source.Type(), target.Type(), format.FormatCSV, format.FormatJSON)
	if err != nil {
		t.Fatalf("NewTableTransferExecutor failed: %v", err)
	}
	if exec.SourceContentReader != source {
		t.Fatalf("reader = %#v, want registered source", exec.SourceContentReader)
	}
	if exec.TargetContentWriter != target {
		t.Fatalf("writer = %#v, want registered target", exec.TargetContentWriter)
	}
	if exec.SourceTableReadProvider.Format() != format.FormatCSV {
		t.Fatalf("table read provider = %q, want csv", exec.SourceTableReadProvider.Format())
	}
	if exec.TargetFormatProvider.Format() != format.FormatJSON {
		t.Fatalf("table writer provider = %q, want json", exec.TargetFormatProvider.Format())
	}
}

type failingMultiTableProvider struct {
	formatType   format.FormatType
	specs        []format.RelatedRefSpec
	sampleCalled bool
}

func (p *failingMultiTableProvider) Format() format.FormatType {
	return p.formatType
}

func (p *failingMultiTableProvider) Capabilities() format.FormatCapability {
	capability, _ := format.GetFormatCapability(p.formatType)
	return capability
}

func (p *failingMultiTableProvider) RelatedRefSpecs() []format.RelatedRefSpec {
	return append([]format.RelatedRefSpec(nil), p.specs...)
}

func (p *failingMultiTableProvider) SampleMultiTable(context.Context, contentio.Reader, []format.RelatedRef, int64, int64, *format.ParseOptions) ([]map[string]interface{}, error) {
	p.sampleCalled = true
	return nil, fmt.Errorf("sample multi provider should not be called")
}
