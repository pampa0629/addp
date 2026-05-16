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

func TestTableImportExecutorExecuteWritesBatches(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n3,Carol\n"}
	writer := &fakeBatchWriter{}
	preparer := &fakeTableWritePreparer{}
	exec := &TableImportExecutor{
		Reader:         reader,
		Preparer:       preparer,
		InfoProvider:   csvformat.NewPlugin(nil),
		Writer:         writer,
		FormatProvider: csvformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), TableImportPlan{
		BatchSize: 2,
		Format:    format.FormatCSV,
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
	if len(reader.opens) != 2 || reader.opens[0] != 0 || reader.opens[1] != 0 {
		t.Fatalf("source open offsets = %#v, want two full reads", reader.opens)
	}
	if len(preparer.modes) != 0 {
		t.Fatalf("prepare modes = %#v, want no prepare for append", preparer.modes)
	}
}

func TestTableImportExecutorPreparesTargetOnce(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n"}
	preparer := &fakeTableWritePreparer{}
	exec := &TableImportExecutor{
		Reader:         reader,
		Preparer:       preparer,
		InfoProvider:   csvformat.NewPlugin(nil),
		Writer:         &fakeBatchWriter{},
		FormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableImportPlan{
		BatchSize:     1,
		Format:        format.FormatCSV,
		TargetPrepare: engineplugin.TableWriteOptions{Mode: "truncate_insert"},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := strings.Join(preparer.modes, ","); got != "truncate_insert" {
		t.Fatalf("prepare modes = %q, want truncate_insert once", got)
	}
}

func TestTableImportExecutorPrepareCreateIfNotExistsUsesTableInfo(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n"}
	preparer := &fakeTableWritePreparer{}
	exec := &TableImportExecutor{
		Reader:         reader,
		Preparer:       preparer,
		InfoProvider:   csvformat.NewPlugin(nil),
		Writer:         &fakeBatchWriter{},
		FormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableImportPlan{
		BatchSize:     2,
		Format:        format.FormatCSV,
		TargetPrepare: engineplugin.TableWriteOptions{Mode: "create_if_not_exists"},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got := strings.Join(preparer.modes, ","); got != "create_if_not_exists" {
		t.Fatalf("prepare modes = %q, want create_if_not_exists once", got)
	}
	if len(preparer.fields) != 2 || preparer.fields[0].Name != "id" || preparer.fields[0].Type != "int" {
		t.Fatalf("prepare fields = %#v, want CSV inferred fields", preparer.fields)
	}
	if len(reader.opens) != 3 {
		t.Fatalf("source open count = %d, want one describe open, one data sample open, and one EOF probe open", len(reader.opens))
	}
}

func TestTableImportExecutorRequiresPreparerForPrepareMode(t *testing.T) {
	exec := &TableImportExecutor{
		Reader:         &fakeContentReader{},
		Writer:         &fakeBatchWriter{},
		FormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableImportPlan{
		Format:        format.FormatCSV,
		TargetPrepare: engineplugin.TableWriteOptions{Mode: "truncate_insert"},
	})
	if err == nil {
		t.Fatal("Execute succeeded, want missing preparer error")
	}
	if !strings.Contains(err.Error(), "table write prepare") {
		t.Fatalf("error = %q, want table write prepare", err)
	}
}

func TestTableImportExecutorRejectsMismatchedFormat(t *testing.T) {
	exec := &TableImportExecutor{
		Reader:         &fakeContentReader{},
		Writer:         &fakeBatchWriter{},
		FormatProvider: csvformat.NewPlugin(nil),
	}

	_, err := exec.Execute(context.Background(), TableImportPlan{Format: format.FormatTSV})
	if err == nil {
		t.Fatal("Execute succeeded, want format mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match table sample provider format") {
		t.Fatalf("error = %q, want format mismatch", err)
	}
}

func TestNewTableImportExecutorFromRegistry(t *testing.T) {
	source := &fakeContentReader{engineType: "registry_import_source"}
	target := &fakeBatchWriter{engineType: "registry_import_target"}
	engineplugin.Register(source)
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(source.Type())
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewTableImportExecutor(source.Type(), target.Type(), format.FormatCSV)
	if err != nil {
		t.Fatalf("NewTableImportExecutor failed: %v", err)
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
	engineType string
	batches    []*engineplugin.BatchData
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

type fakeTableWritePreparer struct {
	modes  []string
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
	p.modes = append(p.modes, opts.Mode)
	p.fields = append([]engineplugin.FieldInfo(nil), opts.Fields...)
	return nil
}
