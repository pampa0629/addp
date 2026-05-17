package executor

import (
	"context"
	"io"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
)

func TestTableTransferExecutorWritesEncodedCSVToNativeTable(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n3,Carol\n"}
	writer := &fakeBatchWriter{}
	preparer := &fakeTableWritePreparer{}
	exec := &TableTransferExecutor{
		SourceContentReader:     reader,
		SourceFormatProvider:    csvformat.NewPlugin(nil),
		SourceTableReadProvider: csvformat.NewPlugin(nil),
		TargetNativePreparer:    preparer,
		TargetNativeWriter:      writer,
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Target:    TableTargetPlan{Kind: TableEndpointNative},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if metrics.RecordsRead != 3 || metrics.RecordsWritten != 3 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 3 read/written and 2 batches", metrics)
	}
	if len(writer.batches) != 2 {
		t.Fatalf("written batches = %d, want 2", len(writer.batches))
	}
	if got := writer.batches[0].Rows[0]["name"]; got != "Alice" {
		t.Fatalf("first row name = %#v, want Alice", got)
	}
	if len(reader.opens) != 1 || reader.opens[0] != 0 {
		t.Fatalf("source open offsets = %#v, want one full read", reader.opens)
	}
	if len(preparer.fields) != 2 || preparer.fields[0].Name != "id" {
		t.Fatalf("prepare fields = %#v, want CSV inferred fields", preparer.fields)
	}
}

