package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

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

func TestTableTransferExecutorWritesShapefileComponents(t *testing.T) {
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
		SourceNativeReader:      reader,
		TargetContentWriter:     writer,
		TargetComponentProvider: shapefileformat.NewPlugin(nil),
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
			t.Fatalf("component %s was not written", path)
		}
	}
}

func TestTableTransferExecutorPreservesNativeSchemaForNativeTarget(t *testing.T) {
	reader := &fakeBatchReader{
		batches: []*engineplugin.BatchData{
			{
				Fields: []engineplugin.FieldInfo{
					{Name: "id", Type: "bigint", NativeType: "bigint"},
					{Name: "SmGeometry", Type: "geometry", NativeType: "geometry(MultiPolygon,4326)"},
				},
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
	if geom.Name != "SmGeometry" || geom.Type != "geometry" || geom.NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("prepared spatial field = %#v, want geometry native typmod", geom)
	}
	if len(writer.sessionFields) != 2 || writer.sessionFields[1].NativeType != "geometry(MultiPolygon,4326)" {
		t.Fatalf("session fields = %#v, want geometry native typmod", writer.sessionFields)
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
	preparedFields []engineplugin.FieldInfo
	sessionFields  []engineplugin.FieldInfo
	batches        []*engineplugin.BatchData
	deleted        bool
	closed         bool
	aborted        bool
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
