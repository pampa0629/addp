package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

const (
	maxTextPreviewBytes  = 256 * 1024     // 256KB
	maxJSONPreviewBytes  = 512 * 1024     // 512KB
	maxGeoJSONPreview    = 1024 * 1024    // 1MB
	maxImagePreviewBytes = 3 * 1024 * 1024 // 3MB
	maxPDFPreviewBytes   = 10 * 1024 * 1024 // 10MB - PDF文件预览限制
)

var reservedObjectSegments = map[string]struct{}{
	"__bucket__": {},
	".minio.sys": {},
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

func (s *MetadataService) previewObjectStorage(resource *models.Resource, bucket, path string) (*models.TablePreview, error) {
	objectPath := strings.Trim(path, "/")

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
		item, err = s.metadataRepo.GetObjectMetadataItem(resource.ID, bucket, objectPath)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			item = nil
		}
	}

	if item == nil {
		node, err = s.metadataRepo.GetObjectMetadataNode(resource.ID, bucket, objectPath)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			node = nil
		}
	}

	decrypted, err := s.metadataRepo.DecryptConnectionInfo(resource.ConnectionInfo)
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

	preview := &models.TablePreview{
		Mode:            "object",
		Page:            1,
		PageSize:        1,
		Columns:         []string{},
		Rows:            []map[string]interface{}{},
		Object:          &models.ObjectPreview{Bucket: bucket, Path: objectPath, NodeType: nodeType},
		GeometryColumns: []string{},
	}

	if nodeType == "bucket" || nodeType == "prefix" || nodeType == "directory" {
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

	contentType := stat.ContentType
	if preview.Object.ContentType != "" {
		contentType = preview.Object.ContentType
	}
	content, truncated, err := fetchObjectContent(ctx, client, bucket, objectPath, contentType, stat.Size)
	if err != nil {
		return nil, err
	}
	if content != nil {
		preview.Object.Content = content
		preview.Object.ContentType = inferContentType(objectPath, contentType)
		if truncated {
			preview.Object.Truncated = true
			if preview.Object.Content != nil {
				preview.Object.Content.Truncated = true
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

func fetchObjectContent(ctx context.Context, client *minio.Client, bucket, objectPath, contentType string, size int64) (*models.ObjectPreviewContent, bool, error) {
	kind := detectContentKind(objectPath, contentType)
	reader, err := client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, false, fmt.Errorf("failed to get object: %w", err)
	}
	defer reader.Close()

	var limit int64
	switch kind {
	case "image":
		limit = maxImagePreviewBytes
	case "geojson":
		limit = maxGeoJSONPreview
	case "json":
		limit = maxJSONPreviewBytes
	case "pdf":
		limit = maxPDFPreviewBytes
	default:
		limit = maxTextPreviewBytes
	}

	if size > 0 && size < limit {
		limit = size
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

	switch kind {
	case "image":
		if truncated || len(data) == 0 {
			return &models.ObjectPreviewContent{
				Kind:      "image",
				Text:      "图片超出预览大小限制，无法展示",
				Truncated: true,
			}, true, nil
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		return &models.ObjectPreviewContent{
			Kind:      "image",
			ImageData: encoded,
			Encoding:  "base64",
		}, false, nil
	case "pdf":
		// PDF 文件返回 base64 编码数据
		if truncated || len(data) == 0 {
			return &models.ObjectPreviewContent{
				Kind:      "pdf",
				Text:      "PDF 文件超出预览大小限制（10MB）",
				Truncated: true,
			}, true, nil
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		return &models.ObjectPreviewContent{
			Kind:     "pdf",
			Data:     encoded, // 使用 Data 字段存储 PDF base64
			Encoding: "base64",
		}, false, nil
	case "geojson":
		// 去除UTF-8 BOM (Byte Order Mark) 如果存在
		cleanData := data
		if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
			cleanData = data[3:]
		}

		var parsed interface{}
		if err := json.Unmarshal(cleanData, &parsed); err != nil {
			return &models.ObjectPreviewContent{
				Kind:      "text",
				Text:      string(data),
				Truncated: truncated,
			}, truncated, nil
		}
		return &models.ObjectPreviewContent{
			Kind:    "geojson",
			Text:    string(cleanData),
			GeoJSON: parsed,
		}, truncated, nil
	case "json":
		// 去除UTF-8 BOM (Byte Order Mark) 如果存在
		cleanData := data
		if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
			cleanData = data[3:]
		}

		var parsed interface{}
		if err := json.Unmarshal(cleanData, &parsed); err != nil {
			return &models.ObjectPreviewContent{
				Kind:      "text",
				Text:      string(data),
				Truncated: truncated,
			}, truncated, nil
		}
		return &models.ObjectPreviewContent{
			Kind: "json",
			Text: string(cleanData),
			JSON: parsed,
		}, truncated, nil
	default:
		return &models.ObjectPreviewContent{
			Kind:      "text",
			Text:      string(data),
			Truncated: truncated,
		}, truncated, nil
	}
}

func detectContentKind(objectPath, contentType string) string {
	ext := strings.ToLower(filepath.Ext(objectPath))
	contentTypeLower := strings.ToLower(contentType)

	// 检查 PDF
	if contentTypeLower == "application/pdf" || strings.Contains(contentTypeLower, "pdf") || ext == ".pdf" {
		return "pdf"
	}

	// 检查图片
	if strings.HasPrefix(contentType, "image/") {
		switch strings.ToLower(contentType) {
		case "image/png", "image/jpeg", "image/jpg":
			return "image"
		}
	}

	// 检查 GeoJSON: 支持 "geojson", "geo+json", "application/geo+json" 等
	if strings.Contains(contentTypeLower, "geojson") || strings.Contains(contentTypeLower, "geo+json") || ext == ".geojson" {
		return "geojson"
	}
	if strings.Contains(contentTypeLower, "json") || ext == ".json" {
		return "json"
	}
	if strings.HasPrefix(contentTypeLower, "text/") {
		return "text"
	}
	switch ext {
	case ".png", ".jpg", ".jpeg":
		return "image"
	case ".txt", ".log", ".csv":
		return "text"
	}
	return "text"
}

func inferContentType(objectPath, contentType string) string {
	if contentType != "" {
		return contentType
	}
	switch strings.ToLower(filepath.Ext(objectPath)) {
	case ".pdf":
		return "application/pdf"
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
	default:
		return "application/octet-stream"
	}
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
