package shapefile

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
)

func TestDescribeRefsUsesRefFormatFacts(t *testing.T) {
	descriptors := DescribeRefs([]contentio.Ref{
		{
			Path:     "roads.shp",
			Name:     "roads.shp",
			Role:     contentio.RoleMain,
			Required: true,
			Primary:  true,
		},
		{
			Path:     "roads.dbf",
			Name:     "roads.dbf",
			Role:     "attributes",
			Required: true,
		},
		{
			Path: "roads.prj",
			Name: "roads.prj",
			Role: "projection",
		},
	})

	byRole := map[string]format.RefDescriptor{}
	for _, descriptor := range descriptors {
		byRole[descriptor.Role] = descriptor
	}
	if got := byRole["main"].Format; got != format.FormatUnknown {
		t.Fatalf("main ref format = %s, want unknown ref file format", got)
	}
	if got := byRole["attributes"].Format; got != format.FormatUnknown {
		t.Fatalf("attributes ref format = %s, want unknown ref file format", got)
	}
	if got := byRole["projection"].Format; got != format.FormatText {
		t.Fatalf("projection format = %s, want text", got)
	}
}

func TestOpenMultiTableWriterWritesReadableShapefile(t *testing.T) {
	plugin := NewPlugin(nil)
	target := contentio.NewRef("exports/cities.shp", contentio.RoleMain)
	output := newMemoryMultiWriter(contentio.SameBasenameRefs(target.Path, RelatedRefSpecs()))
	schema := &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "id", Type: format.FieldTypeInt},
			{Name: "name", Type: format.FieldTypeString, Size: 32},
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geom",
			GeometryType:   "Point",
			SRID:           4326,
		},
	}

	writer, err := plugin.OpenMultiTableWriter(context.Background(), output, target, schema, nil)
	if err != nil {
		t.Fatalf("OpenMultiTableWriter failed: %v", err)
	}
	err = writer.WriteRows(context.Background(), []map[string]interface{}{
		{"id": 1, "name": "Alpha", "geom": "POINT (120 30)"},
		{"id": 2, "name": "Beta", "geom": "POINT (121 31)"},
	})
	if err != nil {
		t.Fatalf("WriteRows failed: %v", err)
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	for _, path := range []string{"exports/cities.shp", "exports/cities.shx", "exports/cities.dbf", "exports/cities.cpg"} {
		if len(output.files[path]) == 0 {
			t.Fatalf("ref %s was not written", path)
		}
	}
	if !output.committed {
		t.Fatal("ref writer was not committed")
	}

	rows, err := plugin.SampleMultiTable(context.Background(), output, 0, 10, nil)
	if err != nil {
		t.Fatalf("SampleMultiTable failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("sample row count = %d, want 2", len(rows))
	}
	if got := rows[0]["NAME"]; got != "Alpha" {
		t.Fatalf("first NAME = %#v, want Alpha", got)
	}
	if got := rows[1]["ID"]; got != int64(2) {
		t.Fatalf("second ID = %#v, want int64(2)", got)
	}
}

type memoryMultiWriter struct {
	refs      []contentio.Ref
	files     map[string][]byte
	committed bool
	aborted   bool
}

func newMemoryMultiWriter(refs []contentio.Ref) *memoryMultiWriter {
	return &memoryMultiWriter{
		refs:  append([]contentio.Ref(nil), refs...),
		files: map[string][]byte{},
	}
}

func (w *memoryMultiWriter) Refs() []contentio.Ref {
	return append([]contentio.Ref(nil), w.refs...)
}

func (w *memoryMultiWriter) Create(ctx context.Context, ref contentio.Ref) (io.WriteCloser, error) {
	return &memoryWriteCloser{onClose: func(data []byte) {
		w.files[ref.Path] = append([]byte(nil), data...)
	}}, nil
}

func (w *memoryMultiWriter) Commit(ctx context.Context) error {
	w.committed = true
	return nil
}

func (w *memoryMultiWriter) Abort(ctx context.Context) error {
	w.aborted = true
	return nil
}

func (w *memoryMultiWriter) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	data, ok := w.files[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (w *memoryMultiWriter) OpenRole(ctx context.Context, role string) (io.ReadCloser, error) {
	for _, ref := range w.refs {
		if ref.Role == role {
			return w.Open(ctx, ref)
		}
	}
	return nil, contentio.ErrContentNotFound
}

type memoryWriteCloser struct {
	bytes.Buffer
	onClose func([]byte)
}

func (w *memoryWriteCloser) Close() error {
	if w.onClose != nil {
		w.onClose(w.Bytes())
	}
	return nil
}
