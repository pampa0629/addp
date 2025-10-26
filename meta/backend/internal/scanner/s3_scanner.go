package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	metadataPartialReadSize int64 = 65535
	maxFullDocumentSize     int64 = 16 * 1024 * 1024
)

type s3Config struct {
	Endpoint  string   `json:"endpoint"`
	AccessKey string   `json:"access_key"`
	SecretKey string   `json:"secret_key"`
	Region    string   `json:"region"`
	UseSSL    bool     `json:"use_ssl"`
	Bucket    string   `json:"bucket"`
	Buckets   []string `json:"buckets"`
	Prefix    string   `json:"prefix"`
	Prefixes  []string `json:"prefixes"`
	PathStyle bool     `json:"path_style"`
}

type S3Scanner struct {
	client        *minio.Client
	cfg           s3Config
	allowedBucket map[string]struct{}
	resourceID    uint   // 资源ID，用于元数据提取
	scanDepth     string // 扫描深度：deep（深度）或 shallow（浅度）
}

var reservedObjectSegments = map[string]struct{}{
	"__bucket__": {},
	".minio.sys": {},
}

func isReservedObjectSegment(segment string) bool {
	_, ok := reservedObjectSegments[segment]
	return ok
}

func isReservedBucketName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	return isReservedObjectSegment(name)
}

func containsReservedObjectSegment(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if isReservedObjectSegment(strings.TrimSpace(part)) {
			return true
		}
	}
	return false
}

func hasReservedObjectSegment(path string) bool {
	clean := strings.Trim(path, "/")
	if clean == "" {
		return false
	}
	return containsReservedObjectSegment(strings.Split(clean, "/"))
}

func NewS3Scanner(connStr string) (Scanner, error) {
	var cfg s3Config
	if err := json.Unmarshal([]byte(connStr), &cfg); err != nil {
		return nil, err
	}

	if cfg.Endpoint == "" {
		return nil, errors.New("missing endpoint for object storage")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("missing access_key or secret_key for object storage")
	}

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

	client, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, err
	}

	scanner := &S3Scanner{
		client:        client,
		cfg:           cfg,
		allowedBucket: make(map[string]struct{}),
	}

	if len(cfg.Buckets) > 0 {
		for _, b := range cfg.Buckets {
			bucket := strings.TrimSpace(b)
			if bucket != "" && !isReservedBucketName(bucket) {
				scanner.allowedBucket[bucket] = struct{}{}
			}
		}
	}
	if trimmed := strings.TrimSpace(cfg.Bucket); trimmed != "" && !isReservedBucketName(trimmed) {
		scanner.allowedBucket[trimmed] = struct{}{}
	}

	return scanner, nil
}

func (s *S3Scanner) Close() error { return nil }

// SetResourceID 设置资源ID（用于元数据提取）
func (s *S3Scanner) SetResourceID(resID uint) {
	s.resourceID = resID
}

// SetScanDepth 设置扫描深度
func (s *S3Scanner) SetScanDepth(depth string) {
	s.scanDepth = depth
}

func (s *S3Scanner) AllowedBuckets() []string {
	s.ensureBuckets()
	var buckets []string
	for b := range s.allowedBucket {
		buckets = append(buckets, b)
	}
	sort.Strings(buckets)
	return buckets
}

func (s *S3Scanner) ensureBuckets() {
	if len(s.allowedBucket) > 0 {
		return
	}
	ctx := context.Background()
	buckets, err := s.client.ListBuckets(ctx)
	if err != nil {
		return
	}
	for _, bucket := range buckets {
		if !isReservedBucketName(bucket.Name) {
			s.allowedBucket[bucket.Name] = struct{}{}
		}
	}
}

