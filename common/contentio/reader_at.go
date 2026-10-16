package contentio

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// NewReaderAt binds byte-range access to one content ref. It owns only the
// per-request range streams, never the caller's complete content stream.
func NewReaderAt(ctx context.Context, reader RangeReader, ref Ref) io.ReaderAt {
	return rangeReaderAt{ctx: ctx, reader: reader, ref: ref}
}

type rangeReaderAt struct {
	ctx    context.Context
	reader RangeReader
	ref    Ref
}

func (r rangeReaderAt) ReadAt(p []byte, offset int64) (n int, err error) {
	if err = r.ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 {
		return 0, fmt.Errorf("range read offset cannot be negative")
	}
	if len(p) == 0 {
		return 0, nil
	}
	rc, err := r.reader.OpenRange(r.ctx, r.ref, offset, int64(len(p)))
	if err != nil {
		return 0, err
	}
	defer func() {
		if closeErr := rc.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	n, err = io.ReadFull(rc, p)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return n, err
}
