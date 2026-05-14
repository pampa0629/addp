package format

import (
	"context"
	"io"

	"github.com/addp/common/resource"
)

type singleResourceReader struct {
	ref         resource.ResourceRef
	open        func(context.Context) (io.ReadCloser, error)
	size        int64
	contentType string
	formatHint  string
	dataType    string
}

func NewSingleResourceReader(ref resource.ResourceRef, open func(context.Context) (io.ReadCloser, error), metadata *resource.ResourceMetadata) resource.ResourceReader {
	reader := &singleResourceReader{ref: ref, open: open}
	if metadata != nil {
		reader.size = metadata.Size
		reader.contentType = metadata.ContentType
		reader.formatHint = metadata.FormatHint
		reader.dataType = metadata.DataTypeHint
	}
	return reader
}

func (r *singleResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	if r == nil || r.open == nil || ref.Path != r.ref.Path {
		return nil, resource.ErrResourceNotFound
	}
	return r.open(ctx)
}

func (r *singleResourceReader) Stat(_ context.Context, ref resource.ResourceRef) (*resource.ResourceMetadata, error) {
	if r == nil || ref.Path != r.ref.Path {
		return nil, resource.ErrResourceNotFound
	}
	return &resource.ResourceMetadata{
		Ref:          r.ref,
		Size:         r.size,
		ContentType:  r.contentType,
		Exists:       true,
		FormatHint:   r.formatHint,
		DataTypeHint: r.dataType,
	}, nil
}

func (r *singleResourceReader) List(context.Context, resource.ResourceRef) ([]resource.ResourceRef, error) {
	if r == nil {
		return nil, resource.ErrResourceNotFound
	}
	return []resource.ResourceRef{r.ref}, nil
}
