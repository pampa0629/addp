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

type engineContentReader struct {
	provider    engineplugin.ContentReadableProvider
	rangeReader engineplugin.RangeReadableProvider
	connInfo    engineplugin.ConnectionInfo
	basePath    engineplugin.CatalogPath
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

func (r *engineContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r == nil || r.provider == nil {
		return nil, fmt.Errorf("engine content reader requires content readable provider")
	}
	path, err := CatalogPath(r.basePath, ref)
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
	path, err := CatalogPath(r.basePath, ref)
	if err != nil {
		return nil, err
	}
	opts := r.readOptions
	opts.Offset = offset
	opts.Length = length
	return r.rangeReader.OpenRange(ctx, r.connInfo, path, opts)
}

type engineContentWriter struct {
	provider     engineplugin.ContentWritableProvider
	connInfo     engineplugin.ConnectionInfo
	basePath     engineplugin.CatalogPath
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

func (w *engineContentWriter) Create(ctx context.Context, ref contentio.Ref) (io.WriteCloser, error) {
	if w == nil || w.provider == nil {
		return nil, fmt.Errorf("engine content writer requires content writable provider")
	}
	path, err := CatalogPath(w.basePath, ref)
	if err != nil {
		return nil, err
	}
	return w.provider.CreateContent(ctx, w.connInfo, path, w.writeOptions)
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
