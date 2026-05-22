package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
)

func TestTableTransferExecutorWritesNativeTableToCSV(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{{Name: "id", Type: "int"}, {Name: "name", Type: "string"}},
				Rows: []map[string]interface{}{
					{"id": int64(1), "name": "Alice"},
					{"id": int64(2), "name": "Bob"},
				},
			},
			{
				Fields: []engineplugin.FieldInfo{{Name: "id", Type: "int"}, {Name: "name", Type: "string"}},
				Rows: []map[string]interface{}{
					{"id": int64(3), "name": "Carol"},
				},
			},
		},
	}
	writer := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:   reader,
		TargetContentWriter:  writer,
		TargetFormatProvider: csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointNative},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if got, want := writer.buf.String(), "id,name\n1,Alice\n2,Bob\n3,Carol\n"; got != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
	if metrics.RecordsRead != 3 || metrics.RecordsWritten != 3 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 3 read/written and 2 batches", metrics)
	}
	if !writer.closed {
		t.Fatal("target content writer was not closed")
	}
	if len(reader.offsets) != 2 || reader.offsets[0] != 0 || reader.offsets[1] != 2 {
		t.Fatalf("reader offsets = %#v, want [0 2]", reader.offsets)
	}
}

func TestTableTransferExecutorAppliesFieldMappingTransform(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{
					{Name: "id", Type: "int"},
					{Name: "name", Type: "string"},
					{Name: "geom", Type: "geometry"},
				},
				Rows: []map[string]interface{}{
					{"id": int64(1), "name": "Alice", "geom": "POINT (120 30)"},
					{"id": int64(2), "name": nil, "geom": "POINT (121 31)"},
				},
			},
		},
	}
	writer := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:   reader,
		TargetContentWriter:  writer,
		TargetFormatProvider: csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative},
		Target: TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Transforms: []TableTransformPlan{{
			Type: "field_mapping",
			FieldMapping: &FieldMappingTransformPlan{
				Mode: FieldMappingModeProject,
				Fields: []FieldMappingFieldPlan{
					{Source: "id", Target: "road_id", TargetType: "bigint", Nullable: false},
					{Source: "name", Target: "road_name", TargetType: "string", Nullable: true, Default: "unknown"},
					{Source: "geom", Target: "geometry", TargetType: "geometry", Nullable: false},
					{Target: "created_by", TargetType: "string", Nullable: false, Default: "transfer"},
				},
			},
		}},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got, want := writer.buf.String(), "road_id,road_name,geometry,created_by\n1,Alice,POINT (120 30),transfer\n2,unknown,POINT (121 31),transfer\n"; got != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want 2 read/written and 1 batch", metrics)
	}
}

func TestTableTransferExecutorReportsBatchProgress(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{{Name: "id", Type: "int"}},
				Rows: []map[string]interface{}{
					{"id": 1},
					{"id": 2},
				},
			},
			{
				Fields: []engineplugin.FieldInfo{{Name: "id", Type: "int"}},
				Offset: 2,
				Rows:   []map[string]interface{}{{"id": 3}},
			},
		},
	}
	writer := &fakeContentWriter{}
	var events []TableProgressEvent
	exec := &TableTransferExecutor{
		SourceNativeReader:   reader,
		TargetContentWriter:  writer,
		TargetFormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointNative},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 2,
		ProgressCallback: func(_ context.Context, event TableProgressEvent) error {
			events = append(events, event)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("progress events = %#v, want 2 events", events)
	}
	if events[0].BatchIndex != 1 || events[0].SourceOffset != 0 || events[0].BatchRows != 2 || events[0].RecordsRead != 2 || events[0].RecordsWritten != 2 {
		t.Fatalf("first progress event = %#v, want batch 1 offset 0 rows/read/written 2", events[0])
	}
	if events[1].BatchIndex != 2 || events[1].SourceOffset != 2 || events[1].BatchRows != 1 || events[1].RecordsRead != 3 || events[1].RecordsWritten != 3 {
		t.Fatalf("second progress event = %#v, want batch 2 offset 2 rows 1 read/written 3", events[1])
	}
}

