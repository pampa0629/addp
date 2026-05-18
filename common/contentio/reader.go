package contentio

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const (
	RoleMain      = "main"
	RoleScope     = "scope"
	RoleManifest  = "manifest"
	RoleAuxiliary = "auxiliary"
)

type Ref struct {
	Path     string `json:"path"`
	Name     string `json:"name,omitempty"`
	Role     string `json:"role,omitempty"`
	Required bool   `json:"required,omitempty"`
	Primary  bool   `json:"primary,omitempty"`
}

func NewRef(path string, role string) Ref {
	path = strings.Trim(path, "/")
	return Ref{
		Path:    path,
		Name:    filepath.Base(path),
		Role:    strings.ToLower(strings.TrimSpace(role)),
		Primary: strings.EqualFold(role, RoleMain),
	}
}

type Metadata struct {
	Ref          Ref        `json:"ref"`
	Size         int64      `json:"size,omitempty"`
	ContentType  string     `json:"content_type,omitempty"`
	ModifiedAt   *time.Time `json:"modified_at,omitempty"`
	Exists       bool       `json:"exists"`
	Children     int64      `json:"children,omitempty"`
	FormatHint   string     `json:"format_hint,omitempty"`
	DataTypeHint string     `json:"data_type_hint,omitempty"`
}

type Reader interface {
	Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
	Stat(ctx context.Context, ref Ref) (*Metadata, error)
	List(ctx context.Context, scope Ref) ([]Ref, error)
}

type Writer interface {
	Create(ctx context.Context, ref Ref) (io.WriteCloser, error)
}

type RangeReader interface {
	Reader
	OpenRange(ctx context.Context, ref Ref, offset, length int64) (io.ReadCloser, error)
}

func FirstByExtension(ctx context.Context, reader Reader, scope Ref, extensions ...string) (Ref, error) {
	if reader == nil {
		return Ref{}, ErrContentNotFound
	}
	refs, err := reader.List(ctx, scope)
	if err != nil {
		return Ref{}, err
	}
	allowed := normalizedExtensionSet(extensions)
	for _, ref := range refs {
		if ref.Role == RoleScope {
			continue
		}
		if len(allowed) == 0 || allowed[strings.ToLower(filepath.Ext(ref.Path))] {
			return ref, nil
		}
	}
	return Ref{}, ErrContentNotFound
}

func normalizedExtensionSet(extensions []string) map[string]bool {
	set := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		ext = NormalizeExtension(ext)
		if ext == "" {
			continue
		}
		set[ext] = true
	}
	return set
}

var ErrContentNotFound = errors.New("content not found")

type MultiReader interface {
	Refs() []Ref
	Open(ctx context.Context, ref Ref) (io.ReadCloser, error)
	OpenRole(ctx context.Context, role string) (io.ReadCloser, error)
}

type MultiRangeReader interface {
	MultiReader
	OpenRange(ctx context.Context, ref Ref, offset, length int64) (io.ReadCloser, error)
}

type MultiWriter interface {
	Refs() []Ref
	Create(ctx context.Context, ref Ref) (io.WriteCloser, error)
	Commit(ctx context.Context) error
	Abort(ctx context.Context) error
}

type StaticMultiWriter struct {
	writer Writer
	refs   []Ref
}

func NewStaticMultiWriter(writer Writer, refs []Ref) *StaticMultiWriter {
	copied := append([]Ref(nil), refs...)
	return &StaticMultiWriter{
		writer: writer,
		refs:   copied,
	}
}

func (w *StaticMultiWriter) Refs() []Ref {
	if w == nil {
		return nil
	}
	return append([]Ref(nil), w.refs...)
}

func (w *StaticMultiWriter) Create(ctx context.Context, ref Ref) (io.WriteCloser, error) {
	if w == nil || w.writer == nil {
		return nil, ErrContentNotFound
	}
	return w.writer.Create(ctx, ref)
}

func (w *StaticMultiWriter) Commit(context.Context) error {
	return nil
}

func (w *StaticMultiWriter) Abort(context.Context) error {
	return nil
}

type StaticMultiReader struct {
	reader Reader
	refs   []Ref
}

func NewStaticMultiReader(reader Reader, refs []Ref) *StaticMultiReader {
	copied := append([]Ref(nil), refs...)
	return &StaticMultiReader{
		reader: reader,
		refs:   copied,
	}
}

func (r *StaticMultiReader) Refs() []Ref {
	if r == nil {
		return nil
	}
	return append([]Ref(nil), r.refs...)
}

func (r *StaticMultiReader) Open(ctx context.Context, ref Ref) (io.ReadCloser, error) {
	if r == nil || r.reader == nil {
		return nil, ErrContentNotFound
	}
	return r.reader.Open(ctx, ref)
}

func (r *StaticMultiReader) OpenRange(ctx context.Context, ref Ref, offset, length int64) (io.ReadCloser, error) {
	if r == nil || r.reader == nil {
		return nil, ErrContentNotFound
	}
	if rangeReader, ok := r.reader.(RangeReader); ok {
		return rangeReader.OpenRange(ctx, ref, offset, length)
	}
	if offset < 0 || length < 0 {
		return nil, ErrContentNotFound
	}
	rc, err := r.Open(ctx, ref)
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

func (r *StaticMultiReader) OpenRole(ctx context.Context, role string) (io.ReadCloser, error) {
	if r == nil {
		return nil, ErrContentNotFound
	}
	role = strings.ToLower(strings.TrimSpace(role))
	for _, ref := range r.refs {
		if strings.EqualFold(ref.Role, role) {
			return r.Open(ctx, ref)
		}
	}
	return nil, ErrContentNotFound
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
