package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/resourceutil"
	"gorm.io/gorm"
)

const (
	maxTextPreviewBytes = 256 * 1024
)

var reservedObjectSegments = map[string]struct{}{
	"__bucket__": {},
	".minio.sys": {},
}

func mergeJSONMaps(maps ...models.JSONMap) models.JSONMap {
	var merged models.JSONMap
	for _, src := range maps {
		if len(src) == 0 {
			continue
		}
		if merged == nil {
			merged = make(models.JSONMap, len(src))
		}
		for k, v := range src {
			merged[k] = v
		}
	}
	return merged
}

func metaItemLiteAttributes(item *models.MetaItemLite) map[string]interface{} {
	if item == nil || len(item.Attributes) == 0 {
		return nil
	}
	return item.Attributes
}

type objectCatalogPreviewProvider struct {
	metadataRepo   *repository.MetadataRepository
	cadPreviewRepo *repository.CADPreviewRepository
	metaClient     *commonClient.MetaClient
	content        *objectcontent.ObjectContentRegistry
}

func (p *objectCatalogPreviewProvider) SetCADPreviewRepository(repo *repository.CADPreviewRepository) {
	p.cadPreviewRepo = repo
}

func NewObjectCatalogPreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, content *objectcontent.ObjectContentRegistry) PreviewProvider {
	return &objectCatalogPreviewProvider{
		metadataRepo: metadataRepo,
		metaClient:   metaClient,
		content:      content,
	}
}

func (p *objectCatalogPreviewProvider) Name() string {
	return "builtin:object-catalog"
}

// isMetaNotFoundError 检查错误是否为 Meta API 的 404 错误
func isMetaNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "404") ||
		strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "record not found")
}

// resolveBucket 解析最终的 bucket 名称
// 优先级: connection_info 中的 bucket > schema 参数
func resolveBucket(connBucket, schemaParam string) (string, error) {
	bucket := connBucket
	if bucket == "" {
		bucket = schemaParam
	}
	if bucket == "" {
		return "", fmt.Errorf("bucket name is required")
	}
	return bucket, nil
}

