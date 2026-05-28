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
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/catalogutil"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/objectcontent"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
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
	metaClient     *commonClient.MetaClient
	metaServiceURL string
	content        *objectcontent.ObjectContentRegistry
}

func NewObjectCatalogPreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, content *objectcontent.ObjectContentRegistry) PreviewProvider {
	return &objectCatalogPreviewProvider{
		metadataRepo:   metadataRepo,
		metaClient:     metaClient,
		metaServiceURL: metaServiceURL,
		content:        content,
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
	preview.Object.StorageRef = fmt.Sprintf("%s/%s", bucket, objectPath)
	preview.Object.Download = previewDownloadPlan(resource.ID, preview.Object.StorageRef, objectPath, "")

	stat, err := catalogutil.ObjectMetadata(ctx, metadataProvider, connInfo, resource.ID, bucket, objectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat object %s: %w", objectPath, err)
	}

	if item != nil {
		if item.ObjectSizeBytes != nil {
			preview.Object.SizeBytes = *item.ObjectSizeBytes
		} else if item.SizeBytes != nil {
			preview.Object.SizeBytes = *item.SizeBytes
		} else if sizeBytes := catalogutil.Int64Attribute(item.Attributes, "size_bytes"); sizeBytes > 0 {
			preview.Object.SizeBytes = sizeBytes
		} else if totalSize := catalogutil.Int64Attribute(item.Attributes, "total_size"); totalSize > 0 {
			preview.Object.SizeBytes = totalSize
		} else {
			preview.Object.SizeBytes = stat.Size
		}
		if rowCount := item.RowCount; rowCount != nil {
			preview.Object.ObjectCount = *rowCount
		}
		if v := catalogutil.StringAttribute(item.Attributes, "content_type"); v != "" {
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
		req := &objectcontent.ObjectContentRequest{
			Bucket:      bucket,
			Path:        dir,  // 目录路径（以 / 结尾）
			Name:        name, // 文件名
			Format:      normalizeObjectContentRequestFormat(catalogutil.StringAttribute(metaItemLiteAttributes(item), "format")),
			Extension:   defaultExtension(objectPath),
			ContentType: canonicalContentType,
			Size:        stat.Size,
			Attributes:  preview.Object.Attributes,
		}
		if url := buildStorageStreamURL(resource.ID, preview.Object.StorageRef); url != "" {
			req.PreviewURL = url
			preview.Object.URL = url
		}
		handler := p.content.Resolve(req)
		if handler != nil {
			if objectcontent.IsContainerFormat(req.Format) {
				if content := containerPreviewContentFromMetaAttributes(preview.Object.Attributes, stat.Size, req.Path, req.Name); content != nil {
					preview.Object.Content = content
					return preview, nil
				}
			}
			if streamHandler, ok := handler.(objectcontent.StreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return catalogutil.OpenObjectContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
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
					reader, err := catalogutil.OpenObjectContent(ctx, contentReader, connInfo, resource.ID, bucket, objectPath)
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

func (p *objectCatalogPreviewProvider) decryptedConnectionInfo(engine *models.Engine) (plugin.ConnectionInfo, error) {
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

func listObjectPreviewChildren(ctx context.Context, catalogProvider plugin.CatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, prefix string) ([]models.ObjectPreviewChild, error) {
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
		contentType := catalogutil.StringAttribute(node.Attributes, "content_type")
		if node.IsContainer {
			childType = "prefix"
			contentType = "application/x-directory"
		}
		childPath := strings.TrimPrefix(catalogutil.NodePhysicalPath(node), strings.Trim(bucket, "/")+"/")
		if childPath == "" {
			childPath = joinObjectPath(prefix, node.Name)
		}
		child := models.ObjectPreviewChild{
			Name:        node.Name,
			Path:        childPath,
			Type:        childType,
			SizeBytes:   catalogutil.Int64Stat(node.Stats, "size_bytes"),
			ContentType: objectcontent.InferContentType(childPath, contentType),
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
