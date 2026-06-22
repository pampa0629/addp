package executor

import (
	"context"
	"github.com/addp/common/datatype"
	"strings"
	"testing"

	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
	jsonformat "github.com/addp/common/format/plugins/json"
	parquetformat "github.com/addp/common/format/plugins/parquet"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
	"github.com/addp/common/resume"
)

const testWGS84CRSDefinition = `GEOGCS["WGS 84",DATUM["WGS_1984",SPHEROID["WGS 84",6378137,298.257223563,AUTHORITY["EPSG","7030"]],AUTHORITY["EPSG","6326"]],PRIMEM["Greenwich",0,AUTHORITY["EPSG","8901"]],UNIT["degree",0.0174532925199433,AUTHORITY["EPSG","9122"]],AUTHORITY["EPSG","4326"]]`

func TestTableTransferExecutorConvertsEncodedCSVToJSONL(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n3,Carol\n"}
	writer := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceContentReader:       reader,
		TargetContentWriter:       writer,
		SourceTableReadProvider:   csvformat.NewPlugin(nil),
		TargetTableWriterProvider: jsonformat.NewPlugin(nil),
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

func TestEncodedTargetRelatedRefMappingUsesBucketlessObjectKeys(t *testing.T) {
	path := engineplugin.ObjectItemPath(0, "manager", "tenant_1/export/session/test.shp")
	refBasePath, mapper := encodedTargetRelatedRefMapping(path)
	if refBasePath != "tenant_1/export/session/test.shp" {
		t.Fatalf("ref base path = %q, want bucketless object key", refBasePath)
	}
	if mapper == nil {
		t.Fatal("mapper is nil, want object bucket mapper")
	}
	mapped, err := mapper(format.SameBasenameRelatedRefs(refBasePath, []format.RelatedRefSpec{
		{Extension: ".dbf", Role: "attributes", Required: true},
	})[0].Ref)
	if err != nil {
		t.Fatalf("mapper failed: %v", err)
	}
	if got := mapped.StringPath(); got != "manager/tenant_1/export/session/test.dbf" {
		t.Fatalf("mapped path = %q, want bucket/object catalog path", got)
	}
}

func TestTablePipelineProgressCarriesResumeAndCommitMarkers(t *testing.T) {
	sourceMarker := &resume.Marker{
		Version:      resume.MarkerVersionV1,
		Provider:     "test.source",
		PositionUnit: "row",
		ReadPosition: map[string]interface{}{"row_offset": int64(2)},
	}
	commitMarker := &resume.Marker{
		Version:        resume.MarkerVersionV1,
		Provider:       "test.target",
		PositionUnit:   "session_commit",
		CommitPosition: map[string]interface{}{"rows_committed": int64(2)},
	}
	source := &markerTableBatchSource{marker: sourceMarker}
	target := &markerTableBatchTarget{marker: commitMarker}
	events := make([]TableProgressEvent, 0)
	pipeline := &TablePipeline{
		Source:    source,
		Target:    target,
		BatchSize: 2,
		ProgressCallback: func(_ context.Context, event TableProgressEvent) error {
			events = append(events, event)
			return nil
		},
	}

	metrics, err := pipeline.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want one batch of two rows", metrics)
	}
	if len(events) != 2 {
		t.Fatalf("events = %#v, want batch event and final event", events)
	}
	if events[0].Final || events[0].ResumeMarker == nil || events[0].CommitMarker != nil {
		t.Fatalf("batch event markers = %#v, want resume marker only", events[0])
	}
	if !events[1].Final || events[1].ResumeMarker == nil || events[1].CommitMarker == nil {
		t.Fatalf("final event markers = %#v, want both resume and commit markers", events[1])
	}
	if events[1].CommitMarker.Provider != "test.target" {
		t.Fatalf("final commit marker = %#v, want test.target", events[1].CommitMarker)
	}
	if events[0].ResumeMarker == sourceMarker || events[1].CommitMarker == commitMarker {
		t.Fatal("progress event reused provider marker pointer, want cloned markers")
	}
}