func (p *objectCatalogPreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
	resource := req.Engine
	bucket := req.Schema
	path := req.Table

	objectPath := strings.Trim(path, "/")
	displayPath := objectPath
	if displayPath == "" {
		displayPath = bucket
	}

	// 如果 path 以 bucket 名称开头，去掉 bucket 前缀
	// 前端可能传递 full_name（如 "addp/json/中国.geoJson"），需要转换为 bucket 内的相对路径
	// 例如: "addp/json/中国.geoJson" → "json/中国.geoJson"
	if strings.HasPrefix(objectPath, bucket+"/") {
		objectPath = strings.TrimPrefix(objectPath, bucket+"/")
	}

	metaClient := p.metaClient
	if metaClient != nil && req.TenantID != nil {
		metaClient = metaClient.WithTenantID(*req.TenantID)
	}

	var item *models.MetaItemLite
	var node *models.MetaNodeLite

	if objectPath != "" {
		if fetchedItem, err := p.metadataRepo.GetObjectMetadataItem(resource.ID, bucket, objectPath, metaClient); err == nil {
			item = fetchedItem
		} else if !errors.Is(err, gorm.ErrRecordNotFound) && !isMetaNotFoundError(err) {
			// 只有在非"记录不存在"错误时才返回错误
			return nil, err
		}

		if fetchedNode, err := p.metadataRepo.GetObjectMetadataNode(resource.ID, bucket, objectPath, metaClient); err == nil {
			node = fetchedNode
		} else if !errors.Is(err, gorm.ErrRecordNotFound) && !isMetaNotFoundError(err) {
			// 只有在非"记录不存在"错误时才返回错误
			return nil, err
		}
	} else {
		if fetchedNode, err := p.metadataRepo.GetObjectMetadataNode(resource.ID, bucket, objectPath, metaClient); err == nil {
			node = fetchedNode
		} else if !errors.Is(err, gorm.ErrRecordNotFound) && !isMetaNotFoundError(err) {
			// 只有在非"记录不存在"错误时才返回错误
			return nil, err
		}
	}

	connInfo, err := p.connectionInfo(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection info: %w", err)
	}

	pl, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	catalogProvider, _ := pl.(plugin.EngineCatalogProvider)
	factsProvider, _ := pl.(plugin.EngineCatalogFactsProvider)
	contentReader, _ := pl.(plugin.ContentReadableProvider)
	if catalogProvider == nil {
		return nil, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	nodeType := "object"
	if item == nil {
		if node != nil {
			nodeType = strings.ToLower(node.NodeType)
		} else if req.NodeType != "" {
			nodeType = strings.ToLower(req.NodeType)
		} else {
			nodeType = "directory"
		}
	}

	mode := PreviewModeObject
	preview := &models.TablePreview{
		Mode:     mode,
		Page:     1,
		PageSize: 1,
		Columns:  []string{},
		Rows:     []map[string]interface{}{},
		Object: &models.ObjectPreview{
			Bucket:   bucket,
			Path:     displayPath,
			NodeType: nodeType,
			EngineID: resource.ID,
		},
		GeometryColumns: []string{},
	}

	attributeSources := make([]models.JSONMap, 0, 2)
	if len(req.Attributes) > 0 {
		attributeSources = append(attributeSources, models.JSONMap(req.Attributes))
	}
	if item != nil && len(item.Attributes) > 0 {
		attributeSources = append(attributeSources, item.Attributes)
	}
	combinedAttributes := mergeJSONMaps(attributeSources...)
	if len(combinedAttributes) > 0 {
		preview.Object.Attributes = combinedAttributes
	}

	if applyOSGBScenePreviewPrompt(combinedAttributes, preview.Object) {
		return preview, nil
	}

	if formatTypeFromMetaAttributes(combinedAttributes) == format.FormatRasterMosaic {
		preview.Object.StorageRef = fmt.Sprintf("%s/%s", bucket, objectPath)
		preview.Object.ContentType = "application/vnd.addp.raster-mosaic+json"
		if item != nil {
			if item.ObjectSizeBytes != nil {
				preview.Object.SizeBytes = *item.ObjectSizeBytes
			} else if item.SizeBytes != nil {
				preview.Object.SizeBytes = *item.SizeBytes
			}
		}
		preview.Object.Content = &models.ObjectPreviewContent{
			Kind: "raster_mosaic",
			Metadata: map[string]interface{}{
				"format": "raster_mosaic",
				"layout": "whole",
			},
		}
		leafRefs, _, err := rasterMosaicLeafRefsForPreview(ctx, newObjectCatalogContentReader(contentReader, catalogProvider, connInfo, resource.ID, bucket), objectPath, format.FormatRasterMosaic, combinedAttributes)
		if err != nil {
			return nil, err
		}
		if len(leafRefs) > 0 {
			preview.Object.Content.Metadata["refs"] = refPreviewDescriptors(format.FormatRasterMosaic, leafRefs)
			preview.Object.Content.Metadata["layout"] = "raster_mosaic_leaf"
		}
		return preview, nil
	}
	if formatTypeFromMetaAttributes(combinedAttributes) == format.Format3DTiles {
		manifestRef := threeDTilesManifestObjectPath(bucket, objectPath, combinedAttributes)
		manifestStorageRef := manifestRef
		if !strings.HasPrefix(manifestStorageRef, bucket+"/") {
			manifestStorageRef = strings.Trim(bucket+"/"+manifestStorageRef, "/")
		}
		manifestObjectPath := strings.TrimPrefix(manifestStorageRef, bucket+"/")
		dir, name := commonModels.SplitObjectPath(manifestObjectPath)
		preview.Object.StorageRef = manifestStorageRef
		preview.Object.ContentType = "application/vnd.ogc.3dtiles+json"
		if item != nil {
			if item.ObjectSizeBytes != nil {
				preview.Object.SizeBytes = *item.ObjectSizeBytes
			} else if item.SizeBytes != nil {
				preview.Object.SizeBytes = *item.SizeBytes
			}
		}
		contentReq := &objectcontent.ObjectContentRequest{
			Locator:     req.Locator,
			Bucket:      bucket,
			Path:        dir,
			Name:        name,
			Format:      string(format.Format3DTiles),
			Extension:   ".json",
			ContentType: preview.Object.ContentType,
			Size:        preview.Object.SizeBytes,
			Attributes:  preview.Object.Attributes,
		}
		if url := buildStorageStreamURL(resource.ID, preview.Object.StorageRef); url != "" {
			contentReq.PreviewURL = url
			preview.Object.URL = url
		}
		if p.content != nil {
			handler := p.content.Resolve(contentReq)
			if handler == nil {
				return preview, nil
			}
			content, truncated, err := handler.Handle(ctx, contentReq, nil)
			if err != nil {
				return nil, err
			}
			preview.Object.Content = content
			if truncated || (content != nil && content.Truncated) {
				preview.Object.Truncated = true
				if preview.Object.Content != nil {
					preview.Object.Content.Truncated = true
				}
			}
		}
		return preview, nil
	}
	if formatTypeFromMetaAttributes(combinedAttributes) == format.FormatS3M {
		if applyS3MScenePreview(combinedAttributes, preview.Object, resource.ID, bucket, objectPath) {
			return preview, nil
		}
	}

	if nodeType == "bucket" || nodeType == "prefix" || nodeType == "directory" || objectPath == "" {
		preview.Mode = PreviewModeNode
		if node != nil {
			preview.Object.SizeBytes = node.TotalSizeBytes
			preview.Object.ObjectCount = int64(node.ItemCount)
		}
		children, err := listObjectPreviewChildren(ctx, catalogProvider, connInfo, resource.ID, bucket, objectPath)
		if err != nil {
			return nil, err
		}
		preview.Object.NodeType = "directory"
		preview.Object.ContentType = "application/x-directory"
		preview.Object.Children = children
		return preview, nil
	}

	if objectPath == "" {
		return nil, fmt.Errorf("object path is empty")
	}
	preview.Object.StorageRef = fmt.Sprintf("%s/%s", bucket, objectPath)
	preview.Object.Download = previewDownloadPlan(resource.ID, "object", preview.Object.StorageRef, objectPath, "")

	stat, err := resourceutil.ObjectStorageFacts(ctx, factsProvider, connInfo, resource.ID, bucket, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat object %s: %w", objectPath, err)
	}

	if item != nil {
		if item.ObjectSizeBytes != nil {
			preview.Object.SizeBytes = *item.ObjectSizeBytes
		} else if item.SizeBytes != nil {
			preview.Object.SizeBytes = *item.SizeBytes
		} else if sizeBytes := resourceutil.Int64Attribute(item.Attributes, "size_bytes"); sizeBytes > 0 {
			preview.Object.SizeBytes = sizeBytes
		} else if totalSize := resourceutil.Int64Attribute(item.Attributes, "total_size"); totalSize > 0 {
			preview.Object.SizeBytes = totalSize
		} else {
			preview.Object.SizeBytes = stat.Size
		}
		if rowCount := item.RowCount; rowCount != nil {
			preview.Object.ObjectCount = *rowCount
		}
		if v := resourceutil.StringAttribute(item.Attributes, "content_type"); v != "" {
			preview.Object.ContentType = v
		}
	} else {
		preview.Object.SizeBytes = stat.Size
	}

	if !stat.ModifiedAt.IsZero() {
		mod := stat.ModifiedAt
		preview.Object.LastModified = &mod
	}

	metadata := map[string]string{
		"etag": stat.ETag,
	}
	if len(metadata) > 0 {
		preview.Object.Metadata = metadata
	}

	rawContentType := stat.ContentType
	if preview.Object.ContentType != "" {
		rawContentType = preview.Object.ContentType
	}
	canonicalContentType := objectcontent.InferContentType(objectPath, rawContentType)
	preview.Object.ContentType = canonicalContentType

	if p.content != nil && objectPath != "" {
		// 按照路径统一规范拆分：path（目录，以/结尾）、name（文件名）
		dir, name := commonModels.SplitObjectPath(objectPath)
		contentReq := &objectcontent.ObjectContentRequest{
			Locator:     req.Locator,
			Bucket:      bucket,
			Path:        dir,  // 目录路径（以 / 结尾）
			Name:        name, // 文件名
			Format:      normalizeObjectContentRequestFormat(resourceutil.StringAttribute(combinedAttributes, "format")),
			Extension:   defaultExtension(objectPath),
			ContentType: canonicalContentType,
			Size:        stat.Size,
			Attributes:  preview.Object.Attributes,
		}
		if isCADObjectContentRequest(contentReq) {
			url, err := resolveCADPreviewURL(ctx, p.cadPreviewRepo, req, contentReq)
			if err != nil {
				return nil, err
			}
			contentReq.PreviewURL = url
			preview.Object.URL = url
		} else if formatTypeFromMetaAttributes(combinedAttributes) == format.FormatPMTiles {
			contentReq.PreviewURL = buildPMTilesTileURL(req.Locator)
			preview.Object.URL = contentReq.PreviewURL
		} else if url := buildStorageStreamURL(resource.ID, preview.Object.StorageRef); url != "" {
			contentReq.PreviewURL = url
			preview.Object.URL = url
		}
		handler := p.content.Resolve(contentReq)
		if handler != nil {
			if objectcontent.IsContainerFormat(contentReq.Format) {
				if content := containerPreviewContentFromMetaAttributes(preview.Object.Attributes, stat.Size, contentReq.Path, contentReq.Name); content != nil {
					preview.Object.Content = content
					return preview, nil
				}
			}
			if streamHandler, ok := handler.(objectcontent.StreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return resourceutil.OpenObjectContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
				}

				content, truncated, err := streamHandler.HandleStream(ctx, contentReq, streamer)
				if err != nil {
					return nil, err
				}
				if content != nil {
					preview.Object.Content = content
					if truncated || content.Truncated {
						preview.Object.Truncated = true
						preview.Object.Content.Truncated = true
					}
				}
			} else {
				// 回退到传统的字节数组处理（适用于小文件）
				fetcher := func(limit int64) ([]byte, bool, error) {
					limitBytes := limit
					if limitBytes <= 0 {
						limitBytes = maxTextPreviewBytes
					}
					reader, err := resourceutil.OpenObjectContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
					if err != nil {
						return nil, false, fmt.Errorf("failed to get object: %w", err)
					}
					defer reader.Close()

					data, truncated, err := readObjectWithLimit(reader, limitBytes)
					if err != nil {
						return nil, false, err
					}
					return data, truncated, nil
				}

				content, truncated, err := handler.Handle(ctx, contentReq, fetcher)
				if err != nil {
					return nil, err
				}
				if content != nil {
					preview.Object.Content = content
					if truncated || content.Truncated {
						preview.Object.Truncated = true
						preview.Object.Content.Truncated = true
					}
				}
			}
		}
	}

	// Manager不做任何元数据解析，只负责原样传递Meta存储的attributes
	// 前端会根据attributes中的内容自动识别和显示元数据

	return preview, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getTokenFromContext(ctx context.Context) string {
	// 尝试从context获取token
	// 这需要在handler中将token放入context
	if token, ok := ctx.Value("jwt_token").(string); ok {
		return token
	}
	return ""
}

func (p *objectCatalogPreviewProvider) connectionInfo(engine *models.Engine) (plugin.ConnectionInfo, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	return plugin.ConnectionInfo(engine.ConnectionInfo), nil
}

func listObjectPreviewChildren(ctx context.Context, catalogProvider plugin.EngineCatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, prefix string) ([]models.ObjectPreviewChild, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.ObjectDirectoryPath(engineID, bucket, prefix), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	children := make([]models.ObjectPreviewChild, 0, len(nodes))
	for _, node := range nodes {
		if isReservedSegment(node.Name) {
			continue
		}
		childType := "object"
		contentType := catalogEntryContentType(node)
		if node.Role == plugin.EngineCatalogRoleBranch {
			childType = "prefix"
			contentType = "application/x-directory"
		}
		childPath := strings.TrimPrefix(resourceutil.NodePhysicalPath(node), strings.Trim(bucket, "/")+"/")
		if childPath == "" {
			childPath = joinObjectPath(prefix, node.Name)
		}
		child := models.ObjectPreviewChild{
			Name:        node.Name,
			Path:        childPath,
			Type:        childType,
			SizeBytes:   catalogEntrySizeBytes(node),
			ContentType: objectcontent.InferContentType(childPath, contentType),
		}
		if node.UpdatedAt != nil {
			mod := *node.UpdatedAt
			child.LastModified = &mod
		}
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})
	return children, nil
}

