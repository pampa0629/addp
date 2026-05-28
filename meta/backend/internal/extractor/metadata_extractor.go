package extractor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	pathpkg "path"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataExtractor 元数据提取器，负责对象元数据的提取、缓存和查询。
type MetadataExtractor struct {
	db  *gorm.DB
	log *slog.Logger
}

// NewMetadataExtractor 创建元数据提取器实例。
func NewMetadataExtractor(db *gorm.DB) *MetadataExtractor {
	return &MetadataExtractor{
		db:  db,
		log: logger.With("component", "metadata_extractor"),
	}
}

// GetObjectMetadata 获取指定对象的元数据。
// 用于 Manager 模块预览时查询已扫描的元数据。
func (e *MetadataExtractor) GetObjectMetadata(
	tenantID, engineID uint,
	objectKey string,
) (*models.MetaItem, error) {
	bucket, relativePath := metapath.SplitObjectPath(objectKey)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object key: %s", objectKey)
	}

	var bucketNode models.MetaNode
	err := e.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
		tenantID, engineID, "bucket", bucket).First(&bucketNode).Error
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	objectName := pathpkg.Base(relativePath)
	if objectName == "" {
		objectName = relativePath
	}

	var item models.MetaItem

	if relativePath != "" && relativePath != objectName {
		prefixPath := pathpkg.Dir(relativePath)
		segments := strings.Split(prefixPath, "/")

		currentParent := &bucketNode
		for _, segment := range segments {
			if segment == "" || segment == "." {
				continue
			}

			var prefixNode models.MetaNode
			err := e.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id = ? AND node_type = ? AND name = ?",
				tenantID, engineID, currentParent.ID, "prefix", segment).First(&prefixNode).Error
			if err != nil {
				return nil, fmt.Errorf("prefix not found: %s", segment)
			}
			currentParent = &prefixNode
		}

		err = e.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, currentParent.ID, "object", objectName).First(&item).Error
	} else {
		err = e.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, bucketNode.ID, "object", objectName).First(&item).Error
	}

	if err != nil {
		return nil, fmt.Errorf("object metadata not found: %w", err)
	}

	return &item, nil
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据。
// 当 Manager 预览时发现元数据未提取，可以调用此方法触发实时提取。
func (e *MetadataExtractor) ExtractObjectMetadataOnDemand(
	tenantID, engineID uint,
	objectKey string,
	token string,
	objectReader io.Reader,
) (map[string]interface{}, error) {
	item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		return nil, err
	}

	content, err := io.ReadAll(objectReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read object content: %w", err)
	}
	contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	bucket, objectPath := metapath.SplitObjectPath(objectKey)
	if objectPath == "" {
		objectPath = objectKey
	}

	enhancedAttrs := item.Attributes
	if enhancedAttrs == nil {
		enhancedAttrs = make(models.JSONMap)
	}
	if bucket != "" {
		metaattr.SetStorage(enhancedAttrs, "bucket", bucket)
	}
	metaattr.SetStorage(enhancedAttrs, "content_type", contentType)
	physicalPath := objectPath
	if descriptor := dataitem.DescriptorFromAttributes(enhancedAttrs); descriptor.PhysicalPath != "" {
		physicalPath = descriptor.PhysicalPath
	}
	sizeBytes := int64(len(content))
	detected := onDemandDetectedItemFromAttributes(enhancedAttrs, physicalPath, sizeBytes)

	_, _, err = metaenrich.EnrichResourceAttributes(context.Background(), enhancedAttrs, metaenrich.ResourceAttributesInput{
		ContentReader:  onDemandContentReader{content: content},
		EngineID:       engineID,
		Item:           detected,
		PhysicalPath:   physicalPath,
		SizeBytes:      sizeBytes,
		CatalogPathFor: plugin.ObjectItemPathForBucket(engineID, bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to enrich object metadata: %w", err)
	}
	enhancedAttrs = metaattr.Normalize(enhancedAttrs)

	if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
		e.log.Warn("更新元数据失败", "item_id", item.ID, "error", err)
		return nil, err
	}

	return enhancedAttrs, nil
}

