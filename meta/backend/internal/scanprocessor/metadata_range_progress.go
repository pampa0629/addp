package scanprocessor

import (
	"context"
	"io"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

// Observe explicit range I/O and native ReaderAt calls during enrichment.
// Preserve optional capabilities without inventing random access on streams.
func observeMetadataRanges(reader plugin.ContentReadableProvider) plugin.ContentReadableProvider {
	if reader == nil {
		return nil
	}
	content := &metadataContentProvider{ContentReadableProvider: reader}
	catalog, hasCatalog := reader.(plugin.EngineCatalogProvider)
	if ranges, ok := reader.(plugin.RangeReadableProvider); ok {
		observed := &metadataRangeProvider{metadataContentProvider: content, ranges: ranges}
		if hasCatalog {
			return struct {
				*metadataRangeProvider
				metadataCatalog
			}{observed, metadataCatalog{catalog}}
		}
		return observed
	}
	if hasCatalog {
		return struct {
			*metadataContentProvider
			metadataCatalog
		}{content, metadataCatalog{catalog}}
	}
	return content
}

type metadataContentProvider struct{ plugin.ContentReadableProvider }

func (p *metadataContentProvider) OpenContent(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, opts plugin.ReadOptions) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc, err := p.ContentReadableProvider.OpenContent(ctx, conn, path, opts)
	if err != nil {
		return nil, err
	}
	native, ok := rc.(io.ReaderAt)
	if !ok {
		return rc, nil
	}
	observed := &metadataReaderAtStream{ReadCloser: rc, readerAt: native, ctx: ctx}
	if seeker, ok := rc.(io.Seeker); ok {
		return struct {
			*metadataReaderAtStream
			io.Seeker
		}{observed, seeker}, nil
	}
	return observed, nil
}

type metadataReaderAtStream struct {
	io.ReadCloser
	readerAt io.ReaderAt
	ctx      context.Context
}

func (s *metadataReaderAtStream) ReadAt(p []byte, off int64) (int, error) {
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := s.readerAt.ReadAt(p, off)
	reportMetadataRangeRead(s.ctx, n, true)
	return n, err
}

type metadataRangeProvider struct {
	*metadataContentProvider
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
	reportMetadataRangeRead(r.ctx, n, !r.counted)
	if n > 0 {
		r.counted = true
	}
	return n, err
}

func reportMetadataRangeRead(ctx context.Context, n int, newRange bool) {
	if n <= 0 {
		return
	}
	scanflow.FileProgressFrom(ctx).Update(func(s *models.ScanProgress) {
		s.CurrentRangeBytesRead += int64(n)
		if newRange {
			s.CurrentRangesRead++
		}
	})
}