func TestTableTransferExecutorNoRowsCreatesEmptyEncodedTarget(t *testing.T) {
	reader := &fakeBatchReader{batches: []*engineplugin.BatchData{{}}}
	writer := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:   reader,
		TargetContentWriter:  writer,
		TargetFormatProvider: csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointNative},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 10,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := writer.buf.String(); got != "" {
		t.Fatalf("csv output = %q, want empty", got)
	}
	if metrics.RecordsRead != 0 || metrics.RecordsWritten != 0 || metrics.Batches != 0 {
		t.Fatalf("metrics = %#v, want zero", metrics)
	}
	if !writer.closed {
		t.Fatal("target content writer was not closed")
	}
}

func TestTableTransferExecutorPrefersNativeTableReadSession(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{{Name: "id"}, {Name: "name"}},
				Rows: []map[string]interface{}{
					{"id": 1, "name": "Alice"},
					{"id": 2, "name": "Bob"},
				},
			},
			{
				Fields: []engineplugin.FieldInfo{{Name: "id"}, {Name: "name"}},
				Rows:   []map[string]interface{}{{"id": 3, "name": "Carol"}},
			},
		},
	}
	writer := &fakeContentWriter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:         &fakeBatchReader{},
		SourceTableSessionProvider: reader,
		TargetContentWriter:        writer,
		TargetFormatProvider:       csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointNative},
		Target:    TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got, want := writer.buf.String(), "id,name\n1,Alice\n2,Bob\n3,Carol\n"; got != want {
		t.Fatalf("csv output = %q, want %q", got, want)
	}
	if metrics.Batches != 2 || metrics.RecordsRead != 3 {
		t.Fatalf("metrics = %#v, want 2 batches and 3 rows", metrics)
	}
	if len(reader.sessionLimits) != 2 || reader.sessionLimits[0] != 2 || reader.sessionLimits[1] != 2 {
		t.Fatalf("session limits = %#v, want [2 2]", reader.sessionLimits)
	}
	if !reader.sessionClosed {
		t.Fatal("table read session was not closed")
	}
}

func TestTableTransferExecutorWritesShapefileRefs(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{
					{Name: "id", Type: "int"},
					{Name: "name", Type: "string"},
					{Name: "geom", Type: "geometry"},
				},
				Rows: []map[string]interface{}{
					{"id": 1, "name": "Alpha", "geom": "POINT (120 30)"},
					{"id": 2, "name": "Beta", "geom": "POINT (121 31)"},
				},
			},
		},
	}
	writer := &fakeContentWriter{files: map[string][]byte{}}
	exec := &TableTransferExecutor{
		SourceNativeReader:  reader,
		TargetContentWriter: writer,
		TargetMultiProvider: shapefileformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative},
		Target: TableTargetPlan{
			Kind:         TableEndpointEncoded,
			Path:         engineplugin.FileItemPath(2, "exports/cities.shp"),
			ContentWrite: engineplugin.WriteOptions{Overwrite: true},
			Format:       format.FormatShapefile,
			FormatOptions: &format.WriteOptions{
				Encoding: "utf-8",
				ExtraParams: map[string]interface{}{
					"geometry_field": "geom",
					"geometry_type":  "Point",
				},
			},
		},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 2 || metrics.RecordsWritten != 2 || metrics.Batches != 1 {
		t.Fatalf("metrics = %#v, want 2 read/written and 1 batch", metrics)
	}
	for _, path := range []string{"exports/cities.shp", "exports/cities.shx", "exports/cities.dbf", "exports/cities.cpg"} {
		if len(writer.files[path]) == 0 {
			t.Fatalf("ref %s was not written", path)
		}
	}
}

