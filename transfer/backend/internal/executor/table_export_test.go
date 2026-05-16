package executor

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
)

func TestTableExportExecutorExecuteWritesCSV(t *testing.T) {
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
	exec := &TableExportExecutor{
		Reader:         reader,
		Writer:         writer,
		FormatProvider: csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableExportPlan{
		BatchSize: 2,
		Format:    format.FormatCSV,
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

func TestTableExportExecutorExecuteNoRowsCreatesEmptyTarget(t *testing.T) {
	reader := &fakeBatchReader{batches: []*engineplugin.BatchData{{}}}
	writer := &fakeContentWriter{}
	exec := &TableExportExecutor{
		Reader:         reader,
		Writer:         writer,
		FormatProvider: csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableExportPlan{BatchSize: 10})
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

func TestTableExportExecutorPrefersTableReadSession(t *testing.T) {
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
	exec := &TableExportExecutor{
		Reader:               &fakeBatchReader{},
		TableSessionProvider: reader,
		Writer:               writer,
		FormatProvider:       csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableExportPlan{BatchSize: 2, Format: format.FormatCSV})
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

func TestTableExportExecutorRejectsMismatchedFormat(t *testing.T) {
	exec := &TableExportExecutor{
		Reader:         &fakeBatchReader{},
		Writer:         &fakeContentWriter{},
		FormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableExportPlan{Format: format.FormatTSV})
	if err == nil {
		t.Fatal("Execute succeeded, want format mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match table writer provider format") {
		t.Fatalf("error = %q, want format mismatch", err)
	}
}

func TestNewTableExportExecutorFromRegistry(t *testing.T) {
	source := &fakeBatchReader{engineType: "registry_source"}
	target := &fakeContentWriter{engineType: "registry_target"}
	engineplugin.Register(source)
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(source.Type())
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewTableExportExecutor(source.Type(), target.Type(), format.FormatCSV)
	if err != nil {
		t.Fatalf("NewTableExportExecutor failed: %v", err)
	}
	if exec.Reader != source {
		t.Fatalf("reader = %#v, want registered source", exec.Reader)
	}
	if exec.Writer != target {
		t.Fatalf("writer = %#v, want registered target", exec.Writer)
	}
	if exec.FormatProvider.Format() != format.FormatCSV {
		t.Fatalf("format provider = %q, want csv", exec.FormatProvider.Format())
	}
}

func TestNewTableExportExecutorRejectsMissingCapability(t *testing.T) {
	target := &fakeContentWriter{engineType: "registry_target_only"}
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(target.Type())
	})

	_, err := NewTableExportExecutor(target.Type(), target.Type(), format.FormatCSV)
	if err == nil {
		t.Fatal("NewTableExportExecutor succeeded, want source capability error")
	}
	if !strings.Contains(err.Error(), "does not implement batch table read") {
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
	closed     bool
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

func (w *fakeContentWriter) CreateContent(context.Context, engineplugin.ConnectionInfo, engineplugin.CatalogPath, engineplugin.WriteOptions) (io.WriteCloser, error) {
	return fakeWriteCloser{Writer: &w.buf, close: func() { w.closed = true }}, nil
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
