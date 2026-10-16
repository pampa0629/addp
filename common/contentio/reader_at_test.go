package contentio

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type readerAtRangeFixture struct {
	memoryReader
	opens, closes  int
	ref            Ref
	offset, length int64
	closeErr       error
}

func (r *readerAtRangeFixture) OpenRange(_ context.Context, ref Ref, off, length int64) (io.ReadCloser, error) {
	r.opens++
	r.ref = ref
	r.offset = off
	r.length = length
	content := r.data[ref.Path]
	if off > int64(len(content)) {
		off = int64(len(content))
	}
	end := off + length
	if end > int64(len(content)) {
		end = int64(len(content))
	}
	return readerAtRangeStream{Reader: strings.NewReader(content[off:end]), close: func() error { r.closes++; return r.closeErr }}, nil
}

type readerAtRangeStream struct {
	io.Reader
	close func() error
}

func (r readerAtRangeStream) Close() error { return r.close() }
func TestReaderAtAdapterBindsRangesAndClosesPartialReads(t *testing.T) {
	fixture := &readerAtRangeFixture{memoryReader: memoryReader{data: map[string]string{"file": "012345"}}}
	ref := NewRef("file", RoleMain)
	at := NewReaderAt(context.Background(), fixture, ref)
	p := make([]byte, 3)
	if n, err := at.ReadAt(p, 2); n != 3 || err != nil || string(p) != "234" {
		t.Fatalf("read=%d,%q,%v", n, p, err)
	}
	if fixture.ref != ref || fixture.offset != 2 || fixture.length != 3 || fixture.closes != 1 {
		t.Fatalf("range binding=%+v", fixture)
	}
	if n, err := at.ReadAt(p, 5); n != 1 || err != io.EOF || fixture.closes != 2 {
		t.Fatalf("partial=%d,%v closes=%d", n, err, fixture.closes)
	}
	failure := errors.New("close failed")
	fixture.closeErr = failure
	if _, err := at.ReadAt(p, 0); !errors.Is(err, failure) {
		t.Fatalf("close error lost: %v", err)
	}
	if _, err := at.ReadAt(p, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
	before := fixture.opens
	if n, err := at.ReadAt(nil, 0); n != 0 || err != nil || fixture.opens != before {
		t.Fatal("empty read opened a range")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewReaderAt(ctx, fixture, ref).ReadAt(p, 0); !errors.Is(err, context.Canceled) || fixture.opens != before {
		t.Fatal("cancelled read opened a range")
	}
}
