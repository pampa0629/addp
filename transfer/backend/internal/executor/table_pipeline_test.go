package executor

import (
	"context"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	csvformat "github.com/addp/common/format/plugins/csv"
	jsonformat "github.com/addp/common/format/plugins/json"
	shapefileformat "github.com/addp/common/format/plugins/shapefile"
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

func TestEncodedTableTransferExecutorReadsShapefileComponents(t *testing.T) {
	source := &fakeContentWriter{files: map[string][]byte{}}
	shapefilePlugin := shapefileformat.NewPlugin(nil)
	target := engineplugin.FileItemPath(7, "imports/cities.shp")
	componentWriter := newContentComponentWriter(source, nil, target, engineplugin.WriteOptions{Overwrite: true}, shapefilePlugin.ComponentSpecs())
	tableWriter, err := shapefilePlugin.OpenComponentTableWriter(context.Background(), componentWriter, resourceRefFromCatalogPath(target), &format.TableInfo{
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
		t.Fatalf("OpenComponentTableWriter failed: %v", err)
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
	exec := &EncodedTableTransferExecutor{
		Reader:            source,
		Writer:            output,
		ComponentProvider: shapefilePlugin,
		FormatProvider:    csvformat.NewPlugin(nil),
	}
	metrics, err := exec.Execute(context.Background(), EncodedTableTransferPlan{
		SourcePath:   target,
		SourceFormat: format.FormatShapefile,
		TargetFormat: format.FormatCSV,
		BatchSize:    1,
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
