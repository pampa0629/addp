package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxTextPreviewBytes   = 256 * 1024        // 256KB
	maxJSONPreviewBytes   = 512 * 1024        // 512KB
	maxGeoJSONPreview     = 1024 * 1024       // 1MB
	maxImagePreviewBytes  = 10 * 1024 * 1024  // 10MB - 图片预览限制
	maxPDFPreviewBytes    = 20 * 1024 * 1024  // 20MB - PDF 文件预览限制
	maxDOCXPreviewBytes   = 100 * 1024 * 1024 // 100MB - DOCX 文件预览限制
	maxWPSPreviewBytes    = 100 * 1024 * 1024 // 100MB - WPS 文件预览限制
	maxPPTXPreviewBytes   = 100 * 1024 * 1024 // 100MB - PPTX 文件预览限制
	maxSQLitePreviewBytes = 30 * 1024 * 1024  // 30MB - SQLite 文件默认预览限制
)

var reservedObjectSegments = map[string]struct{}{
	"__bucket__": {},
	".minio.sys": {},
}

const shapefileGeometryColumn = "__geometry__"

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

type objectStoragePreviewProvider struct {
	metadataRepo   *repository.MetadataRepository
	metaClient     *commonClient.MetaClient
	metaServiceURL string
	content        *ObjectContentRegistry
}

func NewObjectStoragePreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, content *ObjectContentRegistry) PreviewProvider {
	return &objectStoragePreviewProvider{
		metadataRepo:   metadataRepo,
		metaClient:     metaClient,
		metaServiceURL: metaServiceURL,
		content:        content,
	}
}

func (p *objectStoragePreviewProvider) Name() string {
	return "builtin:object-storage"
}

func isObjectStorageType(resourceType string) bool {
	switch strings.ToLower(resourceType) {
	case "minio", "s3", "oss", "object_storage", "object-storage":
		return true
	default:
		return false
	}
}

