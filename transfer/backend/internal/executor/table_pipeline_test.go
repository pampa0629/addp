package executor

import (
	"context"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
	jsonformat "github.com/addp/common/format/plugins/json"
)

func TestEncodedTableTransferExecutorConvertsCSVToJSONL(t *testing.T) {
	reader := &fakeContentReader{content: "id,name\n1,Alice\n2,Bob\n3,Carol\n"}
	writer := &fakeContentWriter{}
	exec := &EncodedTableTransferExecutor{
		Reader:            reader,
		Writer:            writer,
		TableReadProvider: csvformat.NewPlugin(nil),
		FormatProvider:    jsonformat.NewPlugin(nil),
	}

	metrics, err := exec.Execute(context.Background(), EncodedTableTransferPlan{
		SourceFormat: format.FormatCSV,
		TargetFormat: format.FormatJSON,
		BatchSize:    2,
		WriteOptions: &format.WriteOptions{
			ExtraParams: map[string]interface{}{"json_mode": "jsonl"},
		},
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

func TestNewEncodedTableTransferExecutorFromRegistry(t *testing.T) {
	source := &fakeContentReader{engineType: "registry_encoded_source"}
	target := &fakeContentWriter{engineType: "registry_encoded_target"}
	engineplugin.Register(source)
	engineplugin.Register(target)
	t.Cleanup(func() {
		engineplugin.Unregister(source.Type())
		engineplugin.Unregister(target.Type())
	})

	exec, err := NewEncodedTableTransferExecutor(source.Type(), target.Type(), format.FormatCSV, format.FormatJSON)
	if err != nil {
		t.Fatalf("NewEncodedTableTransferExecutor failed: %v", err)
	}
	if exec.Reader != source {
		t.Fatalf("reader = %#v, want registered source", exec.Reader)
	}
	if exec.Writer != target {
		t.Fatalf("writer = %#v, want registered target", exec.Writer)
	}
	if exec.TableReadProvider.Format() != format.FormatCSV {
		t.Fatalf("table read provider = %q, want csv", exec.TableReadProvider.Format())
	}
	if exec.FormatProvider.Format() != format.FormatJSON {
		t.Fatalf("table writer provider = %q, want json", exec.FormatProvider.Format())
	}
}
