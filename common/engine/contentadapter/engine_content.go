package contentadapter

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	engineplugin "github.com/addp/common/engine/plugin"
)

type RefCatalogPathMapper func(ref contentio.Ref) (engineplugin.CatalogPath, error)

type engineContentReader struct {
	provider    engineplugin.ContentReadableProvider
	rangeReader engineplugin.RangeReadableProvider
	connInfo    engineplugin.ConnectionInfo
	basePath    engineplugin.CatalogPath
	mapRef      RefCatalogPathMapper
	readOptions engineplugin.ReadOptions
}

func NewReader(provider engineplugin.ContentReadableProvider, connInfo engineplugin.ConnectionInfo, basePath engineplugin.CatalogPath, readOptions engineplugin.ReadOptions) contentio.Reader {
	var rangeReader engineplugin.RangeReadableProvider
	if typed, ok := provider.(engineplugin.RangeReadableProvider); ok {
		rangeReader = typed
	}
	return &engineContentReader{
		provider:    provider,
		rangeReader: rangeReader,
		connInfo:    connInfo,
		basePath:    basePath,
		readOptions: readOptions,
	}
}

func NewMappedReader(provider engineplugin.ContentReadableProvider, connInfo engineplugin.ConnectionInfo, mapRef RefCatalogPathMapper, readOptions engineplugin.ReadOptions) contentio.Reader {
	var rangeReader engineplugin.RangeReadableProvider
	if typed, ok := provider.(engineplugin.RangeReadableProvider); ok {
		rangeReader = typed
	}
	return &engineContentReader{
		provider:    provider,
		rangeReader: rangeReader,
		connInfo:    connInfo,
		mapRef:      mapRef,
		readOptions: readOptions,
	}
}

func (r *engineContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r == nil || r.provider == nil {
		return nil, fmt.Errorf("engine content reader requires content readable provider")
	}
	path, err := r.catalogPath(ref)
	if err != nil {
		return nil, err
	}
	return r.provider.OpenContent(ctx, r.connInfo, path, r.readOptions)
}

func (r *engineContentReader) Stat(context.Context, contentio.Ref) (*contentio.Metadata, error) {
	return nil, contentio.ErrContentNotFound
}

func (r *engineContentReader) List(context.Context, contentio.Ref) ([]contentio.Ref, error) {
	return nil, contentio.ErrContentNotFound
}

func (r *engineContentReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	if r == nil || r.rangeReader == nil {
		return nil, contentio.ErrContentNotFound
	}
	path, err := r.catalogPath(ref)
	if err != nil {
		return nil, err
	}
	opts := r.readOptions
	opts.Offset = offset
	opts.Length = length
	return r.rangeReader.OpenRange(ctx, r.connInfo, path, opts)
}

func (r *engineContentReader) catalogPath(ref contentio.Ref) (engineplugin.CatalogPath, error) {
	if r != nil && r.mapRef != nil {
		return r.mapRef(ref)
	}
	return CatalogPath(r.basePath, ref)
}

type engineContentWriter struct {
	provider     engineplugin.ContentWritableProvider
	connInfo     engineplugin.ConnectionInfo
	basePath     engineplugin.CatalogPath
	mapRef       RefCatalogPathMapper
	writeOptions engineplugin.WriteOptions
}

func NewWriter(provider engineplugin.ContentWritableProvider, connInfo engineplugin.ConnectionInfo, basePath engineplugin.CatalogPath, writeOptions engineplugin.WriteOptions) contentio.Writer {
	return &engineContentWriter{
		provider:     provider,
		connInfo:     connInfo,
		basePath:     basePath,
		writeOptions: writeOptions,
	}
}

func NewMappedWriter(provider engineplugin.ContentWritableProvider, connInfo engineplugin.ConnectionInfo, mapRef RefCatalogPathMapper, writeOptions engineplugin.WriteOptions) contentio.Writer {
	return &engineContentWriter{
		provider:     provider,
		connInfo:     connInfo,
		mapRef:       mapRef,
		writeOptions: writeOptions,
	}
}

func (w *engineContentWriter) Create(ctx context.Context, ref contentio.Ref) (io.WriteCloser, error) {
	if w == nil || w.provider == nil {
		return nil, fmt.Errorf("engine content writer requires content writable provider")
	}
	path, err := w.catalogPath(ref)
	if err != nil {
		return nil, err
	}
	return w.provider.CreateContent(ctx, w.connInfo, path, w.writeOptions)
}

func (w *engineContentWriter) catalogPath(ref contentio.Ref) (engineplugin.CatalogPath, error) {
	if w != nil && w.mapRef != nil {
		return w.mapRef(ref)
	}
	return CatalogPath(w.basePath, ref)
}

func CatalogPath(base engineplugin.CatalogPath, ref contentio.Ref) (engineplugin.CatalogPath, error) {
	if len(base.Segments) == 0 {
		return engineplugin.CatalogPath{}, fmt.Errorf("content base path requires at least one segment")
	}
	name := filepath.Base(ref.Path)
	if name == "." || name == "/" || name == "" {
		return engineplugin.CatalogPath{}, fmt.Errorf("content path %q has no file name", ref.Path)
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

func ObjectPathMapper(engineID uint) RefCatalogPathMapper {
	return func(ref contentio.Ref) (engineplugin.CatalogPath, error) {
		path := strings.Trim(ref.Path, "/")
		if path == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("content ref path is empty")
		}
		bucket, objectPath, ok := strings.Cut(path, "/")
		if !ok || strings.TrimSpace(bucket) == "" || strings.TrimSpace(objectPath) == "" {
			return engineplugin.CatalogPath{}, fmt.Errorf("object content ref %q must be bucket/object", ref.Path)
		}
		return engineplugin.ObjectItemPath(engineID, bucket, objectPath), nil
	}
}