func TestTableTransferExecutorWritesShapefileZFromNativeSpatialDimension(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{
					{Name: "id", Type: "int"},
					{Name: "geom", Type: "geometry"},
				},
				Spatial: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 3),
				Rows: []map[string]interface{}{
					{"id": 1, "geom": "POINT Z (120 30 99.5)"},
				},
			},
		},
	}
	writer := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	exec := &TableTransferExecutor{
		SourceNativeReader:  reader,
		TargetContentWriter: writer,
		TargetMultiProvider: shapefilePlugin,
	}

	targetPath := engineplugin.FileItemPath(2, "exports/cities_z.shp")
	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative},
		Target: TableTargetPlan{
			Kind:         TableEndpointEncoded,
			Path:         targetPath,
			ContentWrite: engineplugin.WriteOptions{Overwrite: true},
			Format:       format.FormatShapefile,
			FormatOptions: &format.WriteOptions{
				Encoding: "utf-8",
				ExtraParams: map[string]interface{}{
					"geometry_field": "geom",
				},
			},
		},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 1 || metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v, want 1 read/written", metrics)
	}

	contentReader := contentadapter.NewReader(writer, nil, targetPath, engineplugin.ReadOptions{})
	refs := format.SameBasenameRelatedRefs(targetPath.StringPath(), shapefilePlugin.RelatedRefSpecs())
	info, err := shapefilePlugin.DescribeMultiTable(context.Background(), contentReader, refs, format.DefaultParseOptions())
	if err != nil {
		t.Fatalf("DescribeMultiTable failed: %v", err)
	}
	schema := format.TableSchemaFromDescribeResult(info)
	if schema.SpatialInfo == nil || schema.SpatialInfo.PrimaryDimensionValue() != 3 {
		t.Fatalf("spatial info = %#v, want dimension 3", schema.SpatialInfo)
	}
	rows, err := shapefilePlugin.SampleMultiTable(context.Background(), contentReader, refs, 0, 1, format.DefaultParseOptions())
	if err != nil {
		t.Fatalf("SampleMultiTable failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %#v, want one row", rows)
	}
	geometryColumn := schema.SpatialInfo.PrimaryGeometryName()
	got, ok := rows[0][geometryColumn].(string)
	if !ok {
		t.Fatalf("geometry value = %#v, want WKT string", rows[0][geometryColumn])
	}
	if got != "POINT Z (120 30 99.5)" {
		t.Fatalf("geometry = %q, want POINT Z", got)
	}
}

func TestTableTransferExecutorPreservesNativeSchemaForNativeTarget(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{
					{Name: "id", Type: "bigint"},
					{Name: "SmGeometry", Type: "geometry"},
				},
				Spatial: datatype.NewSingleGeometrySpatialInfo("SmGeometry", "MultiPolygon", 4326, 0),
				Rows: []map[string]interface{}{
					{"id": int64(1), "SmGeometry": "0106000020E610000000000000"},
				},
			},
		},
	}
	writer := &fakeNativeTableWriter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:         reader,
		SourceTableSessionProvider: reader,
		TargetNativePreparer:       writer,
		TargetNativeWriter:         writer,
		TargetTableSessionProvider: writer,
		TargetDeleteProvider:       writer,
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative},
		Target: TableTargetPlan{
			Kind:              TableEndpointNative,
			DeleteBeforeWrite: true,
			TableWrite:        engineplugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsWritten != 1 {
		t.Fatalf("metrics = %#v, want one written record", metrics)
	}
	if !writer.deleted {
		t.Fatal("target was not deleted before write")
	}
	if len(writer.preparedFields) != 2 {
		t.Fatalf("prepared fields = %#v, want 2 fields", writer.preparedFields)
	}
	geom := writer.preparedFields[1]
	if geom.Name != "SmGeometry" || geom.Type != "geometry" {
		t.Fatalf("prepared spatial field = %#v, want geometry field", geom)
	}
	if writer.preparedSpatialInfo.PrimaryGeometryType() != "MultiPolygon" || writer.preparedSpatialInfo.PrimarySRIDValue() != 4326 {
		t.Fatalf("prepared spatial info = %#v, want standard spatial facts", writer.preparedSpatialInfo)
	}
	if len(writer.sessionFields) != 2 || writer.sessionSpatialInfo.PrimaryGeometryType() != "MultiPolygon" {
		t.Fatalf("session fields = %#v, spatial info = %#v, want standard spatial facts", writer.sessionFields, writer.sessionSpatialInfo)
	}
}

