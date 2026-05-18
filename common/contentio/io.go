package contentio

import (
	"context"
	"errors"
	"io"
)

type Reader interface {
	Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
	Stat(ctx context.Context, ref Ref) (*Stat, error)
	List(ctx context.Context, scope Ref) ([]Ref, error)
}

type Writer interface {
	Create(ctx context.Context, ref Ref) (io.WriteCloser, error)
}

type RangeReader interface {
	Reader
	OpenRange(ctx context.Context, ref Ref, offset, length int64) (io.ReadCloser, error)
}

var ErrContentNotFound = errors.New("content not found")