// ListSchemas returns buckets as schemas
func (s *S3Scanner) ListSchemas() ([]SchemaInfo, error) {
	s.ensureBuckets()
	var schemas []SchemaInfo
	for bucket := range s.allowedBucket {
		schemas = append(schemas, SchemaInfo{Name: bucket})
	}
	sort.Slice(schemas, func(i, j int) bool { return schemas[i].Name < schemas[j].Name })
	return schemas, nil
}

// ScanTables not applicable for object storage, return empty
func (s *S3Scanner) ScanTables(schemaName string) ([]TableInfo, error) {
	return []TableInfo{}, nil
}

// ScanFields not applicable
func (s *S3Scanner) ScanFields(schemaName, tableName string) ([]FieldInfo, error) {
	return []FieldInfo{}, nil
}

func (s *S3Scanner) ListNodes(path string) ([]ObjectNode, error) {
	s.ensureBuckets()
	path = strings.TrimSpace(path)
	if path == "" {
		var nodes []ObjectNode
		for bucket := range s.allowedBucket {
			nodes = append(nodes, ObjectNode{
				Name: bucket,
				Path: bucket,
				Type: "bucket",
			})
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		return nodes, nil
	}

	bucket, prefix, err := s.splitPath(path)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	cleanPrefix := prefix
	if cleanPrefix != "" && !strings.HasSuffix(cleanPrefix, "/") {
		cleanPrefix = cleanPrefix + "/"
	}

	objectCh := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    cleanPrefix,
		Recursive: true,
	})

	dirMap := make(map[string]*ObjectNode)
	var objectNodes []ObjectNode

	for object := range objectCh {
		if object.Err != nil {
			continue
		}
		relative := strings.TrimPrefix(object.Key, cleanPrefix)
		relative = strings.TrimPrefix(relative, "/")
		if relative == "" {
			continue
		}
		parts := strings.Split(relative, "/")
		if containsReservedObjectSegment(parts) {
			continue
		}
		name := parts[0]
		fullPath := s.joinPath(bucket, cleanPrefix, name)
		if len(parts) > 1 || strings.HasSuffix(object.Key, "/") {
			if _, exists := dirMap[name]; !exists {
				dirMap[name] = &ObjectNode{
					Name: name,
					Path: fullPath,
					Type: "prefix",
				}
			}
			continue
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		lastModified := object.LastModified
		node := ObjectNode{
			Name:         name,
			Path:         fullPath,
			Type:         "object",
			SizeBytes:    object.Size,
			FileType:     ext,
			LastModified: &lastModified,
		}
		objectNodes = append(objectNodes, node)
	}

	var nodes []ObjectNode
	for _, dir := range dirMap {
		nodes = append(nodes, *dir)
	}
	nodes = append(nodes, objectNodes...)
	nodes = filterReservedNodes(nodes)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

func (s *S3Scanner) ScanPath(path string) ([]ObjectMetadata, error) {
	s.ensureBuckets()
	path = strings.TrimSpace(path)
	if path == "" {
		var meta []ObjectMetadata
		for bucket := range s.allowedBucket {
			bucketMeta, err := s.scanBucket(bucket, "")
			if err != nil {
				return nil, err
			}
			meta = append(meta, bucketMeta...)
		}
		return meta, nil
	}

	bucket, prefix, err := s.splitPath(path)
	if err != nil {
		return nil, err
	}

	if hasReservedObjectSegment(prefix) {
		return []ObjectMetadata{}, nil
	}

	if prefix == "" {
		return s.scanBucket(bucket, "")
	}

	// Determine if it's object or prefix
	ctx := context.Background()
	p := strings.TrimPrefix(prefix, "/")
	stat, err := s.client.StatObject(ctx, bucket, p, minio.StatObjectOptions{})
	if err == nil {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")

		// 尝试提取详细元数据
		var extractedMetadata *Metadata
		if s.resourceID > 0 {
			contentType := inferContentTypeFromExt(ext)
			extractedMetadata = s.extractObjectMetadata(s.resourceID, bucket, p, contentType, stat.Size, stat.LastModified)
		}

		return []ObjectMetadata{
			{
				Bucket:            bucket,
				Path:              bucket + "/" + p,
				RelativePath:      p,
				NodeType:          "object",
				FileType:          ext,
				SizeBytes:         stat.Size,
				ObjectCount:       1,
				LastModified:      &stat.LastModified,
				ExtractedMetadata: extractedMetadata,
			},
		}, nil
	}

	return s.scanBucket(bucket, p)
}

func (s *S3Scanner) scanBucket(bucket, prefix string) ([]ObjectMetadata, error) {
	ctx := context.Background()
	cleanPrefix := strings.TrimPrefix(prefix, "/")
	if cleanPrefix != "" && !strings.HasSuffix(cleanPrefix, "/") {
		cleanPrefix = cleanPrefix + "/"
	}

	totalSize := int64(0)
	var totalCount int64
	objects := []ObjectMetadata{}
	dirAgg := map[string]*ObjectMetadata{}

	// 判断是否为深度扫描
	isDeepScan := strings.EqualFold(s.scanDepth, "deep")

	objectCh := s.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    cleanPrefix,
		Recursive: isDeepScan, // 浅度扫描时不递归
	})

	for object := range objectCh {
		if object.Err != nil {
			continue
		}
		relative := strings.TrimPrefix(object.Key, cleanPrefix)
		relative = strings.TrimPrefix(relative, "/")
		if relative == "" {
			continue
		}
		if hasReservedObjectSegment(relative) {
			continue
		}
		parts := strings.Split(relative, "/")

		// 浅度扫描：只保留当前层级的直接子项（一级子目录和一级文件）
		if !isDeepScan && len(parts) > 1 {
			// 跳过多层级的对象，只处理第一层的目录（prefix）
			continue
		}

		if strings.HasSuffix(object.Key, "/") {
			dirPath := strings.TrimSuffix(relative, "/")
			s.ensureDirAggEntry(dirAgg, bucket, dirPath)
			continue
		}

		totalSize += object.Size
		totalCount++
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(relative)), ".")
		lastModified := object.LastModified

		// 尝试提取详细元数据（视频、图片、文档等）
		// 浅度扫描时，resourceID 已经在外层被设置为 0，这里会自动跳过
		var extractedMetadata *Metadata
		if s.resourceID > 0 && isDeepScan {
			// 推断Content-Type
			contentType := inferContentTypeFromExt(ext)
			extractedMetadata = s.extractObjectMetadata(s.resourceID, bucket, object.Key, contentType, object.Size, lastModified)
		}

		meta := ObjectMetadata{
			Bucket:            bucket,
			Path:              bucket + "/" + object.Key,
			RelativePath:      relative,
			NodeType:          "object",
			FileType:          ext,
			SizeBytes:         object.Size,
			ObjectCount:       1,
			LastModified:      &lastModified,
			ExtractedMetadata: extractedMetadata, // 添加提取的元数据
		}
		objects = append(objects, meta)

		if len(parts) > 1 {
			for i := 1; i < len(parts); i++ {
				dirPath := strings.Join(parts[:i], "/")
				s.ensureDirAggEntry(dirAgg, bucket, dirPath)
				agg := dirAgg[dirPath]
				agg.SizeBytes += object.Size
				agg.ObjectCount++
			}
		}
	}

	results := []ObjectMetadata{}

	bucketMeta := ObjectMetadata{
		Bucket:       bucket,
		Path:         bucket,
		RelativePath: strings.TrimSuffix(cleanPrefix, "/"),
		NodeType:     "bucket",
		SizeBytes:    totalSize,
		ObjectCount:  totalCount,
	}
	if prefix != "" {
		bucketMeta.NodeType = "prefix"
		if cleanPrefix != "" {
			bucketMeta.Path = bucket + "/" + strings.TrimSuffix(cleanPrefix, "/")
			bucketMeta.RelativePath = strings.TrimSuffix(cleanPrefix, "/")
		}
	}
	results = append(results, bucketMeta)

	for _, dir := range dirAgg {
		base := strings.TrimSuffix(cleanPrefix, "/")
		if base != "" {
			dir.Path = bucket + "/" + base + "/" + dir.RelativePath
		} else {
			dir.Path = bucket + "/" + dir.RelativePath
		}
		results = append(results, *dir)
	}

	results = filterReservedMetadata(results)
	sort.Slice(objects, func(i, j int) bool { return objects[i].RelativePath < objects[j].RelativePath })
	results = append(results, objects...)

	return results, nil
}

