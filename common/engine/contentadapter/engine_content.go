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

// RefCatalogPathMapper maps a contentio ref to an engine catalog path.
// It is the explicit boundary between bottom-level content I/O refs and
// engine-specific catalog addressing.
type RefCatalogPathMapper func(ref contentio.Ref) (engineplugin.CatalogPath, error)

type engineContentReader struct {
	provider    engineplugin.ContentReadableProvider
	catalog     engineplugin.CatalogProvider
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
	var catalog engineplugin.CatalogProvider
	if typed, ok := provider.(engineplugin.CatalogProvider); ok {
		catalog = typed
	}
	return &engineContentReader{
		provider:    provider,
		catalog:     catalog,
		rangeReader: rangeReader,
		connInfo:    connInfo,
		basePath:    basePath,
		readOptions: readOptions,
	}
}

// NewMappedReader creates a content reader whose catalog path is fully decided
// by mapRef. Use it when a content ref is not derived by replacing basePath's
// basename, for example a fixed single-content endpoint or object bucket/path.
func NewMappedReader(provider engineplugin.ContentReadableProvider, connInfo engineplugin.ConnectionInfo, mapRef RefCatalogPathMapper, readOptions engineplugin.ReadOptions) contentio.Reader {
	var rangeReader engineplugin.RangeReadableProvider
	if typed, ok := provider.(engineplugin.RangeReadableProvider); ok {
		rangeReader = typed
	}
	var catalog engineplugin.CatalogProvider
	if typed, ok := provider.(engineplugin.CatalogProvider); ok {
		catalog = typed
	}
	return &engineContentReader{
		provider:    provider,
		catalog:     catalog,
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

func (r *engineContentReader) Stat(ctx context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	if r == nil || r.catalog == nil {
		return nil, contentio.ErrContentNotFound
	}
	path, err := r.catalogPath(ref)
	if err != nil {
		return nil, err
	}
	node, err := r.catalog.ResolvePath(ctx, r.connInfo, path)
	if err != nil {
		return nil, err
	}
	if node == nil || node.Role != engineplugin.CatalogRoleLeaf {
		return nil, contentio.ErrContentNotFound
	}
	stat := &contentio.Stat{
		Ref:         ref,
		Size:        catalogEntrySizeBytes(*node),
		ContentType: catalogEntryContentType(*node),
		Exists:      true,
	}
	if node.UpdatedAt != nil {
		modifiedAt := *node.UpdatedAt
		stat.ModifiedAt = &modifiedAt
	}
	return stat, nil
}

func (r *engineContentReader) List(ctx context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	if r == nil || r.catalog == nil {
		return nil, contentio.ErrContentNotFound
	}
	path, err := r.catalogPath(scope)
	if err != nil {
		return nil, err
	}
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, path, engineplugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]contentio.Ref, 0, len(nodes))
	for _, node := range nodes {
		refPath := node.Path.StringPath()
		if refPath == "" {
			continue
		}
		role := contentio.RoleMain
		if node.Role == engineplugin.CatalogRoleBranch {
			role = contentio.RoleScope
		}
		refs = append(refs, contentio.NewRef(refPath, role))
	}
	return refs, nil
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

// NewMappedWriter is the writer counterpart of NewMappedReader.
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

func catalogEntryContentType(node engineplugin.CatalogEntry) string {
	if node.Storage == nil {
		return ""
	}
	return node.Storage.ContentType
}

func catalogEntrySizeBytes(node engineplugin.CatalogEntry) int64 {
	if node.Storage == nil || node.Storage.SizeBytes == nil {
		return 0
	}
	return *node.Storage.SizeBytes
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

// FixedPathMapper maps every content ref to the same catalog path. It is for
// single-content endpoints where the endpoint path is already exact.
func FixedPathMapper(path engineplugin.CatalogPath) RefCatalogPathMapper {
	return func(contentio.Ref) (engineplugin.CatalogPath, error) {
		return cloneCatalogPath(path), nil
	}
}

// ScopePathMapper maps whole-scope refs back to engine catalog paths.
// Unlike CatalogPath, it preserves nested paths under the scope instead of
// replacing only the basename, which is required for partitioned datasets.
func ScopePathMapper(base engineplugin.CatalogPath) RefCatalogPathMapper {
	return func(ref contentio.Ref) (engineplugin.CatalogPath, error) {
		path := strings.Trim(ref.Path, "/")
		if isObjectCatalogPath(base) {
			bucket, objectPath, ok := strings.Cut(path, "/")
			if !ok {
				bucket = path
			}
			if bucket == "" {
				bucket, objectPath = objectBaseParts(base)
			}
			if ref.Role == contentio.RoleScope {
				return engineplugin.ObjectDirectoryPath(base.EngineID, bucket, objectPath), nil
			}
			return engineplugin.ObjectItemPath(base.EngineID, bucket, objectPath), nil
		}
		if ref.Role == contentio.RoleScope {
			return engineplugin.FileDirectoryPath(base.EngineID, path), nil
		}
		return engineplugin.FileItemPath(base.EngineID, path), nil
	}
}

func isObjectCatalogPath(path engineplugin.CatalogPath) bool {
	for _, segment := range path.Segments {
		if segment.Term == engineplugin.CatalogTermBucket || segment.Kind == engineplugin.CatalogKindBucket ||
			segment.Term == engineplugin.CatalogTermObject || segment.Kind == engineplugin.CatalogKindObject ||
			segment.Term == engineplugin.CatalogTermPrefix || segment.Kind == engineplugin.CatalogKindPrefix {
			return true
		}
	}
	return false
}

func objectBaseParts(path engineplugin.CatalogPath) (string, string) {
	bucket := ""
	parts := make([]string, 0, len(path.Segments))
	for _, segment := range path.Segments {
		name := strings.Trim(segment.Name, "/")
		if name == "" {
			continue
		}
		if bucket == "" && (segment.Term == engineplugin.CatalogTermBucket || segment.Kind == engineplugin.CatalogKindBucket) {
			bucket = name
			continue
		}
		if bucket != "" {
			parts = append(parts, name)
		}
	}
	return bucket, strings.Join(parts, "/")
}

func cloneCatalogPath(path engineplugin.CatalogPath) engineplugin.CatalogPath {
	return engineplugin.CatalogPath{
		Version:  path.Version,
		EngineID: path.EngineID,
		Segments: append([]engineplugin.CatalogSegment(nil), path.Segments...),
	}
}
