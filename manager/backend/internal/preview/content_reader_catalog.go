package preview

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/manager/internal/catalogutil"
)

type objectCatalogContentReader struct {
	contentReader plugin.ContentReadableProvider
	catalog       plugin.CatalogProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
	bucket        string
}

func newObjectCatalogContentReader(contentReader plugin.ContentReadableProvider, catalog plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket string) *objectCatalogContentReader {
	return &objectCatalogContentReader{
		contentReader: contentReader,
		catalog:       catalog,
		connInfo:      connInfo,
		engineID:      engineID,
		bucket:        bucket,
	}
}

func (r *objectCatalogContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return catalogutil.OpenObjectContent(ctx, r.contentReader, r.connInfo, r.engineID, r.bucket, r.objectKey(ref.Path))
}

func (r *objectCatalogContentReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	rangeReader, ok := r.contentReader.(plugin.RangeReadableProvider)
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return rangeReader.OpenRange(ctx, r.connInfo, plugin.ObjectItemPath(r.engineID, r.bucket, r.objectKey(ref.Path)), plugin.ReadOptions{
		Offset: offset,
		Length: length,
	})
}

func (r *objectCatalogContentReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r *objectCatalogContentReader) List(ctx context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	if r.catalog == nil {
		return nil, contentio.ErrContentNotFound
	}
	scopePath := r.objectKey(scope.Path)
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, plugin.ObjectDirectoryPath(r.engineID, r.bucket, scopePath), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]contentio.Ref, 0, len(nodes))
	for _, node := range nodes {
		path := strings.TrimPrefix(catalogutil.NodePhysicalPath(node), strings.Trim(r.bucket, "/")+"/")
		if path == "" {
			path = node.Path.StringPath()
		}
		role := contentio.RoleMain
		if node.IsContainer {
			role = contentio.RoleScope
		}
		refs = append(refs, contentio.NewRef(path, role))
	}
	return refs, nil
}

func (r *objectCatalogContentReader) objectKey(path string) string {
	path = strings.Trim(path, "/")
	bucket := strings.Trim(r.bucket, "/")
	if bucket != "" && strings.HasPrefix(path, bucket+"/") {
		return strings.TrimPrefix(path, bucket+"/")
	}
	return path
}

type fileCatalogContentReader struct {
	contentReader plugin.ContentReadableProvider
	catalog       plugin.CatalogProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
}

func newFileCatalogContentReader(contentReader plugin.ContentReadableProvider, catalog plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint) *fileCatalogContentReader {
	return &fileCatalogContentReader{
		contentReader: contentReader,
		catalog:       catalog,
		connInfo:      connInfo,
		engineID:      engineID,
	}
}

func (r *fileCatalogContentReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return openFileCatalogContent(ctx, r.contentReader, r.connInfo, r.engineID, ref.Path)
}

func (r *fileCatalogContentReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	rangeReader, ok := r.contentReader.(plugin.RangeReadableProvider)
	if !ok {
		return nil, contentio.ErrContentNotFound
	}
	return rangeReader.OpenRange(ctx, r.connInfo, plugin.FileItemPath(r.engineID, ref.Path), plugin.ReadOptions{
		Offset: offset,
		Length: length,
	})
}

func (r *fileCatalogContentReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r *fileCatalogContentReader) List(ctx context.Context, scope contentio.Ref) ([]contentio.Ref, error) {
	if r.catalog == nil {
		return nil, contentio.ErrContentNotFound
	}
	nodes, err := r.catalog.ListChildren(ctx, r.connInfo, plugin.FileDirectoryPath(r.engineID, scope.Path), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	refs := make([]contentio.Ref, 0, len(nodes))
	for _, node := range nodes {
		path := catalogutil.NodePhysicalPath(node)
		role := contentio.RoleMain
		if node.IsContainer {
			role = contentio.RoleScope
		}
		refs = append(refs, contentio.NewRef(path, role))
	}
	return refs, nil
}

func refsForPreview(mainPath string, formatType format.FormatType, attrs map[string]interface{}) []format.RelatedRef {
	specs := refSpecsForPreviewFormat(formatType)
	refs := refRefsFromMetaAttributes(attrs)
	if len(refs) == 0 || format.ValidateRelatedRefs(refs) != nil {
		return format.SameBasenameRelatedRefs(mainPath, specs)
	}
	return refs
}

func refRefsFromMetaAttributes(attrs map[string]interface{}) []format.RelatedRef {
	itemAttrs := commonJSON.Section(attrs, "item")
	if len(itemAttrs) == 0 {
		return nil
	}
	values := commonJSON.InterfaceSlice(itemAttrs["refs"])
	if len(values) == 0 {
		return nil
	}
	refs := make([]format.RelatedRef, 0, len(values))
	for _, value := range values {
		item := commonJSON.InterfaceMap(value)
		if len(item) == 0 {
			continue
		}
		path := strings.Trim(strings.TrimSpace(commonJSON.InterfaceString(item["path"])), "/")
		if path == "" {
			continue
		}
		role := strings.TrimSpace(commonJSON.InterfaceString(item["role"]))
		extension := format.NormalizeExtension(commonJSON.InterfaceString(item["extension"]))
		if extension == "" {
			extension = format.NormalizeExtension(filepath.Ext(path))
		}
		if role == "" {
			role = strings.TrimPrefix(extension, ".")
		}
		ref := contentio.NewRef(path, role)
		required := false
		if _, ok := item["required"]; ok {
			required = commonJSON.InterfaceBool(item["required"])
		}
		primary := false
		if _, ok := item["primary"]; ok {
			primary = commonJSON.InterfaceBool(item["primary"])
		}
		refs = append(refs, format.NewRelatedRef(ref, required, primary))
	}
	return refs
}

func refSpecsForPreviewFormat(formatType format.FormatType) []format.RelatedRefSpec {
	if provider, err := format.GetMultiTableInfoProvider(formatType); err == nil {
		return provider.RelatedRefSpecs()
	}
	if reader, err := format.GetMultiTableSampleReader(formatType); err == nil {
		return reader.RelatedRefSpecs()
	}
	if reader, err := format.GetMultiTableReaderProvider(formatType); err == nil {
		return reader.RelatedRefSpecs()
	}
	if plugin, err := format.GetFormatPlugin(formatType); err == nil {
		if specProvider, ok := plugin.(format.RelatedRefSpecProvider); ok {
			return specProvider.RelatedRefSpecs()
		}
	}
	return nil
}

func refRoleForPreviewFormat(formatType format.FormatType, ext string) (string, bool) {
	ext = format.NormalizeExtension(ext)
	for _, spec := range refSpecsForPreviewFormat(formatType) {
		if format.NormalizeExtension(spec.Extension) == ext {
			return spec.Role, true
		}
	}
	return "", false
}

func refRequiredForPreviewFormat(formatType format.FormatType, ext string) bool {
	ext = format.NormalizeExtension(ext)
	for _, spec := range refSpecsForPreviewFormat(formatType) {
		if format.NormalizeExtension(spec.Extension) == ext {
			return spec.Required
		}
	}
	return false
}
