package resource

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

type memoryResourceReader struct {
	data map[string]string
}

func (r memoryResourceReader) Open(_ context.Context, ref ResourceRef) (io.ReadCloser, error) {
	value, ok := r.data[ref.Path]
	if !ok {
		return nil, ErrComponentNotFound
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (r memoryResourceReader) Stat(_ context.Context, ref ResourceRef) (*ResourceMetadata, error) {
	_, ok := r.data[ref.Path]
	return &ResourceMetadata{Ref: ref, Exists: ok}, nil
}

func (r memoryResourceReader) List(context.Context, ResourceRef) ([]ResourceRef, error) {
	refs := make([]ResourceRef, 0, len(r.data))
	for path := range r.data {
		refs = append(refs, NewResourceRef(path, ResourceRoleMain))
	}
	return refs, nil
}

type memoryRangeResourceReader struct {
	memoryResourceReader
	rangeRef    ResourceRef
	rangeOffset int64
	rangeLength int64
}

func (r *memoryRangeResourceReader) OpenRange(_ context.Context, ref ResourceRef, offset, length int64) (io.ReadCloser, error) {
	r.rangeRef = ref
	r.rangeOffset = offset
	r.rangeLength = length
	value, ok := r.data[ref.Path]
	if !ok {
		return nil, ErrComponentNotFound
	}
	end := offset + length
	if offset < 0 || length < 0 || offset > int64(len(value)) || end > int64(len(value)) {
		return nil, ErrResourceNotFound
	}
	return io.NopCloser(strings.NewReader(value[offset:end])), nil
}

func TestSameBasenameComponents(t *testing.T) {
	got := SameBasenameComponents("datasets/roads/roads.shp", []ComponentSpec{
		{Extension: ".shp", Role: "main", Required: true},
		{Extension: "dbf", Required: true},
	})
	want := []ComponentRef{
		{ResourceRef: NewResourceRef("datasets/roads/roads.shp", ResourceRoleComponent), ComponentRole: "main", Required: true},
		{ResourceRef: NewResourceRef("datasets/roads/roads.dbf", ResourceRoleComponent), ComponentRole: "dbf", Required: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SameBasenameComponents() = %#v, want %#v", got, want)
	}
}

func TestFirstResourceByExtension(t *testing.T) {
	ref, err := FirstResourceByExtension(context.Background(), memoryResourceReader{data: map[string]string{
		"lake/_SUCCESS":         "",
		"lake/part-000.parquet": "data",
	}}, NewResourceRef("lake", ResourceRoleScope), ".parquet")
	if err != nil {
		t.Fatalf("FirstResourceByExtension() error = %v", err)
	}
	if ref.Path != "lake/part-000.parquet" {
		t.Fatalf("ref.Path = %q, want lake/part-000.parquet", ref.Path)
	}
}

func TestStaticComponentReaderOpensRole(t *testing.T) {
	reader := NewStaticComponentReader(memoryResourceReader{data: map[string]string{
		"roads.shp": "shape",
	}}, []ComponentRef{
		{ResourceRef: NewResourceRef("roads.shp", ResourceRoleComponent), ComponentRole: "main", Required: true},
	})

	rc, err := reader.OpenComponentRole(context.Background(), "main")
	if err != nil {
		t.Fatalf("OpenComponentRole() error = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "shape" {
		t.Fatalf("component data = %q, want shape", data)
	}
}

func TestStaticComponentReaderOpenComponentRangeDelegatesToRangeReader(t *testing.T) {
	backing := &memoryRangeResourceReader{
		memoryResourceReader: memoryResourceReader{data: map[string]string{
			"roads.shx": "0123456789",
		}},
	}
	reader := NewStaticComponentReader(backing, []ComponentRef{
		{ResourceRef: NewResourceRef("roads.shx", ResourceRoleComponent), ComponentRole: "index", Required: true},
	})
	component := reader.Components()[0]

	rc, err := reader.OpenComponentRange(context.Background(), component, 2, 4)
	if err != nil {
		t.Fatalf("OpenComponentRange() error = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "2345" {
		t.Fatalf("range data = %q, want 2345", data)
	}
	if backing.rangeRef.Path != "roads.shx" || backing.rangeOffset != 2 || backing.rangeLength != 4 {
		t.Fatalf("range call = (%q,%d,%d), want (roads.shx,2,4)", backing.rangeRef.Path, backing.rangeOffset, backing.rangeLength)
	}
}

func TestStaticComponentReaderOpenComponentRangeFallsBackToStream(t *testing.T) {
	reader := NewStaticComponentReader(memoryResourceReader{data: map[string]string{
		"roads.shx": "0123456789",
	}}, []ComponentRef{
		{ResourceRef: NewResourceRef("roads.shx", ResourceRoleComponent), ComponentRole: "index", Required: true},
	})
	component := reader.Components()[0]

	rc, err := reader.OpenComponentRange(context.Background(), component, 2, 4)
	if err != nil {
		t.Fatalf("OpenComponentRange() error = %v", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "2345" {
		t.Fatalf("range data = %q, want 2345", data)
	}
}

type memoryResourceWriter struct {
	created []ResourceRef
}

func (w *memoryResourceWriter) Create(_ context.Context, ref ResourceRef) (io.WriteCloser, error) {
	w.created = append(w.created, ref)
	return nopWriteCloser{Writer: io.Discard}, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (w nopWriteCloser) Close() error {
	return nil
}

func TestStaticComponentWriterCreatesComponentResource(t *testing.T) {
	backing := &memoryResourceWriter{}
	writer := NewStaticComponentWriter(backing, []ComponentRef{
		{ResourceRef: NewResourceRef("roads.dbf", ResourceRoleComponent), ComponentRole: "attributes", Required: true},
	})
	components := writer.Components()
	if len(components) != 1 {
		t.Fatalf("component count = %d, want 1", len(components))
	}
	if _, err := writer.CreateComponent(context.Background(), components[0]); err != nil {
		t.Fatalf("CreateComponent() error = %v", err)
	}
	if got, want := backing.created[0].Path, "roads.dbf"; got != want {
		t.Fatalf("created path = %q, want %q", got, want)
	}
	components[0].Path = "mutated.dbf"
	if got, want := writer.Components()[0].Path, "roads.dbf"; got != want {
		t.Fatalf("component slice was mutated: got %q, want %q", got, want)
	}
}
