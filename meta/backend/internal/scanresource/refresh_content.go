package scanresource

import (
	"context"
	"io"

	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
)

// BoundRefreshReader preserves optional range-read capability while constraining
// source IO to the frozen maximum. It does not invent an Allow or a new Provider.
func BoundRefreshReader(ctx context.Context, resource *commonmodels.Engine, base plugin.ContentReadableProvider) plugin.ContentReadableProvider {
	if base == nil || (MetadataRefreshScope(ctx) == nil && !hasUploadedContentTargets(ctx)) {
		return base
	}
	reader := &refreshContentReader{ContentReadableProvider: base, scope: ctx, resource: resource}
	if ranges, ok := base.(plugin.RangeReadableProvider); ok {
		return &refreshRangeReader{refreshContentReader: reader, ranges: ranges}
	}
	return reader
}

type refreshContentReader struct {
	plugin.ContentReadableProvider
	scope    context.Context
	resource *commonmodels.Engine
}

func (r *refreshContentReader) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if err := r.scope.Err(); err != nil {
		return nil, err
	}
	if err := CheckRefreshContent(r.scope, path); err != nil {
		return nil, err
	}
	if err := CheckCollectionRead(r.scope, r.resource); err != nil {
		return nil, err
	}
	return r.ContentReadableProvider.OpenContent(ctx, conn, path, opts)
}

type refreshRangeReader struct {
	*refreshContentReader
	ranges plugin.RangeReadableProvider
}

func (r *refreshRangeReader) OpenRange(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if err := r.scope.Err(); err != nil {
		return nil, err
	}
	if err := CheckRefreshContent(r.scope, path); err != nil {
		return nil, err
	}
	if err := CheckCollectionRead(r.scope, r.resource); err != nil {
		return nil, err
	}
	return r.ranges.OpenRange(ctx, conn, path, opts)
}
