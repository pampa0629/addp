package metaenrich

import (
	"context"
	"io"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaitem"
)

type tableFileContentReader struct {
	contentReader  plugin.ContentReadableProvider
	connInfo       plugin.ConnectionInfo
	engineID       uint
	catalogPathFor func(path string) plugin.EngineCatalogPath
	files          []metaitem.StorageFileRef
	subdirs        []metaitem.StorageDirectoryRef
}

func (r tableFileContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	if r.contentReader == nil {
		return nil, contentio.ErrContentNotFound
	}
	return r.contentReader.OpenContent(ctx, r.connInfo, resolveTableFileCatalogPath(r.engineID, ref.Path, r.catalogPathFor), plugin.ReadOptions{})
}

func (r tableFileContentReader) Stat(_ context.Context, ref contentio.Ref) (*contentio.Stat, error) {
	for _, file := range r.files {
		if strings.Trim(file.Path, "/") == strings.Trim(ref.Path, "/") {
			return &contentio.Stat{
				Ref:    contentio.NewRef(file.Path, contentio.RoleMain),
				Size:   file.Size,
				Exists: true,
			}, nil
		}
	}
	for _, dir := range r.subdirs {
		if strings.Trim(dir.Path, "/") == strings.Trim(ref.Path, "/") {
			return &contentio.Stat{
				Ref:    contentio.NewRef(dir.Path, contentio.RoleScope),
				Exists: true,
			}, nil
		}
	}
	return &contentio.Stat{Ref: ref, Exists: false}, nil
}

func (r tableFileContentReader) List(_ context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	scopePath := strings.Trim(scope.Path, "/")
	refs := make([]contentio.Ref, 0)
	for _, file := range r.files {
		path := strings.Trim(file.Path, "/")
		if !isImmediateChildPath(scopePath, path) {
			continue
		}
		refs = append(refs, contentio.NewRef(path, contentio.RoleMain))
	}
	for _, dir := range r.subdirs {
		path := strings.Trim(dir.Path, "/")
		if !isImmediateChildPath(scopePath, path) {
			continue
		}
		refs = append(refs, contentio.NewRef(path, contentio.RoleScope))
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	if len(refs) == 0 {
		return nil, contentio.ErrContentNotFound
	}
	return refs, nil
}

func isImmediateChildPath(scopePath string, childPath string) bool {
	if scopePath == "" {
		return childPath != "" && !strings.Contains(childPath, "/")
	}
	if !strings.HasPrefix(childPath, scopePath+"/") {
		return false
	}
	rest := strings.TrimPrefix(childPath, scopePath+"/")
	return rest != "" && !strings.Contains(rest, "/")
}