func (s *S3Scanner) ensureDirAggEntry(dirAgg map[string]*ObjectMetadata, bucket, dirPath string) {
	dirPath = strings.Trim(dirPath, "/")
	if dirPath == "" {
		return
	}
	if _, exists := dirAgg[dirPath]; exists {
		return
	}
	if hasReservedObjectSegment(dirPath) {
		return
	}
	dirAgg[dirPath] = &ObjectMetadata{
		Bucket:       bucket,
		RelativePath: dirPath,
		NodeType:     "prefix",
	}
}

func filterReservedNodes(nodes []ObjectNode) []ObjectNode {
	var filtered []ObjectNode
	for _, node := range nodes {
		if node.Name == "" {
			continue
		}
		if isReservedObjectSegment(node.Name) {
			continue
		}
		if hasReservedObjectSegment(node.Path) {
			continue
		}
		filtered = append(filtered, node)
	}
	return filtered
}

func filterReservedMetadata(metas []ObjectMetadata) []ObjectMetadata {
	var filtered []ObjectMetadata
	for _, meta := range metas {
		if strings.EqualFold(meta.NodeType, "bucket") {
			continue
		}
		if meta.RelativePath != "" && hasReservedObjectSegment(meta.RelativePath) {
			continue
		}
		if hasReservedObjectSegment(meta.Path) {
			continue
		}
		filtered = append(filtered, meta)
	}
	return filtered
}