func catalogEntryContentType(node plugin.EngineCatalogEntry) string {
	if node.Storage == nil {
		return ""
	}
	return node.Storage.ContentType
}

func catalogEntrySizeBytes(node plugin.EngineCatalogEntry) int64 {
	if node.Storage == nil || node.Storage.SizeBytes == nil {
		return 0
	}
	return *node.Storage.SizeBytes
}

func buildStorageStreamURL(engineID uint, storageRef string) string {
	storageRef = strings.Trim(storageRef, "/")
	if engineID == 0 || storageRef == "" {
		return ""
	}
	values := url.Values{}
	values.Set("engine_id", strconv.FormatUint(uint64(engineID), 10))
	values.Set("storage_ref", storageRef)
	return "/api/v1/manager/storage-stream?" + values.Encode()
}

func buildPMTilesTileURL(locator string) string {
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return ""
	}
	values := url.Values{}
	values.Set("locator", locator)
	return "/api/v1/manager/quick-view/tiles/{z}/{x}/{y}.mvt?" + values.Encode()
}

func readObjectWithLimit(reader io.Reader, limit int64) ([]byte, bool, error) {
	if limit <= 0 {
		limit = maxTextPreviewBytes
	}
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read object: %w", err)
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	return data, truncated, nil
}

func defaultExtension(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return ""
	}
	return ext
}

func isReservedSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return true
	}
	_, ok := reservedObjectSegments[segment]
	return ok
}

func joinObjectPath(prefix, name string) string {
	prefix = strings.Trim(prefix, "/")
	name = strings.Trim(name, "/")
	if prefix == "" {
		return name
	}
	if name == "" {
		return prefix
	}
	return prefix + "/" + name
}
