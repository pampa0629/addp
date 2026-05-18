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
	formatHint  string
	dataType    string
}

func NewSingleContentReader(ref contentio.Ref, open func(context.Context) (io.ReadCloser, error), metadata *contentio.Metadata) contentio.Reader {
	reader := &singleContentReader{ref: ref, open: open}
	if metadata != nil {
		reader.size = metadata.Size
		reader.contentType = metadata.ContentType
		reader.formatHint = metadata.FormatHint
		reader.dataType = metadata.DataTypeHint
	}
	return reader
}

func (r *singleContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r == nil || r.open == nil || ref.Path != r.ref.Path {
		return nil, contentio.ErrContentNotFound
	}
	return r.open(ctx)
}

func (r *singleContentReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Metadata, error) {
	if r == nil || ref.Path != r.ref.Path {
		return nil, contentio.ErrContentNotFound
	}
	return &contentio.Metadata{
		Ref:          r.ref,
		Size:         r.size,
		ContentType:  r.contentType,
		Exists:       true,
		FormatHint:   r.formatHint,
		DataTypeHint: r.dataType,
	}, nil
}

func (r *singleContentReader) List(context.Context, contentio.Ref) ([]contentio.Ref, error) {
	if r == nil {
		return nil, contentio.ErrContentNotFound
	}
	return []contentio.Ref{r.ref}, nil
}
