package executor

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resource"
)

type engineResourceReader struct {
	provider    engineplugin.ContentReadableProvider
	rangeReader engineplugin.RangeReadableProvider
	connInfo    engineplugin.ConnectionInfo
	basePath    engineplugin.CatalogPath
	readOptions engineplugin.ReadOptions
}

func newEngineResourceReader(provider engineplugin.ContentReadableProvider, connInfo engineplugin.ConnectionInfo, basePath engineplugin.CatalogPath, readOptions engineplugin.ReadOptions) resource.ResourceReader {
	var rangeReader engineplugin.RangeReadableProvider
	if typed, ok := provider.(engineplugin.RangeReadableProvider); ok {
		rangeReader = typed
	}
	return &engineResourceReader{
		provider:    provider,
		rangeReader: rangeReader,
		connInfo:    connInfo,
		basePath:    basePath,
		readOptions: readOptions,
	}
}

func (r *engineResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	if r == nil || r.provider == nil {
		return nil, fmt.Errorf("engine resource reader requires content readable provider")
	}
	path, err := resourceCatalogPath(r.basePath, ref)
	if err != nil {
		return nil, err
	}
	return r.provider.OpenContent(ctx, r.connInfo, path, r.readOptions)
}

func (r *engineResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, resource.ErrResourceNotFound
}

func (r *engineResourceReader) List(context.Context, resource.ResourceRef) ([]resource.ResourceRef, error) {
	return nil, resource.ErrResourceNotFound
}

func (r *engineResourceReader) OpenRange(ctx context.Context, ref resource.ResourceRef, offset, length int64) (io.ReadCloser, error) {
	if r == nil || r.rangeReader == nil {
		return nil, resource.ErrResourceNotFound
	}
	path, err := resourceCatalogPath(r.basePath, ref)
	if err != nil {
		return nil, err
	}
	opts := r.readOptions
	opts.Offset = offset
	opts.Length = length
	return r.rangeReader.OpenRange(ctx, r.connInfo, path, opts)
}

type engineResourceWriter struct {
	provider     engineplugin.ContentWritableProvider
	connInfo     engineplugin.ConnectionInfo
	basePath     engineplugin.CatalogPath
	writeOptions engineplugin.WriteOptions
}

func newEngineResourceWriter(provider engineplugin.ContentWritableProvider, connInfo engineplugin.ConnectionInfo, basePath engineplugin.CatalogPath, writeOptions engineplugin.WriteOptions) *engineResourceWriter {
	return &engineResourceWriter{
		provider:     provider,
		connInfo:     connInfo,
		basePath:     basePath,
		writeOptions: writeOptions,
	}
}

func (w *engineResourceWriter) Create(ctx context.Context, ref resource.ResourceRef) (io.WriteCloser, error) {
	if w == nil || w.provider == nil {
		return nil, fmt.Errorf("engine resource writer requires content writable provider")
	}
	path, err := resourceCatalogPath(w.basePath, ref)
	if err != nil {
		return nil, err
	}
	return w.provider.CreateContent(ctx, w.connInfo, path, w.writeOptions)
}

func resourceCatalogPath(base engineplugin.CatalogPath, ref resource.ResourceRef) (engineplugin.CatalogPath, error) {
	if len(base.Segments) == 0 {
		return engineplugin.CatalogPath{}, fmt.Errorf("resource base path requires at least one segment")
	}
	name := filepath.Base(ref.Path)
	if name == "." || name == "/" || name == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("resource path %q has no file name", ref.Path)
	}
	next := engineplugin.CatalogPath{
		Version:  base.Version,
		EngineID: base.EngineID,
		Segments: append([]engineplugin.CatalogSegment(nil), base.Segments...),
	}
	if next.Version == "" {
		next.Version = engineplugin.CatalogPathVersion
	}
	last := &next.Segments[len(next.Segments)-1]
	last.Name = name
	if strings.TrimSpace(last.Term) == "" {
		last.Term = engineplugin.CatalogTermFile
	}
	if strings.TrimSpace(last.Kind) == "" {
		last.Kind = engineplugin.CatalogKindFile
	}
	return next, nil
}
