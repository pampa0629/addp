package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
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

type objectStoragePreviewProvider struct {
	metadataRepo   *repository.MetadataRepository
	metaClient     *commonClient.MetaClient
	metaServiceURL string
	content        *ObjectContentRegistry
	priority       int
}

func NewObjectStoragePreviewProvider(metadataRepo *repository.MetadataRepository, metaClient *commonClient.MetaClient, metaServiceURL string, content *ObjectContentRegistry) PreviewProvider {
	return &objectStoragePreviewProvider{
		metadataRepo:   metadataRepo,
		metaClient:     metaClient,
		metaServiceURL: metaServiceURL,
		content:        content,
		priority:       95,
	}
}

func (p *objectStoragePreviewProvider) Name() string {
	return "builtin:object-storage"
}

func (p *objectStoragePreviewProvider) Priority() int {
	return p.priority
}

type objectStorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	PathStyle bool
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

// createMinioClientFromEngine 从 Engine 创建 MinIO 客户端
// 返回: MinIO 客户端、bucket 名称（可能为空）、错误
func createMinioClientFromEngine(engine *models.Engine) (*minio.Client, string, error) {
	connInfo := engine.ConnectionInfo

	endpoint, _ := connInfo["endpoint"].(string)
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	useSSL, _ := connInfo["use_ssl"].(bool)
	bucket, _ := connInfo["bucket"].(string)

	// endpoint, accessKey, secretKey 是必需的
	// bucket 可以为空(从 schema 参数获取)
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, "", fmt.Errorf("missing required connection info")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, "", err
	}

	return client, bucket, nil
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

