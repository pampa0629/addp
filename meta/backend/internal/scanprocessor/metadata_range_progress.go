package scanprocessor

import (
	"context"
	"io"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

// Observe only enrichment range I/O; preserve sequential streams and optional
// catalog capability so format readers retain their existing access behavior.
func observeMetadataRanges(reader plugin.ContentReadableProvider) plugin.ContentReadableProvider {
	ranges, ok := reader.(plugin.RangeReadableProvider)
	if !ok {
		return reader
	}
	observed := &metadataRangeProvider{ContentReadableProvider: reader, ranges: ranges}
	if catalog, ok := reader.(plugin.EngineCatalogProvider); ok {
		return struct {
			*metadataRangeProvider
			metadataCatalog
		}{observed, metadataCatalog{catalog}}
	}
	return observed
}

type metadataRangeProvider struct {
	plugin.ContentReadableProvider
	ranges plugin.RangeReadableProvider
}

func (p *metadataRangeProvider) OpenRange(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc, err := p.ranges.OpenRange(ctx, conn, path, opts)
	if err != nil {
		return nil, err
	}
	return &metadataProgressStream{ReadCloser: rc, ctx: ctx}, nil
}

type metadataCatalog struct{ catalog plugin.EngineCatalogProvider }

func (p metadataCatalog) ListChildren(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	return p.catalog.ListChildren(ctx, conn, path, opts)
}

func (p metadataCatalog) ResolvePath(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath) (*plugin.EngineCatalogEntry, error) {
	return p.catalog.ResolvePath(ctx, conn, path)
}

type metadataProgressStream struct {
	io.ReadCloser
	ctx     context.Context
	counted bool
}

func (r *metadataProgressStream) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.ReadCloser.Read(buffer)
	if n > 0 {
		scanflow.FileProgressFrom(r.ctx).Update(func(s *models.ScanProgress) {
			s.CurrentRangeBytesRead += int64(n)
			if !r.counted {
				s.CurrentRangesRead++
			}
		})
		r.counted = true
	}
	return n, err
}
