package resource

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type ResourceRole string

const (
	ResourceRoleMain      ResourceRole = "main"
	ResourceRoleComponent ResourceRole = "component"
	ResourceRoleManifest  ResourceRole = "manifest"
	ResourceRoleAuxiliary ResourceRole = "auxiliary"
	ResourceRoleScope     ResourceRole = "scope"
)

type ResourceRef struct {
	Path string       `json:"path"`
	Name string       `json:"name,omitempty"`
	Role ResourceRole `json:"role,omitempty"`
}

func NewResourceRef(path string, role ResourceRole) ResourceRef {
	path = strings.Trim(path, "/")
	return ResourceRef{
		Path: path,
		Name: filepath.Base(path),
		Role: role,
	}
}

type ResourceMetadata struct {
	Ref          ResourceRef `json:"ref"`
	Size         int64       `json:"size,omitempty"`
	ContentType  string      `json:"content_type,omitempty"`
	ModifiedAt   *time.Time  `json:"modified_at,omitempty"`
	Exists       bool        `json:"exists"`
	Children     int64       `json:"children,omitempty"`
	FormatHint   string      `json:"format_hint,omitempty"`
	DataTypeHint string      `json:"data_type_hint,omitempty"`
}

type ResourceReader interface {
	Open(ctx context.Context, ref ResourceRef) (io.ReadCloser, error)
	Stat(ctx context.Context, ref ResourceRef) (*ResourceMetadata, error)
	List(ctx context.Context, scope ResourceRef) ([]ResourceRef, error)
}

type ResourceWriter interface {
	Create(ctx context.Context, ref ResourceRef) (io.WriteCloser, error)
}

type RangeReader interface {
	ResourceReader
	OpenRange(ctx context.Context, ref ResourceRef, offset, length int64) (io.ReadCloser, error)
}

func FirstResourceByExtension(ctx context.Context, reader ResourceReader, scope ResourceRef, extensions ...string) (ResourceRef, error) {
	if reader == nil {
		return ResourceRef{}, ErrResourceNotFound
	}
	refs, err := reader.List(ctx, scope)
	if err != nil {
		return ResourceRef{}, err
	}
	allowed := normalizedExtensionSet(extensions)
	for _, ref := range refs {
		if ref.Role == ResourceRoleScope {
			continue
		}
		if len(allowed) == 0 || allowed[strings.ToLower(filepath.Ext(ref.Path))] {
			return ref, nil
		}
	}
	return ResourceRef{}, ErrResourceNotFound
}

func normalizedExtensionSet(extensions []string) map[string]bool {
	set := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		ext = strings.ToLower(strings.TrimSpace(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		set[ext] = true
	}
	return set
}

var ErrResourceNotFound = errors.New("resource not found")

type ComponentRef struct {
	ResourceRef
	ComponentRole string `json:"component_role"`
	Required      bool   `json:"required"`
}

type ComponentReader interface {
	Components() []ComponentRef
	OpenComponent(ctx context.Context, component ComponentRef) (io.ReadCloser, error)
	OpenComponentRole(ctx context.Context, role string) (io.ReadCloser, error)
}

type ComponentRangeReader interface {
	ComponentReader
	OpenComponentRange(ctx context.Context, component ComponentRef, offset, length int64) (io.ReadCloser, error)
}

type ComponentWriter interface {
	Components() []ComponentRef
	CreateComponent(ctx context.Context, component ComponentRef) (io.WriteCloser, error)
	CommitComponents(ctx context.Context) error
	AbortComponents(ctx context.Context) error
}

type StaticComponentWriter struct {
	resourceWriter ResourceWriter
	components     []ComponentRef
}

func NewStaticComponentWriter(resourceWriter ResourceWriter, components []ComponentRef) *StaticComponentWriter {
	copied := append([]ComponentRef(nil), components...)
	return &StaticComponentWriter{
		resourceWriter: resourceWriter,
		components:     copied,
	}
}

func (w *StaticComponentWriter) Components() []ComponentRef {
	if w == nil {
		return nil
	}
	return append([]ComponentRef(nil), w.components...)
}

func (w *StaticComponentWriter) CreateComponent(ctx context.Context, component ComponentRef) (io.WriteCloser, error) {
	if w == nil || w.resourceWriter == nil {
		return nil, ErrComponentNotFound
	}
	return w.resourceWriter.Create(ctx, component.ResourceRef)
}

func (w *StaticComponentWriter) CommitComponents(context.Context) error {
	return nil
}

func (w *StaticComponentWriter) AbortComponents(context.Context) error {
	return nil
}

type StaticComponentReader struct {
	resourceReader ResourceReader
	components     []ComponentRef
}

func NewStaticComponentReader(resourceReader ResourceReader, components []ComponentRef) *StaticComponentReader {
	copied := append([]ComponentRef(nil), components...)
	return &StaticComponentReader{
		resourceReader: resourceReader,
		components:     copied,
	}
}

func (r *StaticComponentReader) Components() []ComponentRef {
	if r == nil {
		return nil
	}
	return append([]ComponentRef(nil), r.components...)
}

func (r *StaticComponentReader) OpenComponent(ctx context.Context, component ComponentRef) (io.ReadCloser, error) {
	return r.resourceReader.Open(ctx, component.ResourceRef)
}

func (r *StaticComponentReader) OpenComponentRange(ctx context.Context, component ComponentRef, offset, length int64) (io.ReadCloser, error) {
	rangeReader, ok := r.resourceReader.(RangeReader)
	if ok {
		return rangeReader.OpenRange(ctx, component.ResourceRef, offset, length)
	}
	if offset < 0 || length < 0 {
		return nil, ErrResourceNotFound
	}
	rc, err := r.OpenComponent(ctx, component)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if _, err := io.CopyN(io.Discard, rc, offset); err != nil {
			rc.Close()
			return nil, err
		}
	}
	return &limitedReadCloser{
		Reader: io.LimitReader(rc, length),
		closer: rc,
	}, nil
}

func (r *StaticComponentReader) OpenComponentRole(ctx context.Context, role string) (io.ReadCloser, error) {
	if r == nil {
		return nil, ErrComponentNotFound
	}
	role = strings.ToLower(strings.TrimSpace(role))
	for _, component := range r.components {
		if strings.EqualFold(component.ComponentRole, role) {
			return r.OpenComponent(ctx, component)
		}
	}
	return nil, ErrComponentNotFound
}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *limitedReadCloser) Close() error {
	if r == nil || r.closer == nil {
		return nil
	}
	return r.closer.Close()
}
