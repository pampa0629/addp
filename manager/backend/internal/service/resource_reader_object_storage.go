package service

import (
	"context"
	"io"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/resource"
)

type objectStorageResourceReader struct {
	contentReader plugin.ContentReadableProvider
	catalog       plugin.CatalogProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
	bucket        string
}

func newObjectStorageResourceReader(contentReader plugin.ContentReadableProvider, catalog plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket string) *objectStorageResourceReader {
	return &objectStorageResourceReader{
		contentReader: contentReader,
		catalog:       catalog,
		connInfo:      connInfo,
		engineID:      engineID,
		bucket:        bucket,
	}
}

func (r *objectStorageResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	return openObjectStorageContent(ctx, r.contentReader, r.connInfo, r.engineID, r.bucket, ref.Path)
}

func (r *objectStorageResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, nil
}

func (r *objectStorageResourceReader) List(ctx context.Context, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	if r.catalog == nil {
		return nil, resource.ErrResourceNotFound
	}
	scopePath := strings.Trim(scope.Path, "/")
	if bucket := strings.Trim(r.bucket, "/"); bucket != "" && strings.HasPrefix(scopePath, bucket+"/") {
		scopePath = strings.TrimPrefix(scopePath, bucket+"/")
	}
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, objectStorageDirectoryCatalogPath(r.engineID, r.bucket, scopePath), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]resource.ResourceRef, 0, len(nodes))
	for _, node := range nodes {
		path := strings.TrimPrefix(catalogNodePhysicalPath(node), strings.Trim(r.bucket, "/")+"/")
		if path == "" {
			path = node.Path.StringPath()
		}
		role := resource.ResourceRoleMain
		if node.IsContainer {
			role = resource.ResourceRoleScope
		}
		refs = append(refs, resource.NewResourceRef(path, role))
	}
	return refs, nil
}

type fileSystemResourceReader struct {
	contentReader plugin.ContentReadableProvider
	catalog       plugin.CatalogProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
}

func newFileSystemResourceReader(contentReader plugin.ContentReadableProvider, catalog plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint) *fileSystemResourceReader {
	return &fileSystemResourceReader{
		contentReader: contentReader,
		catalog:       catalog,
		connInfo:      connInfo,
		engineID:      engineID,
	}
}

func (r *fileSystemResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	return openFileSystemContent(ctx, r.contentReader, r.connInfo, r.engineID, ref.Path)
}

func (r *fileSystemResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, nil
}

func (r *fileSystemResourceReader) List(ctx context.Context, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	if r.catalog == nil {
		return nil, resource.ErrResourceNotFound
	}
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, fileSystemDirectoryCatalogPath(r.engineID, scope.Path), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]resource.ResourceRef, 0, len(nodes))
	for _, node := range nodes {
		path := catalogNodePhysicalPath(node)
		role := resource.ResourceRoleMain
		if node.IsContainer {
			role = resource.ResourceRoleScope
		}
		refs = append(refs, resource.NewResourceRef(path, role))
	}
	return refs, nil
}

func shapefileComponentReader(reader resource.ResourceReader, mainPath string) *resource.StaticComponentReader {
	return resource.NewStaticComponentReader(reader, resource.SameBasenameComponents(mainPath, []resource.ComponentSpec{
		{Extension: ".shp", Role: "main", Required: true},
		{Extension: ".shx", Role: "index", Required: false},
		{Extension: ".dbf", Role: "attributes", Required: false},
		{Extension: ".prj", Role: "projection", Required: false},
		{Extension: ".cpg", Role: "encoding", Required: false},
	}))
}
