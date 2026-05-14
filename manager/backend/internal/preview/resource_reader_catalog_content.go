package preview

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/addp/manager/internal/catalogutil"
)

type objectCatalogResourceReader struct {
	contentReader plugin.ContentReadableProvider
	catalog       plugin.CatalogProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
	bucket        string
}

func newObjectCatalogResourceReader(contentReader plugin.ContentReadableProvider, catalog plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket string) *objectCatalogResourceReader {
	return &objectCatalogResourceReader{
		contentReader: contentReader,
		catalog:       catalog,
		connInfo:      connInfo,
		engineID:      engineID,
		bucket:        bucket,
	}
}

func (r *objectCatalogResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	return catalogutil.OpenObjectContent(ctx, r.contentReader, r.connInfo, r.engineID, r.bucket, r.objectKey(ref.Path))
}

func (r *objectCatalogResourceReader) OpenRange(ctx context.Context, ref resource.ResourceRef, offset, length int64) (io.ReadCloser, error) {
	rangeReader, ok := r.contentReader.(plugin.RangeReadableProvider)
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return rangeReader.OpenRange(ctx, r.connInfo, catalogutil.ObjectItemPath(r.engineID, r.bucket, r.objectKey(ref.Path)), plugin.ReadOptions{
		Offset: offset,
		Length: length,
	})
}

func (r *objectCatalogResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, nil
}

func (r *objectCatalogResourceReader) List(ctx context.Context, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	if r.catalog == nil {
		return nil, resource.ErrResourceNotFound
	}
	scopePath := r.objectKey(scope.Path)
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, catalogutil.ObjectDirectoryPath(r.engineID, r.bucket, scopePath), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]resource.ResourceRef, 0, len(nodes))
	for _, node := range nodes {
		path := strings.TrimPrefix(catalogutil.NodePhysicalPath(node), strings.Trim(r.bucket, "/")+"/")
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

func (r *objectCatalogResourceReader) objectKey(path string) string {
	path = strings.Trim(path, "/")
	bucket := strings.Trim(r.bucket, "/")
	if bucket != "" && strings.HasPrefix(path, bucket+"/") {
		return strings.TrimPrefix(path, bucket+"/")
	}
	return path
}

type fileCatalogResourceReader struct {
	contentReader plugin.ContentReadableProvider
	catalog       plugin.CatalogProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
}

func newFileCatalogResourceReader(contentReader plugin.ContentReadableProvider, catalog plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint) *fileCatalogResourceReader {
	return &fileCatalogResourceReader{
		contentReader: contentReader,
		catalog:       catalog,
		connInfo:      connInfo,
		engineID:      engineID,
	}
}

func (r *fileCatalogResourceReader) Open(ctx context.Context, ref resource.ResourceRef) (io.ReadCloser, error) {
	return openFileCatalogContent(ctx, r.contentReader, r.connInfo, r.engineID, ref.Path)
}

func (r *fileCatalogResourceReader) OpenRange(ctx context.Context, ref resource.ResourceRef, offset, length int64) (io.ReadCloser, error) {
	rangeReader, ok := r.contentReader.(plugin.RangeReadableProvider)
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	return rangeReader.OpenRange(ctx, r.connInfo, fileCatalogPath(r.engineID, ref.Path), plugin.ReadOptions{
		Offset: offset,
		Length: length,
	})
}

func (r *fileCatalogResourceReader) Stat(context.Context, resource.ResourceRef) (*resource.ResourceMetadata, error) {
	return nil, nil
}

func (r *fileCatalogResourceReader) List(ctx context.Context, scope resource.ResourceRef) ([]resource.ResourceRef, error) {
	if r.catalog == nil {
		return nil, resource.ErrResourceNotFound
	}
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, fileCatalogDirectoryPath(r.engineID, scope.Path), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]resource.ResourceRef, 0, len(nodes))
	for _, node := range nodes {
		path := catalogutil.NodePhysicalPath(node)
		role := resource.ResourceRoleMain
		if node.IsContainer {
			role = resource.ResourceRoleScope
		}
		refs = append(refs, resource.NewResourceRef(path, role))
	}
	return refs, nil
}

func componentReaderForPreview(reader resource.ResourceReader, mainPath string, formatType format.FormatType, attrs map[string]interface{}) *resource.StaticComponentReader {
	specs := componentSpecsForPreviewFormat(formatType)
	components := componentRefsFromAttributes(mainPath, formatType, stringSliceAttribute(attrs, "component_files"))
	if len(components) == 0 {
		return resource.NewStaticComponentReader(reader, resource.SameBasenameComponents(mainPath, specs))
	}
	return resource.NewStaticComponentReader(reader, components)
}

func componentRefsFromAttributes(mainPath string, formatType format.FormatType, componentFiles []string) []resource.ComponentRef {
	if len(componentFiles) == 0 {
		return nil
	}
	mainPath = strings.Trim(mainPath, "/")
	components := make([]resource.ComponentRef, 0, len(componentFiles))
	for _, path := range componentFiles {
		path = strings.Trim(path, "/")
		if path == "" {
			continue
		}
		ext := strings.ToLower(filepath.Ext(path))
		role, known := componentRoleForPreviewFormat(formatType, ext)
		if !known {
			continue
		}
		if mainPath != "" && !strings.EqualFold(path, mainPath) {
			components = append(components, resource.ComponentRef{
				ResourceRef:   resource.NewResourceRef(path, resource.ResourceRoleComponent),
				ComponentRole: role,
				Required:      componentRequiredForPreviewFormat(formatType, ext),
			})
			continue
		}
		components = append(components, resource.ComponentRef{
			ResourceRef:   resource.NewResourceRef(path, resource.ResourceRoleMain),
			ComponentRole: role,
			Required:      true,
		})
	}
	return components
}

func componentSpecsForPreviewFormat(formatType format.FormatType) []resource.ComponentSpec {
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil
	}
	specProvider, ok := provider.(format.ComponentSpecProvider)
	if !ok {
		return nil
	}
	return specProvider.ComponentSpecs()
}

func componentRoleForPreviewFormat(formatType format.FormatType, ext string) (string, bool) {
	ext = resource.NormalizeExtension(ext)
	for _, spec := range componentSpecsForPreviewFormat(formatType) {
		if resource.NormalizeExtension(spec.Extension) == ext {
			return spec.Role, true
		}
	}
	return "", false
}

func componentRequiredForPreviewFormat(formatType format.FormatType, ext string) bool {
	ext = resource.NormalizeExtension(ext)
	for _, spec := range componentSpecsForPreviewFormat(formatType) {
		if resource.NormalizeExtension(spec.Extension) == ext {
			return spec.Required
		}
	}
	return false
}