func (p *objectStoragePreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Engine == nil {
		return false
	}

	if !isObjectStorageType(req.Engine.EngineType) {
		return false
	}

	// schema 代表 bucket
	if req.Schema == "" {
		return false
	}

	// 对象存储插件处理 node/object 场景
	return true
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
				fmt.Printf("[DEBUG] 创建用户 Meta Client, token 前缀: %s..., URL: %s\n", parts[1][:20], p.metaServiceURL)
			} else {
				fmt.Printf("[DEBUG] Authorization header 格式不正确: %s\n", authHeader)
			}
		} else {
			fmt.Printf("[DEBUG] 没有 Authorization header,使用默认 metaClient\n")
		}
	} else {
		fmt.Printf("[DEBUG] context 不是 *gin.Context 类型\n")
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

	decrypted, err := p.metadataRepo.DecryptConnectionInfo(resource.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt connection info: %w", err)
	}

	cfg, err := buildObjectStorageConfig(decrypted)
	if err != nil {
		return nil, err
	}

	client, err := newMinioClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to init minio client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	nodeType := "object"
	if item == nil {
		if node != nil {
			nodeType = strings.ToLower(node.NodeType)
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
		children, err := listImmediateChildren(ctx, client, bucket, objectPath)
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

	stat, err := client.StatObject(ctx, bucket, objectPath, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to stat object %s: %w", objectPath, err)
	}

	if item != nil {
		if item.ObjectSizeBytes != nil {
			preview.Object.SizeBytes = *item.ObjectSizeBytes
		} else if item.SizeBytes != nil {
			preview.Object.SizeBytes = *item.SizeBytes
		} else {
			preview.Object.SizeBytes = stat.Size
		}
		if rowCount := item.RowCount; rowCount != nil {
			preview.Object.ObjectCount = *rowCount
		}
		if v, ok := item.Attributes["file_type"].(string); ok && v != "" {
			preview.Object.ContentType = v
		}
	} else {
		preview.Object.SizeBytes = stat.Size
	}

	if !stat.LastModified.IsZero() {
		mod := stat.LastModified
		preview.Object.LastModified = &mod
	}

	metadata := map[string]string{
		"etag": stat.ETag,
	}
	for k, v := range stat.UserMetadata {
		metadata[strings.ToLower(k)] = v
	}
	if len(metadata) > 0 {
		preview.Object.Metadata = metadata
	}

	if item != nil && len(item.Attributes) > 0 {
		preview.Object.Attributes = item.Attributes
		// Debug: 打印attributes内容以排查问题
		if jsonBytes, err := json.MarshalIndent(item.Attributes, "", "  "); err == nil {
			fmt.Printf("DEBUG: item.Attributes JSON =\n%s\n", string(jsonBytes))
		}
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
			Extension:   defaultExtension(objectPath),
			ContentType: canonicalContentType,
			Size:        stat.Size,
		}
		handler := p.content.Resolve(req)
		if handler != nil {
			// 优先支持复合流式处理（如 Shapefile 等多文件场景）
			if compositeHandler, ok := handler.(CompositeStreamableContentHandler); ok {
				streamer := func() (io.ReadCloser, error) {
					return client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
				}
				siblingProvider := func(path string) (io.ReadCloser, error) {
					return getObjectStream(ctx, client, bucket, path)
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
					return client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
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
					reader, err := client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
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
		extractorAvailable, _ := combinedAttributes["extractor_available"].(bool)
		hasExtracted := preview.Object.ExtractedMetadata != nil

		if extractorAvailable && !hasExtracted {
			extracted := p.tryExtractMetadataFromMeta(ctx, resource.ID, bucket, objectPath, client)
			if extracted != nil {
				preview.Object.ExtractedMetadata = extracted
				if preview.Object.Attributes == nil {
					preview.Object.Attributes = make(models.JSONMap)
				}
				preview.Object.Attributes["extracted_metadata"] = extracted
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
	raw, ok := attrs["file_type"]
	if !ok {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))
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
	client *minio.Client,
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
	objReader, err := client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
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

func buildObjectStorageConfig(info models.ConnectionInfo) (*objectStorageConfig, error) {
	getString := func(key string) string {
		if v, ok := info[key]; ok {
			switch val := v.(type) {
			case string:
				return val
			case fmt.Stringer:
				return val.String()
			case float64:
				return fmt.Sprintf("%.0f", val)
			}
		}
		return ""
	}

	parseBool := func(key string) bool {
		v, ok := info[key]
		if !ok {
			return false
		}
		switch val := v.(type) {
		case bool:
			return val
		case string:
			return strings.EqualFold(val, "true") || val == "1"
		case float64:
			return val != 0
		default:
			return false
		}
	}

	cfg := &objectStorageConfig{
		Endpoint:  normalizeEndpoint(getString("endpoint")),
		AccessKey: getString("access_key"),
		SecretKey: getString("secret_key"),
		Region:    getString("region"),
		UseSSL:    parseBool("use_ssl"),
		PathStyle: parseBool("path_style"),
	}

	if cfg.Endpoint == "" {
		host := getString("host")
		port := getString("port")
		if host != "" {
			cfg.Endpoint = normalizeEndpoint(host)
			if port != "" && !strings.Contains(cfg.Endpoint, ":") {
				cfg.Endpoint = fmt.Sprintf("%s:%s", cfg.Endpoint, port)
			}
		}
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("missing endpoint for object storage")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("missing access credentials for object storage")
	}

	return cfg, nil
}

func newMinioClient(cfg *objectStorageConfig) (*minio.Client, error) {
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	}
	if cfg.Region != "" {
		opts.Region = cfg.Region
	}
	if cfg.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	return minio.New(cfg.Endpoint, opts)
}

func getObjectStream(ctx context.Context, client *minio.Client, bucket, key string) (io.ReadCloser, error) {
	cleanKey := strings.TrimLeft(key, "/")
	if cleanKey == "" {
		return nil, fs.ErrNotExist
	}

	if _, err := client.StatObject(ctx, bucket, cleanKey, minio.StatObjectOptions{}); err != nil {
		if isObjectNotFoundErr(err) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}

	reader, err := client.GetObject(ctx, bucket, cleanKey, minio.GetObjectOptions{})
	if err != nil {
		if isObjectNotFoundErr(err) {
			return nil, fs.ErrNotExist
		}
		return nil, err
	}
	return reader, nil
}

func isObjectNotFoundErr(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" {
		return true
	}
	return resp.StatusCode == http.StatusNotFound
}

func listImmediateChildren(ctx context.Context, client *minio.Client, bucket, path string) ([]models.ObjectPreviewChild, error) {
	cleanPrefix := strings.Trim(path, "/")
	listPrefix := cleanPrefix
	if listPrefix != "" && !strings.HasSuffix(listPrefix, "/") {
		listPrefix += "/"
	}

	objectCh := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    listPrefix,
		Recursive: false,
	})

	dirSeen := make(map[string]struct{})
	var children []models.ObjectPreviewChild

	for object := range objectCh {
		if object.Err != nil {
			return nil, object.Err
		}
		relative := strings.TrimPrefix(object.Key, listPrefix)
		relative = strings.Trim(relative, "/")
		if relative == "" {
			continue
		}
		parts := strings.Split(relative, "/")
		name := parts[0]
		if isReservedSegment(name) {
			continue
		}
		childPath := joinObjectPath(cleanPrefix, name)
		if len(parts) > 1 || strings.HasSuffix(object.Key, "/") {
			if _, exists := dirSeen[name]; exists {
				continue
			}
			dirSeen[name] = struct{}{}
			children = append(children, models.ObjectPreviewChild{
				Name:        name,
				Path:        childPath,
				Type:        "prefix",
				ContentType: "application/x-directory",
			})
			continue
		}
		child := models.ObjectPreviewChild{
			Name:        name,
			Path:        childPath,
			Type:        "object",
			SizeBytes:   object.Size,
			ContentType: inferContentType(childPath, object.ContentType),
		}
		if !object.LastModified.IsZero() {
			mod := object.LastModified
			child.LastModified = &mod
		}
		children = append(children, child)
	}

	sort.Slice(children, func(i, j int) bool {
		return strings.ToLower(children[i].Name) < strings.ToLower(children[j].Name)
	})

	return children, nil
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

	switch strings.ToLower(filepath.Ext(objectPath)) {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".wps":
		return "application/vnd.ms-works"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".json":
		return "application/json"
	case ".geojson":
		return "application/geo+json"
	case ".shp":
		return "application/x-esri-shapefile"
	case ".txt", ".log":
		return "text/plain"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".flv":
		return "video/x-flv"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".m4v":
		return "video/x-m4v"
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

func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	endpoint = strings.TrimPrefix(endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")
	return endpoint
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