func TestTableTransferExecutorPreparesNativeTargetOnce(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n"}
	preparer := &fakeTableWritePreparer{}
	exec := &TableTransferExecutor{
		SourceContentReader:     reader,
		SourceFormatProvider:    csvformat.NewPlugin(nil),
		SourceInfoProvider:      csvformat.NewPlugin(nil),
		SourceTableReadProvider: csvformat.NewPlugin(nil),
		TargetNativePreparer:    preparer,
		TargetNativeWriter:      &fakeBatchWriter{},
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Target:    TableTargetPlan{Kind: TableEndpointNative},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(preparer.fields) != 2 || preparer.fields[0].Name != "id" || preparer.fields[0].Type != "int" {
		t.Fatalf("prepare fields = %#v, want CSV inferred fields", preparer.fields)
	}
}

func TestTableTransferExecutorPrepareAppendUsesTableInfo(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n"}
	preparer := &fakeTableWritePreparer{}
	exec := &TableTransferExecutor{
		SourceContentReader:     reader,
		SourceFormatProvider:    csvformat.NewPlugin(nil),
		SourceInfoProvider:      csvformat.NewPlugin(nil),
		SourceTableReadProvider: csvformat.NewPlugin(nil),
		TargetNativePreparer:    preparer,
		TargetNativeWriter:      &fakeBatchWriter{},
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source:    TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Target:    TableTargetPlan{Kind: TableEndpointNative},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(preparer.fields) != 2 || preparer.fields[0].Name != "id" || preparer.fields[0].Type != "int" {
		t.Fatalf("prepare fields = %#v, want CSV inferred fields", preparer.fields)
	}
	if len(reader.opens) != 2 {
		t.Fatalf("source open count = %d, want one describe open and one data reader open", len(reader.opens))
	}
}

func TestTableTransferExecutorPrefersNativeTableWriteSessionForCopy(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n3,Carol\n"}
	writer := &fakeBatchWriter{}
	preparer := &fakeTableWritePreparer{}
	exec := &TableTransferExecutor{
		SourceContentReader:        reader,
		SourceFormatProvider:       csvformat.NewPlugin(nil),
		SourceInfoProvider:         csvformat.NewPlugin(nil),
		SourceTableReadProvider:    csvformat.NewPlugin(nil),
		TargetNativePreparer:       preparer,
		TargetNativeWriter:         writer,
		TargetTableSessionProvider: writer,
	}

	metrics, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Target: TableTargetPlan{
			Kind:       TableEndpointNative,
			TableWrite: engineplugin.BatchWriteOptions{Method: "copy"},
		},
		BatchSize: 2,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if metrics.RecordsRead != 3 || metrics.RecordsWritten != 3 || metrics.Batches != 2 {
		t.Fatalf("metrics = %#v, want 3 read/written and 2 batches", metrics)
	}
	if len(writer.batches) != 0 {
		t.Fatalf("direct batches = %d, want 0 when session is used", len(writer.batches))
	}
	if len(writer.sessionBatches) != 2 {
		t.Fatalf("session batches = %d, want 2", len(writer.sessionBatches))
	}
	if !writer.sessionClosed || writer.sessionAborted {
		t.Fatalf("session closed=%v aborted=%v, want closed only", writer.sessionClosed, writer.sessionAborted)
	}
	if writer.sessionOptions.Method != "copy" {
		t.Fatalf("session method = %q, want copy", writer.sessionOptions.Method)
	}
	if len(writer.sessionOptions.Fields) != 2 || writer.sessionOptions.Fields[0].Name != "id" {
		t.Fatalf("session fields = %#v, want prepared fields", writer.sessionOptions.Fields)
	}
}

func TestTableTransferExecutorRequiresNativePreparer(t *testing.T) {
	exec := &TableTransferExecutor{
		SourceContentReader:  &fakeContentReader{},
		SourceFormatProvider: csvformat.NewPlugin(nil),
		TargetNativeWriter:   &fakeBatchWriter{},
	}

	_, err := exec.Execute(context.Background(), TableTransferPlan{
		Source: TableSourcePlan{Kind: TableEndpointEncoded, Format: format.FormatCSV},
		Target: TableTargetPlan{Kind: TableEndpointNative},
	})
	if err == nil {
		t.Fatal("Execute succeeded, want missing preparer error")
	}
	if !strings.Contains(err.Error(), "table write prepare") {
		t.Fatalf("error = %q, want table write prepare", err)
	}
}

func TestNewTableTransferExecutorLoadsEncodedToNativeProvidersFromRegistry(t *testing.T) {
	source := &fakeContentReader{engineType: "registry_import_source"}
	target := &fakeBatchWriter{engineType: "registry_import_target"}
	engineplugin.Register(source)
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(source.Type())
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewTableTransferExecutor(source.Type(), target.Type(), format.FormatCSV, "")
	if err != nil {
		t.Fatalf("NewTableTransferExecutor failed: %v", err)
	}
	if exec.SourceContentReader != source {
		t.Fatalf("reader = %#v, want registered source", exec.SourceContentReader)
	}
	if exec.TargetNativeWriter != target {
		t.Fatalf("writer = %#v, want registered target", exec.TargetNativeWriter)
	}
	if exec.SourceFormatProvider.Format() != format.FormatCSV {
		t.Fatalf("format provider = %q, want csv", exec.SourceFormatProvider.Format())
	}
	if exec.SourceTableReadProvider == nil || exec.SourceTableReadProvider.Format() != format.FormatCSV {
		t.Fatalf("table read provider = %#v, want csv", exec.SourceTableReadProvider)
	}
}

type fakeContentReader struct {
	engineType string
	content    string
	opens      []int64
}

func (r *fakeContentReader) Type() string {
	if r.engineType != "" {
		return r.engineType
	}
	return "fake_content_reader"
}

func (r *fakeContentReader) DisplayName() string { return "Fake Content Reader" }

func (r *fakeContentReader) EngineOrigin() string { return "general" }

func (r *fakeContentReader) DefaultPort() int { return 0 }

func (r *fakeContentReader) RequiredFields() []string { return nil }

func (r *fakeContentReader) SensitiveFields() []string { return nil }

func (r *fakeContentReader) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (r *fakeContentReader) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (r *fakeContentReader) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (r *fakeContentReader) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (r *fakeContentReader) OpenContent(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.ReadOptions) (io.ReadCloser, error) {
	r.opens = append(r.opens, opts.Offset)
	return io.NopCloser(strings.NewReader(r.content)), nil
}

type fakeBatchWriter struct {
	engineType     string
	batches        []*engineplugin.BatchData
	sessionOptions engineplugin.TableWriteSessionOptions
	sessionBatches []*engineplugin.BatchData
	sessionClosed  bool
	sessionAborted bool
}

func (w *fakeBatchWriter) Type() string {
	if w.engineType != "" {
		return w.engineType
	}
	return "fake_batch_writer"
}

func (w *fakeBatchWriter) DisplayName() string { return "Fake Batch Writer" }

func (w *fakeBatchWriter) EngineOrigin() string { return "general" }

func (w *fakeBatchWriter) DefaultPort() int { return 0 }

func (w *fakeBatchWriter) RequiredFields() []string { return nil }

func (w *fakeBatchWriter) SensitiveFields() []string { return nil }

func (w *fakeBatchWriter) ValidateConnectionInfo(engineplugin.ConnectionInfo) error { return nil }

func (w *fakeBatchWriter) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (w *fakeBatchWriter) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (w *fakeBatchWriter) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (w *fakeBatchWriter) WriteBatch(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, batch *engineplugin.BatchData, _ engineplugin.BatchWriteOptions) error {
	w.batches = append(w.batches, batch)
	return nil
}

func (w *fakeBatchWriter) OpenTableWriteSession(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.TableWriteSessionOptions) (engineplugin.TableWriteSession, error) {
	w.sessionOptions = opts
	return &fakeTableWriteSession{writer: w}, nil
}

type fakeTableWriteSession struct {
	writer *fakeBatchWriter
}

func (s *fakeTableWriteSession) WriteBatch(_ context.Context, batch *engineplugin.BatchData) error {
	s.writer.sessionBatches = append(s.writer.sessionBatches, batch)
	return nil
}

func (s *fakeTableWriteSession) Close(context.Context) error {
	s.writer.sessionClosed = true
	return nil
}

func (s *fakeTableWriteSession) Abort(context.Context) error {
	s.writer.sessionAborted = true
	return nil
}

type fakeTableWritePreparer struct {
	fields []engineplugin.FieldInfo
}

func (p *fakeTableWritePreparer) Type() string { return "fake_table_write_preparer" }

func (p *fakeTableWritePreparer) DisplayName() string { return "Fake Table Write Preparer" }

func (p *fakeTableWritePreparer) EngineOrigin() string { return "general" }

func (p *fakeTableWritePreparer) DefaultPort() int { return 0 }

func (p *fakeTableWritePreparer) RequiredFields() []string { return nil }

func (p *fakeTableWritePreparer) SensitiveFields() []string { return nil }

func (p *fakeTableWritePreparer) ValidateConnectionInfo(engineplugin.ConnectionInfo) error {
	return nil
}

func (p *fakeTableWritePreparer) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}

func (p *fakeTableWritePreparer) Capabilities() engineplugin.EngineCapabilities {
	return engineplugin.EngineCapabilities{}
}

func (p *fakeTableWritePreparer) StoreSemantics() engineplugin.StoreSemantics {
	return engineplugin.StoreSemantics{}
}

func (p *fakeTableWritePreparer) PrepareTableWrite(_ context.Context, _ engineplugin.ConnectionInfo, _ engineplugin.CatalogPath, opts engineplugin.TableWriteOptions) error {
	p.fields = append([]engineplugin.FieldInfo(nil), opts.Fields...)
	return nil
}
