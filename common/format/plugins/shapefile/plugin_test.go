package shapefile

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/common/resource"
)

func TestDescribeComponentsUsesComponentFormatFacts(t *testing.T) {
	descriptors := DescribeComponents([]resource.ComponentRef{
		{
			ResourceRef:   resource.NewResourceRef("roads.shp", resource.ResourceRoleMain),
			ComponentRole: "main",
			Required:      true,
		},
		{
			ResourceRef:   resource.NewResourceRef("roads.dbf", resource.ResourceRoleComponent),
			ComponentRole: "attributes",
			Required:      true,
		},
		{
			ResourceRef:   resource.NewResourceRef("roads.prj", resource.ResourceRoleComponent),
			ComponentRole: "projection",
		},
	})

	byRole := map[string]format.ComponentDescriptor{}
	for _, descriptor := range descriptors {
		byRole[descriptor.Role] = descriptor
	}
	if got := byRole["main"].Format; got != format.FormatUnknown {
		t.Fatalf("main component format = %s, want unknown component file format", got)
	}
	if got := byRole["attributes"].Format; got != format.FormatUnknown {
		t.Fatalf("attributes component format = %s, want unknown component file format", got)
	}
	if got := byRole["projection"].Format; got != format.FormatText {
		t.Fatalf("projection format = %s, want text", got)
	}
}

func TestOpenComponentTableWriterWritesReadableShapefile(t *testing.T) {
	plugin := NewPlugin(nil)
	target := resource.NewResourceRef("exports/cities.shp", resource.ResourceRoleMain)
	output := newMemoryComponentWriter(resource.SameBasenameComponents(target.Path, ComponentSpecs()))
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

	writer, err := plugin.OpenComponentTableWriter(context.Background(), output, target, schema, nil)
	if err != nil {
		t.Fatalf("OpenComponentTableWriter failed: %v", err)
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
			t.Fatalf("component %s was not written", path)
		}
	}
	if !output.committed {
		t.Fatal("component writer was not committed")
	}

	rows, err := plugin.SampleTableComponents(context.Background(), output, 0, 10, nil)
	if err != nil {
		t.Fatalf("SampleTableComponents failed: %v", err)
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

type memoryComponentWriter struct {
	components []resource.ComponentRef
	files      map[string][]byte
	committed  bool
	aborted    bool
}

func newMemoryComponentWriter(components []resource.ComponentRef) *memoryComponentWriter {
	return &memoryComponentWriter{
		components: append([]resource.ComponentRef(nil), components...),
		files:      map[string][]byte{},
	}
}

func (w *memoryComponentWriter) Components() []resource.ComponentRef {
	return append([]resource.ComponentRef(nil), w.components...)
}

func (w *memoryComponentWriter) CreateComponent(ctx context.Context, component resource.ComponentRef) (io.WriteCloser, error) {
	return &memoryWriteCloser{onClose: func(data []byte) {
		w.files[component.Path] = append([]byte(nil), data...)
	}}, nil
}

func (w *memoryComponentWriter) CommitComponents(ctx context.Context) error {
	w.committed = true
	return nil
}

func (w *memoryComponentWriter) AbortComponents(ctx context.Context) error {
	w.aborted = true
	return nil
}

func (w *memoryComponentWriter) OpenComponent(ctx context.Context, component resource.ComponentRef) (io.ReadCloser, error) {
	data, ok := w.files[component.Path]
	if !ok {
		return nil, resource.ErrComponentNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (w *memoryComponentWriter) OpenComponentRole(ctx context.Context, role string) (io.ReadCloser, error) {
	for _, component := range w.components {
		if component.ComponentRole == role {
			return w.OpenComponent(ctx, component)
		}
	}
	return nil, resource.ErrComponentNotFound
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