func TestTableTransferExecutorReadsShapefileRefs(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	target := engineplugin.FileItemPath(7, "imports/cities.shp")
	contentWriter := contentadapter.NewWriter(source, nil, target, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(target.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), contentWriter, refs, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}, &format.WriteOptions{SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 0, 0)})
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
		SourceContentReader:       source,
		TargetContentWriter:       output,
		SourceMultiReadProvider:   shapefilePlugin,
		TargetTableWriterProvider: csvformat.NewPlugin(nil),
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
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), sourceWriter, sourceRefs, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}, &format.WriteOptions{SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 0, 0)})
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
		SourceContentReader:       source,
		TargetContentWriter:       output,
		SourceMultiReadProvider:   shapefilePlugin,
		TargetTableWriterProvider: csvformat.NewPlugin(nil),
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
		SourceContentReader:       &fakeContentReader{},
		TargetContentWriter:       &fakeContentWriter{},
		TargetTableWriterProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:   TableEndpointEncoded,
			Format: format.FormatCSV,
			Layout: format.LayoutWhole,
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
	openCounts := map[string]int{}
	rangeOpenCounts := map[string]int{}
	source := &fakeContentWriter{
		files:           map[string][]byte{},
		openCounts:      openCounts,
		rangeOpenCounts: rangeOpenCounts,
	}
	writeParquetTestFile(t, source, engineplugin.FileItemPath(7, "datasets/orders/part-000.parquet"), parquetPlugin, []map[string]interface{}{
		{"id": 1, "name": "Alice"},
		{"id": 2, "name": "Bob"},
	})
	writeParquetTestFile(t, source, engineplugin.FileItemPath(7, "datasets/orders/dt=2026-05-21/part-001.parquet"), parquetPlugin, []map[string]interface{}{
		{"id": 3, "name": "Carol"},
	})

	output := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceContentReader:       source,
		TargetContentWriter:       output,
		SourceScopeReadProvider:   parquetPlugin,
		TargetTableWriterProvider: csvformat.NewPlugin(nil),
	}
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{
			Kind:   TableEndpointEncoded,
			Path:   engineplugin.FileDirectoryPath(7, "datasets/orders"),
			Format: format.FormatParquet,
			Layout: format.LayoutWhole,
			TableInfo: &datatype.TableInfo{
				Fields: []datatype.FieldInfo{
					{Name: "id", Type: datatype.FieldTypeInt},
					{Name: "name", Type: datatype.FieldTypeString},
				},
			},
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
	for _, path := range []string{"datasets/orders/part-000.parquet", "datasets/orders/dt=2026-05-21/part-001.parquet"} {
		if openCounts[path] != 0 {
			t.Fatalf("regular open count for %s = %d, want 0 for range-backed whole scope read", path, openCounts[path])
		}
		if rangeOpenCounts[path] == 0 {
			t.Fatalf("range open count for %s = %d, want > 0", path, rangeOpenCounts[path])
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
	tableWriter, err := plugin.OpenTableWriter(context.Background(), output, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString},
		},
	}, nil)
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
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), sourceWriter, sourceRefs, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geometry", Type: datatype.FieldTypeGeometry},
		},
	}, &format.WriteOptions{
		Encoding: "utf-8",
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo(
			"geometry",
			"Point",
			4326,
			3,
		),
		ExtraParams: map[string]interface{}{
			format.CRSDefinitionOptionKey: testWGS84CRSDefinition,
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
	spatialInfo := info.Spatial
	if spatialInfo == nil || spatialInfo.PrimaryDimensionValue() != 3 || spatialInfo.PrimaryGeometryType() != "Point" {
		t.Fatalf("spatial info = %#v, want Point dimension 3", spatialInfo)
	}
	rows, err := shapefilePlugin.SampleMultiTable(context.Background(), targetReader, targetRefs, 0, 1, format.DefaultParseOptions())
	if err != nil {
		t.Fatalf("SampleMultiTable failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want one row", rows)
	}
	geometryColumn := spatialInfo.PrimaryGeometryName()
	got, ok := rows[0][geometryColumn].(string)
	if !ok {
		t.Fatalf("geometry value = %#v, want WKT string", rows[0][geometryColumn])
	}
	if got != "POINT Z (120 30 99.5)" {
		t.Fatalf("geometry = %q, want POINT Z", got)
	}
}

func TestTableTransferExecutorCopiesShapefileRefsAcrossStoragePathModels(t *testing.T) {
	tests := []struct {
		name       string
		sourcePath engineplugin.CatalogPath
		targetPath engineplugin.CatalogPath
	}{
		{
			name:       "file_to_object",
			sourcePath: engineplugin.FileItemPath(7, "imports/roads_z.shp"),
			targetPath: engineplugin.ObjectItemPath(8, "addp", "gis/roads_z.shp"),
		},
		{
			name:       "object_to_file",
			sourcePath: engineplugin.ObjectItemPath(8, "addp", "imports/cities_z.shp"),
			targetPath: engineplugin.FileItemPath(7, "exports/cities_z.shp"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			source := &fakeContentWriter{files: map[string][]byte{}}
			target := &fakeContentWriter{files: map[string][]byte{}}
			shapefilePlugin := shapefileformat.NewPlugin(nil)
			writeShapefilePointZTestContent(t, source, tt.sourcePath, shapefilePlugin)

			exec := &TableTransferExecutor{
				SourceContentReader:     source,
				TargetContentWriter:     target,
				SourceMultiReadProvider: shapefilePlugin,
				TargetMultiProvider:     shapefilePlugin,
			}
			metrics, err := exec.Execute(context.Background(), TableTransferPlan{
				Source:    TableSourcePlan{Kind: TableEndpointEncoded, Path: tt.sourcePath, Format: format.FormatShapefile},
				Target:    TableTargetPlan{Kind: TableEndpointEncoded, Path: tt.targetPath, Format: format.FormatShapefile},
				BatchSize: 1,
			})
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 || metrics.Batches != 1 {
				t.Fatalf("metrics = %#v, want 1 read/written and 1 batch", metrics)
			}

			targetRefs := format.SameBasenameRelatedRefs(tt.targetPath.StringPath(), shapefilePlugin.RelatedRefSpecs())
			for _, ref := range targetRefs {
				if ref.Required && len(target.files[ref.Ref.Path]) == 0 {
					t.Fatalf("target required ref %s was not written", ref.Ref.Path)
				}
			}
			targetReader := contentadapter.NewReader(target, nil, tt.targetPath, engineplugin.ReadOptions{})
			info, err := shapefilePlugin.DescribeMultiTable(context.Background(), targetReader, targetRefs, format.DefaultParseOptions())
			if err != nil {
				t.Fatalf("DescribeMultiTable failed: %v", err)
			}
			spatialInfo := info.Spatial
			if spatialInfo == nil || spatialInfo.PrimaryDimensionValue() != 3 || spatialInfo.PrimaryGeometryType() != "Point" {
				t.Fatalf("spatial info = %#v, want Point dimension 3", spatialInfo)
			}
			rows, err := shapefilePlugin.SampleMultiTable(context.Background(), targetReader, targetRefs, 0, 1, format.DefaultParseOptions())
			if err != nil {
				t.Fatalf("SampleMultiTable failed: %v", err)
			}
			geometryColumn := spatialInfo.PrimaryGeometryName()
			if len(rows) != 1 || rows[0][geometryColumn] != "POINT Z (120 30 99.5)" {
				t.Fatalf("rows = %#v, want copied point z geometry", rows)
			}
		})
	}
}

func writeShapefilePointZTestContent(t *testing.T, storage *fakeContentWriter, path engineplugin.CatalogPath, shapefilePlugin *shapefileformat.Plugin) {
	t.Helper()
	writer := contentadapter.NewWriter(storage, nil, path, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(path.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), writer, refs, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geometry", Type: datatype.FieldTypeGeometry},
		},
	}, &format.WriteOptions{
		Encoding:    "utf-8",
		SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geometry", "Point", 4326, 3),
		ExtraParams: map[string]interface{}{
			format.CRSDefinitionOptionKey: testWGS84CRSDefinition,
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
}

func TestTableTransferExecutorPrefersMultiTableProvider(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	target := engineplugin.FileItemPath(7, "imports/cities.shp")
	contentWriter := contentadapter.NewWriter(source, nil, target, engineplugin.WriteOptions{Overwrite: true})
	refs := format.SameBasenameRelatedRefs(target.StringPath(), shapefilePlugin.RelatedRefSpecs())
	tableWriter, err := shapefilePlugin.OpenMultiTableWriter(context.Background(), contentWriter, refs, &datatype.TableInfo{
		Fields: []datatype.FieldInfo{
			{Name: "id", Type: datatype.FieldTypeInt},
			{Name: "name", Type: datatype.FieldTypeString, Size: 32},
			{Name: "geom", Type: datatype.FieldTypeGeometry},
		},
	}, &format.WriteOptions{SpatialInfo: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 0, 0)})
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
		SourceContentReader:       source,
		TargetContentWriter:       output,
		SourceMultiReadProvider:   shapefilePlugin,
		TargetTableWriterProvider: csvformat.NewPlugin(nil),
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
	if exec.TargetTableWriterProvider.Format() != format.FormatJSON {
		t.Fatalf("table writer provider = %q, want json", exec.TargetTableWriterProvider.Format())
	}
}

type markerTableBatchSource struct {
	marker *resume.Marker
}

func (s *markerTableBatchSource) Open(context.Context) (TableBatchReader, error) {
	return &markerTableBatchReader{
		marker: s.marker,
		batches: []*engineplugin.BatchData{
			{
				Rows: []map[string]interface{}{
					{"id": 1},
					{"id": 2},
				},
				Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}},
				Offset: 0,
			},
			{},
		},
	}, nil
}

