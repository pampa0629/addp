package contentio

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
)

type memoryReader struct {
	data map[string]string
}

func (r memoryReader) Open(_ context.Context, ref Ref) (io.ReadCloser, error) {
	value, ok := r.data[ref.Path]
	if !ok {
		return nil, ErrContentNotFound
	}
	return io.NopCloser(strings.NewReader(value)), nil
}

func (r memoryReader) Stat(_ context.Context, ref Ref) (*Stat, error) {
	_, ok := r.data[ref.Path]
	return &Stat{Ref: ref, Exists: ok}, nil
}

func (r memoryReader) List(context.Context, Ref) ([]Ref, error) {
	refs := make([]Ref, 0, len(r.data))
	for path := range r.data {
		refs = append(refs, NewRef(path, RoleMain))
	}
	return refs, nil
}

type memoryRangeReader struct {
	memoryReader
	rangeRef    Ref
	rangeOffset int64
	rangeLength int64
}

func (r *memoryRangeReader) OpenRange(_ context.Context, ref Ref, offset, length int64) (io.ReadCloser, error) {
	r.rangeRef = ref
	r.rangeOffset = offset
	r.rangeLength = length
	value, ok := r.data[ref.Path]
	if !ok {
		return nil, ErrContentNotFound
	}
	end := offset + length
	if offset < 0 || length < 0 || offset > int64(len(value)) || end > int64(len(value)) {
		return nil, ErrContentNotFound
	}
	return io.NopCloser(strings.NewReader(value[offset:end])), nil
}

func TestSameBasenameRefs(t *testing.T) {
	got := SameBasenameRefs("datasets/roads/roads.shp", []RelatedRefSpec{
		{Extension: ".shp", Role: RoleMain, Required: true, Primary: true},
		{Extension: "dbf", Required: true},
	})
	want := []Ref{
		{Path: "datasets/roads/roads.shp", Name: "roads.shp", Role: RoleMain, Required: true, Primary: true},
		{Path: "datasets/roads/roads.dbf", Name: "roads.dbf", Role: "dbf", Required: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SameBasenameRefs() = %#v, want %#v", got, want)
	}
}

func TestRangeReaderOpenRange(t *testing.T) {
	backing := &memoryRangeReader{
		memoryReader: memoryReader{data: map[string]string{
			"roads.shx": "0123456789",
		}},
	}
	ref := Ref{Path: "roads.shx", Name: "roads.shx", Role: "index", Required: true}

	rc, err := backing.OpenRange(context.Background(), ref, 2, 4)
	if err != nil {
		t.Fatalf("OpenRange() error = %v", err)
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

type memoryWriter struct {
	created []Ref
}

func (w *memoryWriter) Create(_ context.Context, ref Ref) (io.WriteCloser, error) {
	w.created = append(w.created, ref)
	return nopWriteCloser{Writer: io.Discard}, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (w nopWriteCloser) Close() error {
	return nil
}

func TestWriterCreatesRef(t *testing.T) {
	backing := &memoryWriter{}
	ref := Ref{Path: "roads.dbf", Name: "roads.dbf", Role: "attributes", Required: true}
	if _, err := backing.Create(context.Background(), ref); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got, want := backing.created[0].Path, "roads.dbf"; got != want {
		t.Fatalf("created path = %q, want %q", got, want)
	}
}