func TestTableTransferExecutorTransformsNativeTargetSchema(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{
					{Name: "id", Type: "int"},
					{Name: "geom", Type: "geometry"},
				},
				Spatial: datatype.NewSingleGeometrySpatialInfo("geom", "Point", 4326, 0),
				Rows: []map[string]interface{}{
					{"id": 1, "geom": "POINT (120 30)"},
				},
			},
		},
	}
	writer := &fakeNativeTableWriter{}
	exec := &TableTransferExecutor{
		SourceNativeReader:         reader,
		SourceTableSessionProvider: reader,
		TargetNativePreparer:       writer,
		TargetNativeWriter:         writer,
		TargetTableSessionProvider: writer,
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative},
		Target: TableTargetPlan{
			Kind:       TableEndpointNative,
			TableWrite: engineplugin.BatchWriteOptions{Method: "copy"},
		},
		Transforms: []TableTransformPlan{{
			Type: "field_mapping",
			FieldMapping: &FieldMappingTransformPlan{
				Mode: FieldMappingModeProject,
				Fields: []FieldMappingFieldPlan{
					{Source: "id", Target: "road_id", TargetType: "bigint", Nullable: false},
					{Source: "geom", Target: "geometry", TargetType: "geometry", Nullable: false},
				},
			},
		}},
		BatchSize: 100,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(writer.preparedFields) != 2 {
		t.Fatalf("prepared fields = %#v, want 2 fields", writer.preparedFields)
	}
	if writer.preparedFields[0].Name != "road_id" || writer.preparedFields[0].Type != "bigint" {
		t.Fatalf("first prepared field = %#v, want road_id bigint", writer.preparedFields[0])
	}
	if writer.preparedFields[1].Name != "geometry" || writer.preparedFields[1].Type != "geometry" {
		t.Fatalf("second prepared field = %#v, want geometry geometry", writer.preparedFields[1])
	}
	if writer.preparedSpatialInfo.PrimaryGeometryName() != "geometry" || writer.preparedSpatialInfo.PrimaryGeometryType() != "Point" {
		t.Fatalf("prepared spatial info = %#v, want transformed geometry spatial facts", writer.preparedSpatialInfo)
	}
	if len(writer.batches) != 1 {
		t.Fatalf("session batches = %#v, want one batch", writer.batches)
	}
	if writer.batches[0].Spatial.PrimaryGeometryName() != "geometry" {
		t.Fatalf("batch spatial info = %#v, want transformed geometry column", writer.batches[0].Spatial)
	}
	if _, ok := writer.batches[0].Rows[0]["road_id"]; !ok {
		t.Fatalf("written row = %#v, want transformed road_id", writer.batches[0].Rows[0])
	}
	if _, ok := writer.batches[0].Rows[0]["id"]; ok {
		t.Fatalf("written row = %#v, should not contain source id in project mode", writer.batches[0].Rows[0])
	}
}

func TestNewTableTransferExecutorLoadsNativeToEncodedProvidersFromRegistry(t *testing.T) {
	source := &fakeBatchReader{engineType: "registry_source"}
	target := &fakeContentWriter{engineType: "registry_target"}
	engineplugin.Register(source)
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(source.Type())
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewTableTransferExecutor(source.Type(), target.Type(), "", format.FormatCSV)
	if err != nil {
		t.Fatalf("NewTableTransferExecutor failed: %v", err)
	}
	if exec.SourceNativeReader != source {
		t.Fatalf("reader = %#v, want registered source", exec.SourceNativeReader)
	}
	if exec.TargetContentWriter != target {
		t.Fatalf("writer = %#v, want registered target", exec.TargetContentWriter)
	}
	if exec.TargetFormatProvider.Format() != format.FormatCSV {
		t.Fatalf("format provider = %q, want csv", exec.TargetFormatProvider.Format())
	}
}

func TestTableTransferExecutorRejectsMissingNativeSourceCapability(t *testing.T) {
	target := &fakeContentWriter{engineType: "registry_target_only"}
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewTableTransferExecutor(target.Type(), target.Type(), "", format.FormatCSV)
	if err != nil {
		t.Fatalf("NewTableTransferExecutor failed: %v", err)
	}
	_, err = exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointNative},
		Target: TableTargetPlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
	})
	if err == nil {
		t.Fatal("Execute succeeded, want source capability error")
	}
	if !strings.Contains(err.Error(), "native table source requires batch reader") {
		t.Fatalf("error = %q, want batch table read capability error", err)
	}
}

type fakeBatchReader struct {
	engineType     string
	batches        []*engineplugin.BatchData
	offsets        []int64
	sessionLimits  []int
	sessionClosed  bool
	sessionOptions engineplugin.TableReadSessionOptions
}

func (r *fakeBatchReader) Type() string {
	if r.engineType != "" {
		return r.engineType
	}
	return "fake_reader"
}

func (r *fakeBatchReader) DisplayName() string { return "Fake Reader" }

func (r *fakeBatchReader) EngineOrigin() string { return "general" }

func (r *fakeBatchReader) DefaultPort() int { return 0 }

