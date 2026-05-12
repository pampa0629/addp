package parquet

import (
	"context"
	"fmt"
	"io"

	"github.com/addp/common/resource"
)

func openFirstParquet(ctx context.Context, reader resource.ResourceReader, scope resource.ResourceRef) (io.ReadCloser, error) {
	ref, err := resource.FirstResourceByExtension(ctx, reader, scope, ".parquet")
	if err != nil {
		return nil, fmt.Errorf("failed to find parquet file in scope %s: %w", scope.Path, err)
	}
	input, err := reader.Open(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("failed to open parquet file %s: %w", ref.Path, err)
	}
	return input, nil
}
