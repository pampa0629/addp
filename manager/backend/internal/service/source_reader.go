package service

import (
	"context"
	"io"
)

// sourceContextReader keeps request/execution cancellation on local copies too.
type sourceContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *sourceContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