// HasReservedObjectSegment 暴露给其他包，用于判断路径中是否包含保留段
func HasReservedObjectSegment(path string) bool {
	return hasReservedObjectSegment(path)
}

// IsReservedObjectName 判断名称是否为保留对象名
func IsReservedObjectName(name string) bool {
	return isReservedObjectSegment(name)
}

func (s *S3Scanner) splitPath(path string) (string, string, error) {
	trimmed := strings.Trim(path, " ")
	trimmed = strings.TrimPrefix(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	bucket := parts[0]
	if _, ok := s.allowedBucket[bucket]; !ok {
		return "", "", fmt.Errorf("bucket %s not allowed", bucket)
	}
	if len(parts) == 1 {
		return bucket, "", nil
	}
	return bucket, parts[1], nil
}

func (s *S3Scanner) joinPath(bucket, prefix, name string) string {
	prefix = strings.TrimPrefix(prefix, "/")
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		return bucket + "/" + name
	}
	return bucket + "/" + prefix + "/" + name
}

// extractObjectMetadata 提取对象的详细元数据（视频、图片、文档等）
// resID: 资源ID (system.resources)
// bucket: bucket名称
// objectKey: 对象键
// contentType: MIME类型
// size: 文件大小
// lastModified: 最后修改时间
func (s *S3Scanner) extractObjectMetadata(resID uint, bucket, objectKey, contentType string, size int64, lastModified time.Time) *Metadata {
	// 根据Content-Type获取合适的提取器
	extractor := GetExtractor(contentType)
	if extractor == nil {
		// 没有找到匹配的提取器，尝试根据文件扩展名推断
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(objectKey)), ".")
		contentType = inferContentTypeFromExt(ext)
		extractor = GetExtractor(contentType)
	}

	if extractor == nil {
		// 仍然没有找到提取器，返回nil（不提取详细元数据）
		return nil
	}

	// 获取对象内容（文档类型尝试读取完整内容以便提取）
	ctx := context.Background()
	opts := minio.GetObjectOptions{}
	ext := strings.ToLower(filepath.Ext(objectKey))
	needFull := shouldDownloadFullContent(contentType, ext, size)
	if !needFull {
		opts.SetRange(0, metadataPartialReadSize-1)
	}

	obj, err := s.client.GetObject(ctx, bucket, objectKey, opts)
	if err != nil {
		// 无法读取对象，返回nil
		return nil
	}
	defer obj.Close()

	// 构建ExtractInput
	extractInput := ExtractInput{
		ResourceID:   resID,
		ObjectKey:    objectKey,
		ContentType:  contentType,
		Size:         size,
		Reader:       obj,
		LastModified: lastModified,
	}

	// 调用提取器
	metadata, err := extractor.Extract(ctx, extractInput)
	if err == nil {
		return metadata
	}

	// 如果部分读取失败且文件不大，尝试读取完整内容再次提取
	if !needFull && size > 0 && size <= maxFullDocumentSize {
		_ = obj.Close()
		fullObj, fullErr := s.client.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
		if fullErr != nil {
			return nil
		}
		defer fullObj.Close()

		fullInput := ExtractInput{
			ResourceID:   resID,
			ObjectKey:    objectKey,
			ContentType:  contentType,
			Size:         size,
			Reader:       fullObj,
			LastModified: lastModified,
		}

		if meta, fullExtractErr := extractor.Extract(ctx, fullInput); fullExtractErr == nil {
			return meta
		}
	}

	// 提取失败，返回nil（不影响基本信息的存储）
	return nil
}