func (r *fakeBatchReader) RequiredFields() []string { return nil }

func (r *fakeBatchReader) SensitiveFields() []string { return nil }

func (r *fakeBatchReader) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (r *fakeBatchReader) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (r *fakeBatchReader) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (r *fakeBatchReader) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (r *fakeBatchReader) ReadBatch(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.BatchReadOptions) (*engineplugin.BatchData, error) {
	r.offsets = append(r.offsets, opts.Offset)
	if len(r.batches) == 0 {
		return &engineplugin.BatchData{}, nil
	}
	batch := r.batches[0]
	r.batches = r.batches[1:]
	return batch, nil
}

func (r *fakeBatchReader) OpenTableReadSession(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.TableReadSessionOptions) (engineplugin.TableReadSession, error) {
	r.sessionOptions = opts
	return &fakeTableReadSession{reader: r}, nil
}

type fakeTableReadSession struct {
	reader *fakeBatchReader
}

func (s *fakeTableReadSession) ReadBatch(_ context.Context, limit int) (*engineplugin.BatchData, error) {
	s.reader.sessionLimits = append(s.reader.sessionLimits, limit)
	if len(s.reader.batches) == 0 {
		return &engineplugin.BatchData{}, nil
	}
	batch := s.reader.batches[0]
	s.reader.batches = s.reader.batches[1:]
	return batch, nil
}

func (s *fakeTableReadSession) Close(context.Context) error {
	s.reader.sessionClosed = true
	return nil
}

type fakeContentWriter struct {
	engineType string
	buf        bytes.Buffer
	files      map[string][]byte
	closed     bool
}

type fakeNativeTableWriter struct {
	preparedFields      []engineplugin.FieldInfo
	preparedSpatialInfo *datatype.SpatialInfo
	sessionFields       []engineplugin.FieldInfo
	sessionSpatialInfo  *datatype.SpatialInfo
	batches             []*engineplugin.BatchData
	deleted             bool
	closed              bool
	aborted             bool
}

func (w *fakeNativeTableWriter) Type() string { return "fake_native_table_writer" }

func (w *fakeNativeTableWriter) DisplayName() string { return "Fake Native Table Writer" }

func (w *fakeNativeTableWriter) EngineOrigin() string { return "general" }

func (w *fakeNativeTableWriter) DefaultPort() int { return 0 }

func (w *fakeNativeTableWriter) RequiredFields() []string { return nil }

func (w *fakeNativeTableWriter) SensitiveFields() []string { return nil }

func (w *fakeNativeTableWriter) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (w *fakeNativeTableWriter) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (w *fakeNativeTableWriter) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (w *fakeNativeTableWriter) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (w *fakeNativeTableWriter) PrepareTableWrite(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.TableWriteOptions) error {
	w.preparedFields = append([]engineplugin.FieldInfo(nil), opts.Fields...)
	w.preparedSpatialInfo = opts.SpatialInfo.Clone()
	return nil
}

func (w *fakeNativeTableWriter) DeleteResource(context.Context, engineplugin.ConnectionInfo, engineplugin.CatalogPath) error {
	w.deleted = true
	return nil
}

func (w *fakeNativeTableWriter) WriteBatch(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, batch *engineplugin.BatchData, _ engineplugin.BatchWriteOptions) error {
	w.batches = append(w.batches, batch)
	return nil
}

func (w *fakeNativeTableWriter) OpenTableWriteSession(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.TableWriteSessionOptions) (engineplugin.TableWriteSession, error) {
	w.sessionFields = append([]engineplugin.FieldInfo(nil), opts.Fields...)
	w.sessionSpatialInfo = opts.SpatialInfo.Clone()
	return &fakeNativeTableWriteSession{writer: w}, nil
}

type fakeNativeTableWriteSession struct {
	writer *fakeNativeTableWriter
}

func (s *fakeNativeTableWriteSession) WriteBatch(_ context.Context, batch *engineplugin.BatchData) error {
	s.writer.batches = append(s.writer.batches, batch)
	return nil
}

func (s *fakeNativeTableWriteSession) Close(context.Context) error {
	s.writer.closed = true
	return nil
}

func (s *fakeNativeTableWriteSession) Abort(context.Context) error {
	s.writer.aborted = true
	return nil
}

