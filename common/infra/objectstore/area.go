// Package objectstore provides object I/O within a deployment-bound logical area.
package objectstore

import (
	"context"
	"io"

	"github.com/addp/common/contentio"
)

const Standard = "standard"

// Area operations take keys relative to the bound area.
type Area interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, mediaType string) error
	Stat(ctx context.Context, key string) (*contentio.Stat, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// ContentArea supports the content and directory operations needed by formats.
type ContentArea interface {
	Area
	OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]contentio.Ref, error)
}
