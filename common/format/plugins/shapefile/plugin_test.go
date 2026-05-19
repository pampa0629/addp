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
	descriptors := DescribeRefs([]format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef("roads.shp", contentio.RoleMain), true, true),
		format.NewRelatedRef(contentio.NewRef("roads.dbf", "attributes"), true, false),
		format.NewRelatedRef(contentio.NewRef("roads.prj", "projection"), false, false),
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

func TestDescriptorDeclaresOnlyMultiTableProvider(t *testing.T) {
	descriptor := NewPlugin(nil).Descriptor()

	if !descriptor.Providers.MultiTable {
		t.Fatalf("providers = %#v, want multi_table", descriptor.Providers)
	}
	if descriptor.Providers.FormatInfo || descriptor.Providers.TableInfo || descriptor.Providers.TableSample || descriptor.Providers.Table {
		t.Fatalf("providers = %#v, shapefile must not declare single table providers", descriptor.Providers)
	}
}

func TestOpenMultiTableWriterWritesReadableShapefile(t *testing.T) {
	plugin := NewPlugin(nil)
	target := contentio.NewRef("exports/cities.shp", contentio.RoleMain)
	refs := format.SameBasenameRelatedRefs(target.Path, RelatedRefSpecs())
	output := newMemoryRefStore()
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

	writer, err := plugin.OpenMultiTableWriter(context.Background(), output, refs, schema, nil)
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
	rows, err := plugin.SampleMultiTable(context.Background(), output, refs, 0, 10, nil)
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

func TestOpenMultiTableWriterRequiresRefs(t *testing.T) {
	plugin := NewPlugin(nil)
	schema := &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geom",
			GeometryType:   "Point",
		},
	}

	if _, err := plugin.OpenMultiTableWriter(context.Background(), newMemoryRefStore(), nil, schema, nil); err == nil {
		t.Fatal("OpenMultiTableWriter succeeded without refs")
	}
}

func TestOpenMultiTableWriterRequiresPrimaryRef(t *testing.T) {
	plugin := NewPlugin(nil)
	refs := []format.RelatedRef{
		format.NewRelatedRef(contentio.NewRef("exports/cities.dbf", "attributes"), true, false),
	}
	schema := &format.TableInfo{
		Fields: []format.FieldInfo{
			{Name: "geom", Type: format.FieldTypeGeometry},
		},
		SpatialInfo: &format.SpatialInfo{
			GeometryColumn: "geom",
			GeometryType:   "Point",
		},
	}

	if _, err := plugin.OpenMultiTableWriter(context.Background(), newMemoryRefStore(), refs, schema, nil); err == nil {
		t.Fatal("OpenMultiTableWriter succeeded without primary ref")
	}
}

type memoryRefStore struct {
	files map[string][]byte
}

func newMemoryRefStore() *memoryRefStore {
	return &memoryRefStore{
		files: map[string][]byte{},
	}
}

func (w *memoryRefStore) Create(ctx context.Context, ref contentio.Ref) (io.WriteCloser, error) {
	return &memoryWriteCloser{onClose: func(data []byte) {
		w.files[ref.Path] = append([]byte(nil), data...)
	}}, nil
}

func (w *memoryRefStore) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	data, ok := w.files[ref.Path]
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (w *memoryRefStore) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
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
