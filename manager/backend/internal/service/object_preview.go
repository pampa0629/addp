package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
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
	maxPPTXPreviewBytes   = 100 * 1024 * 1024 // 100MB - PPTX 文件预览限制
	maxSQLitePreviewBytes = 30 * 1024 * 1024  // 30MB - SQLite 文件默认预览限制
)

var reservedObjectSegments = map[string]struct{}{
	"__bucket__": {},
	".minio.sys": {},
}

type objectStoragePreviewProvider struct {
	metadataRepo *repository.MetadataRepository
	content      *ObjectContentRegistry
	priority     int
}

func newObjectStoragePreviewProvider(metadataRepo *repository.MetadataRepository, content *ObjectContentRegistry) PreviewProvider {
	return &objectStoragePreviewProvider{
		metadataRepo: metadataRepo,
		content:      content,
		priority:     95,
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

func (p *objectStoragePreviewProvider) Supports(req *PreviewRequest) bool {
	if req == nil || req.Resource == nil {
		return false
	}

	if !isObjectStorageType(req.Resource.ResourceType) {
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
	resource := req.Resource
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

	var item *models.MetaItemLite
	var node *models.MetaNodeLite
	var err error

	if objectPath != "" {
		item, err = p.metadataRepo.GetObjectMetadataItem(resource.ID, bucket, objectPath)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			item = nil
		}
	}

	if item == nil {
		node, err = p.metadataRepo.GetObjectMetadataNode(resource.ID, bucket, objectPath)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			node = nil
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
		Mode:            mode,
		Page:            1,
		PageSize:        1,
		Columns:         []string{},
		Rows:            []map[string]interface{}{},
		Object:          &models.ObjectPreview{Bucket: bucket, Path: displayPath, NodeType: nodeType},
		GeometryColumns: []string{},
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

	rawContentType := stat.ContentType
	if preview.Object.ContentType != "" {
		rawContentType = preview.Object.ContentType
	}
	canonicalContentType := inferContentType(objectPath, rawContentType)
	preview.Object.ContentType = canonicalContentType

	if p.content != nil && objectPath != "" {
		req := &ObjectContentRequest{
			Path:        objectPath,
			Extension:   defaultExtension(objectPath),
			ContentType: canonicalContentType,
			Size:        stat.Size,
		}
		handler := p.content.Resolve(req)
		if handler != nil {
			// 优先使用流式处理（对于 SQLite 等大文件）
			if streamHandler, ok := handler.(StreamableContentHandler); ok {
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

	return preview, nil
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
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".json":
		return "application/json"
	case ".geojson":
		return "application/geo+json"
	case ".txt", ".log":
		return "text/plain"
	}

	if contentType != "" {
		return contentType
	}
	return "application/octet-stream"
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