// inferContentTypeFromExt 根据文件扩展名推断Content-Type
func inferContentTypeFromExt(ext string) string {
	// 常见的MIME类型映射
	mimeTypes := map[string]string{
		// 视频
		"mp4":  "video/mp4",
		"avi":  "video/x-msvideo",
		"mkv":  "video/x-matroska",
		"mov":  "video/quicktime",
		"wmv":  "video/x-ms-wmv",
		"flv":  "video/x-flv",
		"webm": "video/webm",
		"m4v":  "video/x-m4v",

		// 图片
		"jpg":  "image/jpeg",
		"jpeg": "image/jpeg",
		"png":  "image/png",
		"gif":  "image/gif",
		"bmp":  "image/bmp",
		"webp": "image/webp",
		"tiff": "image/tiff",
		"svg":  "image/svg+xml",

		// 文档
		"pdf":  "application/pdf",
		"doc":  "application/msword",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"xls":  "application/vnd.ms-excel",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"ppt":  "application/vnd.ms-powerpoint",
		"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",

		// 地理空间
		"geojson": "application/geo+json",
		"kml":     "application/vnd.google-earth.kml+xml",
		"kmz":     "application/vnd.google-earth.kmz",
		"shp":     "application/x-shapefile",

		// 其他
		"json": "application/json",
		"xml":  "application/xml",
		"csv":  "text/csv",
		"txt":  "text/plain",
	}

	if mimeType, ok := mimeTypes[ext]; ok {
		return mimeType
	}

	return "application/octet-stream"
}

func shouldDownloadFullContent(contentType, ext string, size int64) bool {
	if size <= 0 || size > maxFullDocumentSize {
		return false
	}

	lowerType := strings.ToLower(contentType)
	switch lowerType {
	case "text/plain", "text/markdown", "text/html", "text/csv":
		return true
	case "application/pdf",
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.ms-powerpoint",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.ms-excel",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return true
	}

	switch strings.ToLower(ext) {
	case ".txt", ".md", ".csv", ".doc", ".docx", ".pdf", ".ppt", ".pptx", ".xls", ".xlsx", ".json", ".xml":
		return true
	}

	return false
}