func (w *fakeContentWriter) Type() string {
	if w.engineType != "" {
		return w.engineType
	}
	return "fake_writer"
}

func (w *fakeContentWriter) DisplayName() string { return "Fake Writer" }

func (w *fakeContentWriter) EngineOrigin() string { return "general" }

func (w *fakeContentWriter) DefaultPort() int { return 0 }

func (w *fakeContentWriter) RequiredFields() []string { return nil }

func (w *fakeContentWriter) SensitiveFields() []string { return nil }

func (w *fakeContentWriter) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (w *fakeContentWriter) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (w *fakeContentWriter) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (w *fakeContentWriter) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (w *fakeContentWriter) CreateContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.WriteOptions) (io.WriteCloser, error) {
	if w.files != nil {
		buf := &bytes.Buffer{}
		return fakeWriteCloser{Writer: buf, close: func() {
			w.files[path.StringPath()] = append([]byte(nil), buf.Bytes()...)
			w.closed = true
		}}, nil
	}
	return fakeWriteCloser{Writer: &w.buf, close: func() { w.closed = true }}, nil
}

func (w *fakeContentWriter) OpenContent(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, _ engineplugin.ReadOptions) (io.ReadCloser, error) {
	if w.files == nil {
		return io.NopCloser(bytes.NewReader(w.buf.Bytes())), nil
	}
	data, ok := w.files[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("fake content %s not found", path.StringPath())
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (w *fakeContentWriter) OpenRange(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath, opts engineplugin.ReadOptions) (io.ReadCloser, error) {
	if w.files == nil {
		data := w.buf.Bytes()
		end := opts.Offset + opts.Length
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		return io.NopCloser(bytes.NewReader(data[opts.Offset:end])), nil
	}
	data, ok := w.files[path.StringPath()]
	if !ok {
		return nil, fmt.Errorf("fake content %s not found", path.StringPath())
	}
	end := opts.Offset + opts.Length
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return io.NopCloser(bytes.NewReader(data[opts.Offset:end])), nil
}

func (w *fakeContentWriter) ListChildren(_ context.Context, _ engineplugin.ConnectionInfo, parent engineplugin.CatalogPath, _ engineplugin.ListOptions) ([]engineplugin.CatalogNode, error) {
	if w.files == nil {
		return nil, nil
	}
	parentPath := strings.Trim(parent.StringPath(), "/")
	dirs := map[string]bool{}
	nodes := make([]engineplugin.CatalogNode, 0)
	for path := range w.files {
		trimmed := strings.Trim(path, "/")
		if parentPath != "" {
			if !strings.HasPrefix(trimmed, parentPath+"/") {
				continue
			}
			trimmed = strings.TrimPrefix(trimmed, parentPath+"/")
		}
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/") {
			name := strings.Split(trimmed, "/")[0]
			if dirs[name] {
				continue
			}
			dirs[name] = true
			dirPath := parentPath
			if dirPath != "" {
				dirPath += "/"
			}
			dirPath += name
			nodes = append(nodes, engineplugin.CatalogNode{
				Name:        name,
				Path:        engineplugin.FileDirectoryPath(parent.EngineID, dirPath),
				Term:        engineplugin.CatalogTermDirectory,
				Kind:        engineplugin.CatalogKindDirectory,
				IsContainer: true,
			})
			continue
		}
		filePath := parentPath
		if filePath != "" {
			filePath += "/"
		}
		filePath += trimmed
		nodes = append(nodes, engineplugin.CatalogNode{
			Name:   trimmed,
			Path:   engineplugin.FileItemPath(parent.EngineID, filePath),
			Term:   engineplugin.CatalogTermFile,
			Kind:   engineplugin.CatalogKindFile,
			IsItem: true,
		})
	}
	return nodes, nil
}

func (w *fakeContentWriter) ResolvePath(_ context.Context, _ engineplugin.ConnectionInfo, path engineplugin.CatalogPath) (*engineplugin.CatalogNode, error) {
	return &engineplugin.CatalogNode{
		Name:        path.StringPath(),
		Path:        path,
		Term:        engineplugin.CatalogTermDirectory,
		Kind:        engineplugin.CatalogKindDirectory,
		IsContainer: true,
	}, nil
}

type fakeWriteCloser struct {
	io.Writer
	close func()
}

func (w fakeWriteCloser) Close() error {
	if w.close != nil {
		w.close()
	}
	return nil
}