func (e *MetadataExtractor) BuildObjectAccessIndexOnDemand(
	tenantID, engineID uint,
	objectKey string,
	objectReader io.Reader,
) (models.JSONMap, error) {
	item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		return nil, err
	}
	formatName := commonJSON.String(item.Attributes, "item", "format")
	if strings.TrimSpace(formatName) == "" {
		return nil, fmt.Errorf("item format is empty: %s", objectKey)
	}
	formatType := format.NormalizeFormat(formatName)
	if formatType == format.FormatUnknown {
		return nil, fmt.Errorf("item format is unknown: %s", objectKey)
	}
	if !format.SupportsAccessIndex(formatType) {
		return nil, fmt.Errorf("format %s does not support access index", formatType)
	}
	provider, err := format.GetTableInfoProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("format %s cannot build access index: %w", formatType, err)
	}

	tableInfo, err := provider.DescribeTable(context.Background(), objectReader, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build access index for %s: %w", objectKey, err)
	}
	index := tableInfo.AccessIndex
	if index == nil {
		return nil, fmt.Errorf("format %s did not return access index", formatType)
	}
	if index.Source == nil {
		index.Source = map[string]interface{}{}
	}
	if item.SizeBytes != nil {
		index.Source["size_bytes"] = *item.SizeBytes
	}
	if item.DataUpdatedAt != nil {
		index.Source["last_modified_at"] = item.DataUpdatedAt
	}
	if etag := commonJSON.String(item.Attributes, "storage", "etag"); etag != "" {
		index.Source["etag"] = etag
	}

	enhancedAttrs := cloneJSONMap(item.Attributes)
	metaattr.UpsertNested(enhancedAttrs, "access_index", "table", commonJSON.MapFromStruct(index))
	if tableInfo.Table != nil && len(tableInfo.Table.Fields) > 0 {
		metaattr.UpsertNested(enhancedAttrs, "type_info", "table", datatype.TableInfoAttributes(tableInfo.Table))
	}
	enhancedAttrs = metaattr.Normalize(enhancedAttrs)

	if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
		return nil, err
	}
	return enhancedAttrs, nil
}

func cloneJSONMap(attrs models.JSONMap) models.JSONMap {
	cloned := make(models.JSONMap, len(attrs))
	for key, value := range attrs {
		cloned[key] = value
	}
	return cloned
}

func onDemandDetectedItemFromAttributes(attrs models.JSONMap, physicalPath string, sizeBytes int64) *metaitem.DetectedItem {
	descriptor := dataitem.DescriptorFromAttributes(attrs)
	if descriptor.SizeBytes != nil {
		sizeBytes = *descriptor.SizeBytes
	}
	if physicalPath == "" {
		physicalPath = descriptor.PhysicalPath
	}
	return &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Layout:    descriptor.Layout,
			DataType:  descriptor.DataType,
			Format:    descriptor.Format,
			EntryPath: descriptor.EntryPath,
			SizeBytes: &sizeBytes,
			RefList:   descriptor.Refs,
		},
		PhysicalPath: physicalPath,
	}
}

type onDemandContentReader struct {
	content []byte
}

func (r onDemandContentReader) Type() string         { return "on-demand" }
func (r onDemandContentReader) DisplayName() string  { return "on-demand" }
func (r onDemandContentReader) EngineOrigin() string { return "general" }
func (r onDemandContentReader) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (r onDemandContentReader) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (r onDemandContentReader) DefaultPort() int                                   { return 0 }
func (r onDemandContentReader) RequiredFields() []string                           { return nil }
func (r onDemandContentReader) SensitiveFields() []string                          { return nil }
func (r onDemandContentReader) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (r onDemandContentReader) StoreSemantics() plugin.StoreSemantics { return plugin.StoreSemantics{} }
func (r onDemandContentReader) OpenContent(context.Context, plugin.ConnectionInfo, plugin.CatalogPath, plugin.ReadOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.content)), nil
}