type markerTableBatchReader struct {
	marker  *resume.Marker
	batches []*engineplugin.BatchData
}

func (r *markerTableBatchReader) TableInfo() *datatype.TableInfo {
	return &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}}}
}

func (r *markerTableBatchReader) SpatialInfo() *datatype.SpatialInfo {
	return nil
}

func (r *markerTableBatchReader) ReadBatch(context.Context, int) (*engineplugin.BatchData, error) {
	if len(r.batches) == 0 {
		return &engineplugin.BatchData{}, nil
	}
	batch := r.batches[0]
	r.batches = r.batches[1:]
	return batch, nil
}

func (r *markerTableBatchReader) Close(context.Context) error {
	return nil
}

func (r *markerTableBatchReader) ResumeMarker() *resume.Marker {
	return r.marker
}

type markerTableBatchTarget struct {
	marker *resume.Marker
}

func (t *markerTableBatchTarget) Open(context.Context, *datatype.TableInfo, *datatype.SpatialInfo) (TableBatchWriter, error) {
	return &markerTableBatchWriter{marker: t.marker}, nil
}

type markerTableBatchWriter struct {
	marker *resume.Marker
	closed bool
}

func (w *markerTableBatchWriter) WriteBatch(context.Context, *engineplugin.BatchData) error {
	return nil
}

func (w *markerTableBatchWriter) Close(context.Context) error {
	w.closed = true
	return nil
}

func (w *markerTableBatchWriter) Abort(context.Context) error {
	return nil
}

func (w *markerTableBatchWriter) CommitMarker() *resume.Marker {
	if !w.closed {
		return nil
	}
	return w.marker
}

func (w *markerTableBatchWriter) TargetRefs() []format.RelatedRef {
	return nil
}
