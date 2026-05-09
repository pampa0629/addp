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
	return nil, nil
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