// isFileSystemType 判断引擎类型是否为文件系统类（NFS 等）
func isFileSystemType(resourceType string) bool {
	switch strings.ToLower(resourceType) {
	case "nfs":
		return true
	default:
		return false
	}
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

func (p *objectStoragePreviewProvider) Preview(ctx context.Context, req *PreviewRequest) (*models.TablePreview, error) {
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

	// 从 Gin context 的 Authorization header 中提取 JWT token,创建临时的 Meta Client 用于用户认证
	metaClient := p.metaClient
	if ginCtx, ok := ctx.(*gin.Context); ok {
		if authHeader := ginCtx.GetHeader("Authorization"); authHeader != "" {
			// Authorization header 格式: "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[1] != "" {
				// 使用用户的 JWT token 创建临时客户端
				metaClient = commonClient.NewMetaClient(p.metaServiceURL, parts[1])
			}
		}
	}

	// 设置租户 ID（仅当使用服务级别的内部 API client 时）
	// 使用 JWT token 创建的客户端会从 token 中自动提取 tenant_id
	if metaClient == p.metaClient && metaClient != nil {
		metaClient.SetTenantID(req.TenantID)
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

	connInfo, err := p.decryptedConnectionInfo(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt connection info: %w", err)
	}

	pl, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	catalogProvider, _ := pl.(plugin.CatalogProvider)
	metadataProvider, _ := pl.(plugin.ItemMetadataProvider)
	contentReader, _ := pl.(plugin.ContentReadableProvider)
	if catalogProvider == nil {
		return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
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
	if node != nil && len(node.Attributes) > 0 {
		attributeSources = append(attributeSources, node.Attributes)
	}
	if item != nil && len(item.Attributes) > 0 {
		attributeSources = append(attributeSources, item.Attributes)
	}
	combinedAttributes := mergeJSONMaps(attributeSources...)
	if len(combinedAttributes) > 0 {
		preview.Object.Attributes = combinedAttributes
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
	preview.Object.ObjectKey = fmt.Sprintf("%s/%s", bucket, objectPath)

	stat, err := getObjectPreviewMetadata(ctx, metadataProvider, connInfo, resource.ID, bucket, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat object %s: %w", objectPath, err)
	}

	if item != nil {
		if item.ObjectSizeBytes != nil {
			preview.Object.SizeBytes = *item.ObjectSizeBytes
		} else if item.SizeBytes != nil {
			preview.Object.SizeBytes = *item.SizeBytes
		} else if sizeBytes := int64Attribute(item.Attributes, "size_bytes"); sizeBytes > 0 {
			preview.Object.SizeBytes = sizeBytes
		} else if totalSize := int64Attribute(item.Attributes, "total_size"); totalSize > 0 {
			preview.Object.SizeBytes = totalSize
		} else {
			preview.Object.SizeBytes = stat.Size
		}
		if rowCount := item.RowCount; rowCount != nil {
			preview.Object.ObjectCount = *rowCount
		}
		if v := stringAttribute(item.Attributes, "content_type"); v != "" {
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

	if item != nil && len(item.Attributes) > 0 {
		preview.Object.Attributes = item.Attributes
	}

	rawContentType := stat.ContentType
	if preview.Object.ContentType != "" {
		rawContentType = preview.Object.ContentType
	}
	canonicalContentType := inferContentType(objectPath, rawContentType)
	preview.Object.ContentType = canonicalContentType

	if p.content != nil && objectPath != "" {
		// 按照路径统一规范拆分：path（目录，以/结尾）、name（文件名）
		dir, name := commonModels.SplitObjectPath(objectPath)
		req := &ObjectContentRequest{
			Bucket:      bucket,
			Path:        dir,  // 目录路径（以 / 结尾）
			Name:        name, // 文件名
			Format:      stringAttribute(metaItemLiteAttributes(item), "format"),
			Extension:   defaultExtension(objectPath),
			ContentType: canonicalContentType,
			Size:        stat.Size,
			Attributes:  preview.Object.Attributes,
		}
		handler := p.content.Resolve(req)
		if handler != nil {
			if strings.EqualFold(req.Format, string(format.FormatExcel)) {
				if previewJSON := buildExcelPreviewFromAttributes(preview.Object.Attributes, stat.Size); previewJSON != nil {
					preview.Object.Content = &models.ObjectPreviewContent{
						Kind: models.ObjectPreviewKindExcel,
						JSON: previewJSON,
						Metadata: map[string]interface{}{
							"size_bytes": stat.Size,
							"path":       req.Path,
							"name":       req.Name,
							"source":     "meta",
						},
					}
					return preview, nil
				}
			}
			// 优先支持复合流式处理（如 Shapefile 等多文件场景）
			if compositeHandler, ok := handler.(CompositeStreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return openObjectStorageContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
				}
				siblingProvider := func(path string) (io.ReadCloser, error) {
					return readObjectStorageSibling(ctx, contentReader, connInfo, resource.ID, bucket, path)
				}

				content, truncated, err := compositeHandler.HandleCompositeStream(ctx, req, streamer, siblingProvider)
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
			} else if streamHandler, ok := handler.(StreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return openObjectStorageContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
				}

				content, truncated, err := streamHandler.HandleStream(ctx, req, streamer)
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
					reader, err := openObjectStorageContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
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

				content, truncated, err := handler.Handle(ctx, req, fetcher)
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

	applyShapefileTablePreview(preview)

	// Manager不做任何元数据解析，只负责原样传递Meta存储的attributes
	// 前端会根据attributes中的内容自动识别和显示元数据

	// 针对对象尝试触发提取
	if item != nil && objectPath != "" {
		extractorAvailable := commonJSON.Bool(combinedAttributes, "capabilities.extraction", "extractor_available")
		hasExtracted := preview.Object.ExtractedMetadata != nil

		if extractorAvailable && !hasExtracted {
			extracted := p.tryExtractMetadataFromMeta(ctx, resource.ID, bucket, objectPath, func() (io.ReadCloser, error) {
				return openObjectStorageContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
			})
			if extracted != nil {
				preview.Object.ExtractedMetadata = extracted
				if preview.Object.Attributes == nil {
					preview.Object.Attributes = make(models.JSONMap)
				}
				capabilities := commonJSON.Section(preview.Object.Attributes, "capabilities")
				if capabilities == nil {
					capabilities = map[string]interface{}{}
				}
				extraction, _ := capabilities["extraction"].(map[string]interface{})
				if extraction == nil {
					extraction = map[string]interface{}{}
				}
				extraction["metadata_extracted"] = true
				extraction["extracted_metadata"] = extracted
				capabilities["extraction"] = extraction
				preview.Object.Attributes["capabilities"] = capabilities
			}
		}
	}

	return preview, nil
}

func shouldBuildShapefileTablePreview(preview *models.TablePreview) bool {
	if preview == nil || preview.Object == nil || preview.Object.Content == nil {
		return false
	}
	if isShapefileFileType(preview.Object.Attributes) {
		return true
	}
	return strings.EqualFold(preview.Object.Content.Kind, "shapefile")
}

func applyShapefileTablePreview(preview *models.TablePreview) {
	if !shouldBuildShapefileTablePreview(preview) {
		return
	}

	cols, rows, geomCols, renderCols, srid, ok := buildShapefileTableRows(preview.Object.Content)
	if !ok {
		return
	}

	preview.Columns = cols
	preview.Rows = rows
	preview.GeometryColumns = geomCols
	preview.RenderGeometryColumns = renderCols
	if srid > 0 {
		preview.SRID = srid
	}
	if total, ok := resolveShapefilePreviewTotal(preview.Object.Content, len(rows)); ok {
		preview.Total = total
	} else {
		preview.Total = len(rows)
	}
	preview.Page = 1
	if preview.Total > 0 {
		preview.PageSize = preview.Total
	}
}

func isShapefileFileType(attrs models.JSONMap) bool {
	if len(attrs) == 0 {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(commonJSON.String(attrs, "storage", "file_type")))
	if value == "" {
		return false
	}
	return strings.Contains(value, "shp") || strings.Contains(value, "shapefile")
}

func buildShapefileTableRows(content *models.ObjectPreviewContent) ([]string, []map[string]interface{}, []string, map[string]string, int, bool) {
	if content == nil || content.GeoJSON == nil {
		return nil, nil, nil, nil, 0, false
	}

	geojson, ok := content.GeoJSON.(map[string]interface{})
	if !ok {
		return nil, nil, nil, nil, 0, false
	}

	rawFeatures, exists := geojson["features"]
	if !exists {
		return nil, nil, nil, nil, 0, false
	}

	// 兼容两种类型：[]map[string]interface{} 和 []interface{}
	var featureMaps []map[string]interface{}
	switch v := rawFeatures.(type) {
	case []map[string]interface{}:
		featureMaps = v
	case []interface{}:
		featureMaps = make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				featureMaps = append(featureMaps, m)
			}
		}
	default:
		return nil, nil, nil, nil, 0, false
	}

	if len(featureMaps) == 0 {
		return nil, nil, nil, nil, 0, false
	}

	columnsSet := make(map[string]struct{})
	rows := make([]map[string]interface{}, 0, len(featureMaps))
	renderGeometryColumn := renderGeometryColumnName(shapefileGeometryColumn)

	for _, feature := range featureMaps {
		row := make(map[string]interface{})
		if props, ok := feature["properties"].(map[string]interface{}); ok {
			for k, v := range props {
				row[k] = v
				columnsSet[k] = struct{}{}
			}
		}
		if geom, exists := feature["geometry"]; exists {
			row[shapefileGeometryColumn] = geom
			row[renderGeometryColumn] = geom
		}
		if id, exists := feature["id"]; exists {
			row["__feature_id"] = id
			columnsSet["__feature_id"] = struct{}{}
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		return nil, nil, nil, nil, 0, false
	}

	columns := make([]string, 0, len(columnsSet)+2)
	for col := range columnsSet {
		columns = append(columns, col)
	}
	sort.Strings(columns)
	columns = append(columns, shapefileGeometryColumn)
	columns = append(columns, renderGeometryColumn)

	renderColumns := map[string]string{
		shapefileGeometryColumn: renderGeometryColumn,
	}

	return columns, rows, []string{shapefileGeometryColumn}, renderColumns, resolveShapefilePreviewSRID(content), true
}

func resolveShapefilePreviewTotal(content *models.ObjectPreviewContent, fallback int) (int, bool) {
	if content == nil || content.Metadata == nil {
		return fallback, fallback > 0
	}
	raw, ok := content.Metadata["preview_feature_count"]
	if !ok {
		return fallback, fallback > 0
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return fallback, fallback > 0
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fallback, fallback > 0
		}
		return n, true
	default:
		return fallback, fallback > 0
	}
}

func resolveShapefilePreviewSRID(content *models.ObjectPreviewContent) int {
	if content == nil || content.Metadata == nil {
		return 0
	}
	raw, ok := content.Metadata["source_srid"]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// tryExtractMetadataFromMeta 尝试从Meta模块提取元数据
func (p *objectStoragePreviewProvider) tryExtractMetadataFromMeta(
	ctx context.Context,
	resourceID uint,
	bucket, objectPath string,
	openContent func() (io.ReadCloser, error),
) map[string]interface{} {
	// 获取Meta服务URL和token（从环境变量或配置）
	metaURL := getEnvOrDefault("META_URL", "http://localhost:8082")
	token := getTokenFromContext(ctx) // 从context获取JWT token

	if token == "" {
		// 没有token，无法调用Meta API
		return nil
	}

	// 创建Meta客户端
	metaClient := NewMetaClient(metaURL, token)

	// 下载对象内容（用于提取元数据）
	objectKey := bucket + "/" + objectPath
	objReader, err := openContent()
	if err != nil {
		return nil
	}
	defer objReader.Close()

	// 调用Meta提取
	extracted, err := metaClient.ExtractObjectMetadata(&ExtractObjectMetadataRequest{
		EngineID:   resourceID,
		ObjectKey:  objectKey,
		ObjectData: objReader,
	})
	if err != nil {
		// 提取失败，记录但不影响预览
		return nil
	}

	return extracted
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

func (p *objectStoragePreviewProvider) decryptedConnectionInfo(engine *models.Engine) (plugin.ConnectionInfo, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	connInfo := engine.ConnectionInfo
	if p != nil && p.metadataRepo != nil {
		decrypted, err := p.metadataRepo.DecryptConnectionInfo(engine.ConnectionInfo)
		if err != nil {
			return nil, err
		}
		connInfo = decrypted
	}
	return plugin.ConnectionInfo(connInfo), nil
}

func objectStorageCatalogPath(engineID uint, bucket, objectPath string, isContainer bool) plugin.CatalogPath {
	path := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
	}
	bucket = strings.Trim(bucket, "/")
	if bucket == "" {
		return path
	}
	path.Segments = append(path.Segments, plugin.CatalogSegment{
		Term: plugin.CatalogTermBucket,
		Kind: plugin.CatalogKindBucket,
		Name: bucket,
	})
	trimmed := strings.Trim(objectPath, "/")
	if trimmed == "" {
		return path
	}
	parts := strings.Split(trimmed, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		isLast := i == len(parts)-1
		segment := plugin.CatalogSegment{
			Term: plugin.CatalogTermPrefix,
			Kind: plugin.CatalogKindPrefix,
			Name: part,
		}
		if isLast && !isContainer {
			segment.Term = plugin.CatalogTermObject
			segment.Kind = plugin.CatalogKindObject
		}
		path.Segments = append(path.Segments, segment)
	}
	return path
}

func objectStorageDirectoryCatalogPath(engineID uint, bucket, prefix string) plugin.CatalogPath {
	return objectStorageCatalogPath(engineID, bucket, prefix, true)
}

func objectStorageObjectCatalogPath(engineID uint, bucket, objectPath string) plugin.CatalogPath {
	return objectStorageCatalogPath(engineID, bucket, objectPath, false)
}

func listObjectPreviewChildren(ctx context.Context, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, prefix string) ([]models.ObjectPreviewChild, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, objectStorageDirectoryCatalogPath(engineID, bucket, prefix), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	children := make([]models.ObjectPreviewChild, 0, len(nodes))
	for _, node := range nodes {
		if isReservedSegment(node.Name) {
			continue
		}
		childType := "object"
		contentType := stringAttribute(node.Attributes, "content_type")
		if node.IsContainer {
			childType = "prefix"
			contentType = "application/x-directory"
		}
		childPath := strings.TrimPrefix(catalogNodePhysicalPath(node), strings.Trim(bucket, "/")+"/")
		if childPath == "" {
			childPath = joinObjectPath(prefix, node.Name)
		}
		child := models.ObjectPreviewChild{
			Name:        node.Name,
			Path:        childPath,
			Type:        childType,
			SizeBytes:   int64Stat(node.Stats, "size_bytes"),
			ContentType: inferContentType(childPath, contentType),
		}
		if modifiedAt, ok := node.Attributes["modified_at"].(time.Time); ok && !modifiedAt.IsZero() {
			mod := modifiedAt
			child.LastModified = &mod
		}
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool {
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})
	return children, nil
}

func getObjectPreviewMetadata(ctx context.Context, metadataProvider plugin.ItemMetadataProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectPath string) (*plugin.FileMetadata, error) {
	if metadataProvider == nil {
		return nil, fs.ErrNotExist
	}
	item, err := metadataProvider.DescribeItem(ctx, connInfo, objectStorageObjectCatalogPath(engineID, bucket, objectPath), plugin.MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return itemMetadataToFileMetadata(item, bucket+"/"+strings.Trim(objectPath, "/")), nil
}

func openObjectStorageContent(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectPath string) (io.ReadCloser, error) {
	if contentReader == nil {
		return nil, fs.ErrNotExist
	}
	return contentReader.OpenContent(ctx, connInfo, objectStorageObjectCatalogPath(engineID, bucket, objectPath), plugin.ReadOptions{})
}

func readObjectStorageSibling(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, path string) (io.ReadCloser, error) {
	if contentReader == nil {
		return nil, fs.ErrNotExist
	}
	var lastErr error
	for _, candidate := range candidateObjectSiblingPathVariants(bucket, path) {
		reader, err := openObjectStorageContent(ctx, contentReader, connInfo, engineID, bucket, candidate)
		if err == nil {
			return reader, nil
		}
		if isFileSystemNotFoundErr(err) {
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		lastErr = err
	}
	if lastErr == nil || isFileSystemNotFoundErr(lastErr) {
		return nil, fs.ErrNotExist
	}
	return nil, lastErr
}

func candidateObjectSiblingPathVariants(bucket, path string) []string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	bucket = strings.Trim(bucket, "/")
	withoutLeading := strings.TrimLeft(trimmed, "/")
	withoutBucket := strings.TrimPrefix(withoutLeading, bucket+"/")
	candidates := []string{withoutBucket, withoutLeading, trimmed}
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimLeft(candidate, "/")
		if candidate == "" {
			continue
		}
		if _, exists := seen[candidate]; exists {
			continue
		}
		seen[candidate] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
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

func inferContentType(objectPath, contentType string) string {
	ctLower := strings.ToLower(strings.TrimSpace(contentType))
	if ctLower != "" && !isGenericContentType(ctLower) {
		return contentType
	}

	if guessed := format.GuessContentType(objectPath, nil); guessed != "" && !isGenericContentType(guessed) {
		return guessed
	}

	if contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}

func defaultExtension(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return ""
	}
	return ext
}

func isGenericContentType(contentType string) bool {
	switch contentType {
	case "", "application/octet-stream", "binary/octet-stream", "application/download", "application/force-download":
		return true
	}
	if strings.HasPrefix(contentType, "application/x-msdownload") {
		return true
	}
	if !strings.Contains(contentType, "/") {
		return true
	}
	return false
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
