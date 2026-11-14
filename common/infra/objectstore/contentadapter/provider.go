package contentadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/addp/common/contentio"
	p "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/infra/objectstore"
)

// Provider adapts an already-bound logical area to the shared content pipeline.
// It is injected into its consumers and is never registered as a business engine.
type Provider struct {
	Name  string
	Store objectstore.ContentArea
}

func (s *Provider) Type() string                                  { return "infra-object" }
func (s *Provider) DisplayName() string                           { return "Platform objects" }
func (s *Provider) EngineOrigin() string                          { return "extension" }
func (s *Provider) DefaultPort() int                              { return 0 }
func (s *Provider) RequiredFields() []string                      { return nil }
func (s *Provider) SensitiveFields() []string                     { return nil }
func (s *Provider) ValidateConnectionInfo(p.ConnectionInfo) error { return nil }
func (s *Provider) TestConnection(ctx context.Context, _ p.ConnectionInfo) error {
	_, err := s.Store.List(ctx, "")
	return err
}
func (s *Provider) Capabilities() p.EngineCapabilities { return p.NewObjectCapabilities(s.Type()) }
func (s *Provider) StoreSemantics() p.StoreSemantics {
	c := s.Capabilities()
	return p.StoreSemantics{Semantics: c.Storage.Semantics, NotSupported: c.Storage.NotSupported}
}
func (s *Provider) EngineCatalogModel() p.EngineCatalogModelSpec { return p.ObjectCatalogModel() }
func (s *Provider) key(value string) (string, error) {
	prefix := s.Name + "/"
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("object path is outside logical area %q", s.Name)
	}
	return strings.TrimSuffix(strings.TrimPrefix(value, prefix), "/"), nil
}
func (s *Provider) callbacks() p.ObjectCatalogCallbacks {
	return p.ObjectCatalogCallbacks{
		ListBucketsFunc: func(context.Context, p.ConnectionInfo, p.EngineCatalogPath) ([]p.EngineCatalogEntry, error) {
			return nil, fmt.Errorf("platform area root enumeration is unavailable")
		},
		ListDirectoryFunc: s.list,
		GetObjectStorageFactsFunc: func(ctx context.Context, _ p.ConnectionInfo, value string) (*p.StorageObjectFacts, error) {
			key, err := s.key(value)
			if err != nil {
				return nil, err
			}
			stat, err := s.Store.Stat(ctx, key)
			if err != nil {
				return nil, err
			}
			facts := &p.StorageObjectFacts{Name: path.Base(key), Path: value, Size: stat.Size, ContentType: stat.ContentType}
			if stat.ModifiedAt != nil {
				facts.ModifiedAt = *stat.ModifiedAt
			}
			return facts, nil
		},
	}
}
func (s *Provider) ListChildren(ctx context.Context, info p.ConnectionInfo, parent p.EngineCatalogPath, opts p.ListOptions) ([]p.EngineCatalogEntry, error) {
	return p.ListObjectCatalogChildren(ctx, s.callbacks(), info, parent.EngineID, parent, opts)
}
func (s *Provider) ResolvePath(ctx context.Context, info p.ConnectionInfo, value p.EngineCatalogPath) (*p.EngineCatalogEntry, error) {
	return p.ResolveObjectCatalogPath(ctx, s.callbacks(), info, value.EngineID, value)
}
func (s *Provider) DescribeEngineCatalogFacts(ctx context.Context, info p.ConnectionInfo, value p.EngineCatalogPath, opts p.EngineCatalogFactsOptions) (*p.EngineCatalogFacts, error) {
	return p.DescribeObjectCatalogFacts(ctx, s.callbacks(), info, value.EngineID, value)
}
func (s *Provider) list(ctx context.Context, _ p.ConnectionInfo, parent p.EngineCatalogPath) ([]p.EngineCatalogEntry, error) {
	value := parent.StringPath()
	if value == s.Name {
		value += "/"
	}
	key, err := s.key(value)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSuffix(key, "/")
	if prefix != "" {
		prefix += "/"
	}
	refs, err := s.Store.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	var result []p.EngineCatalogEntry
	dirs := map[string]bool{}
	for _, ref := range refs {
		name := strings.TrimPrefix(ref.Path, prefix)
		if dir, _, ok := strings.Cut(name, "/"); ok {
			if !dirs[dir] {
				result = append(result, p.ObjectPrefixCatalogEntry(parent, dir, s.Name+"/"+prefix+dir+"/"))
				dirs[dir] = true
			}
			continue
		}
		stat, err := s.Store.Stat(ctx, ref.Path)
		if err != nil {
			return nil, err
		}
		f := p.StorageObjectFacts{Name: name, Path: s.Name + "/" + ref.Path, Size: stat.Size, ContentType: stat.ContentType}
		if stat.ModifiedAt != nil {
			f.ModifiedAt = *stat.ModifiedAt
		}
		result = append(result, p.ObjectLeafCatalogEntry(parent, f))
	}
	return result, nil
}
func (s *Provider) OpenContent(ctx context.Context, _ p.ConnectionInfo, value p.EngineCatalogPath, opts p.ReadOptions) (io.ReadCloser, error) {
	key, err := s.key(value.StringPath())
	if err != nil {
		return nil, err
	}
	if opts.Offset != 0 || opts.Length != 0 {
		return s.Store.OpenRange(ctx, key, opts.Offset, opts.Length)
	}
	return s.Store.Open(ctx, key)
}

func (s *Provider) OpenRange(ctx context.Context, info p.ConnectionInfo, value p.EngineCatalogPath, opts p.ReadOptions) (io.ReadCloser, error) {
	if opts.Length <= 0 {
		return nil, fmt.Errorf("range read requires positive length")
	}
	return s.OpenContent(ctx, info, value, opts)
}
func (s *Provider) DeleteResource(ctx context.Context, _ p.ConnectionInfo, value p.EngineCatalogPath) error {
	key, err := s.key(value.StringPath())
	if err != nil {
		return err
	}
	return s.Store.Delete(ctx, key)
}
func (s *Provider) CreateContent(ctx context.Context, _ p.ConnectionInfo, value p.EngineCatalogPath, opts p.WriteOptions) (io.WriteCloser, error) {
	key, err := s.key(value.StringPath())
	if err != nil {
		return nil, err
	}
	if !opts.Overwrite {
		if _, err := s.Store.Stat(ctx, key); err == nil {
			return nil, fmt.Errorf("object already exists: %s", key)
		} else if !errors.Is(err, contentio.ErrContentNotFound) {
			return nil, err
		}
	}
	mediaType := opts.ContentType
	if mediaType == "" {
		mediaType = format.GuessContentType(key, nil)
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := s.Store.Put(ctx, key, reader, -1, mediaType)
		reader.CloseWithError(err)
		done <- err
	}()
	return &uploadWriter{PipeWriter: writer, done: done}, nil
}

type uploadWriter struct {
	once sync.Once
	err  error
	*io.PipeWriter
	done chan error
}

func (w *uploadWriter) Close() error {
	w.once.Do(func() { closeErr := w.PipeWriter.Close(); w.err = errors.Join(closeErr, <-w.done) })
	return w.err
}
