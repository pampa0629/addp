package format

import (
	"context"
	"io"

	"github.com/addp/common/contentio"
)

type singleContentReader struct {
	ref         contentio.Ref
	open        func(context.Context) (io.ReadCloser, error)
	size        int64
	contentType string
}

func NewSingleContentReader(ref contentio.Ref, open func(context.Context) (io.ReadCloser, error), stat *contentio.Stat) contentio.Reader {
	reader := &singleContentReader{ref: ref, open: open}
	if stat != nil {
		reader.size = stat.Size
		reader.contentType = stat.ContentType
	}
	return reader
}

func (r *singleContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r == nil || r.open == nil || ref.Path != r.ref.Path {
		return nil, contentio.ErrContentNotFound
	}
	return r.open(ctx)
}

func (r *singleContentReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	if r == nil || ref.Path != r.ref.Path {
		return nil, contentio.ErrContentNotFound
	}
	return &contentio.Stat{
		Ref:         r.ref,
		Size:        r.size,
		ContentType: r.contentType,
		Exists:      true,
	}, nil
}
