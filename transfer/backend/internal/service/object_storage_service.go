package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	commonLogger "github.com/addp/common/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	// ErrSystemIntegrationDisabled 表示 Transfer 未配置 System 集成。
	ErrSystemIntegrationDisabled = errors.New("system integration not available")
	// ErrEngineAccessDenied 表示当前租户不能访问指定引擎。
	ErrEngineAccessDenied = errors.New("engine not accessible")
	// ErrObjectStorageNotSupported 指定资源不是对象存储
	ErrObjectStorageNotSupported = errors.New("resource is not object storage")
)

// ObjectStorageDirectory 目录条目
type ObjectStorageDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ObjectStorageFile 文件条目
type ObjectStorageFile struct {
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// ObjectStorageBrowseResult 目录浏览结果
type ObjectStorageBrowseResult struct {
	Bucket      string                   `json:"bucket"`
	Prefix      string                   `json:"prefix"`
	Directories []ObjectStorageDirectory `json:"directories"`
}

// ObjectStorageListFilesResult 文件列表结果
type ObjectStorageListFilesResult struct {
	Bucket string              `json:"bucket"`
	Prefix string              `json:"prefix"`
	Files  []ObjectStorageFile `json:"files"`
}

// ObjectStorageService 提供对象存储的辅助能力
type ObjectStorageService struct {
	systemClient *commonClient.SystemClient
	logger       *slog.Logger
}

// NewObjectStorageService 构造函数
func NewObjectStorageService(systemClient *commonClient.SystemClient) *ObjectStorageService {
	return &ObjectStorageService{
		systemClient: systemClient,
		logger:       commonLogger.With("component", "object_storage_service"),
	}
}

// ListDirectories 列出指定前缀下的子目录
// 当引擎未配置 bucket 时，根目录列出所有 bucket；prefix 中第一段为 bucket 名
func (s *ObjectStorageService) ListDirectories(ctx context.Context, tenantID uint, engineID uint, prefix string) (*ObjectStorageBrowseResult, error) {
	connInfo, bucket, err := s.resolveConnectionInfo(engineID, tenantID)
	if err != nil {
		return nil, err
	}

	client, err := buildMinIOClient(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create object storage client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if bucket == "" {
		return s.listDirectoriesNoBucket(ctx, client, prefix)
	}
	return s.listDirectoriesInBucket(ctx, client, bucket, prefix)
}

// listDirectoriesNoBucket 处理未配置 bucket 的情况
// prefix 为空时列出所有 bucket；非空时从 prefix 第一段提取 bucket
func (s *ObjectStorageService) listDirectoriesNoBucket(ctx context.Context, client *minio.Client, prefix string) (*ObjectStorageBrowseResult, error) {
	sanitizedPrefix := sanitizePrefix(prefix)

	if sanitizedPrefix == "" {
		buckets, err := client.ListBuckets(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list buckets: %w", err)
		}
		directories := make([]ObjectStorageDirectory, 0, len(buckets))
		for _, b := range buckets {
			directories = append(directories, ObjectStorageDirectory{
				Name: b.Name,
				Path: b.Name + "/",
			})
		}
		return &ObjectStorageBrowseResult{
			Bucket:      "",
			Prefix:      "",
			Directories: directories,
		}, nil
	}

	// 从 prefix 第一段提取 bucket
	parts := strings.SplitN(sanitizedPrefix, "/", 2)
	bucketName := parts[0]
	subPrefix := ""
	if len(parts) > 1 {
		subPrefix = parts[1]
	}

	result, err := s.listDirectoriesInBucket(ctx, client, bucketName, subPrefix)
	if err != nil {
		return nil, err
	}
	// 将 bucket 名前缀加回到所有路径，保持 prefix 一致性
	for i := range result.Directories {
		result.Directories[i].Path = bucketName + "/" + result.Directories[i].Path
	}
	result.Prefix = sanitizedPrefix
	return result, nil
}

// listDirectoriesInBucket 列出指定 bucket 内的子目录
func (s *ObjectStorageService) listDirectoriesInBucket(ctx context.Context, client *minio.Client, bucket, prefix string) (*ObjectStorageBrowseResult, error) {
	sanitizedPrefix := sanitizePrefix(prefix)
	opts := minio.ListObjectsOptions{
		Prefix:    sanitizedPrefix,
		Recursive: false,
	}

	objectCh := client.ListObjects(ctx, bucket, opts)
	dirSet := make(map[string]struct{})

	for object := range objectCh {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}

		if !strings.HasSuffix(object.Key, "/") {
			continue
		}

		childPath := immediateChildPath(sanitizedPrefix, object.Key)
		if childPath == "" {
			continue
		}

		dirSet[childPath] = struct{}{}

		if len(dirSet) >= 2000 {
			break
		}
	}

	directories := make([]ObjectStorageDirectory, 0, len(dirSet))
	for path := range dirSet {
		name := directoryNameFromPath(path, sanitizedPrefix)
		if name == "" {
			continue
		}
		directories = append(directories, ObjectStorageDirectory{
			Name: name,
			Path: path,
		})
	}

	sort.Slice(directories, func(i, j int) bool {
		return strings.ToLower(directories[i].Name) < strings.ToLower(directories[j].Name)
	})

	return &ObjectStorageBrowseResult{
		Bucket:      bucket,
		Prefix:      sanitizedPrefix,
		Directories: directories,
	}, nil
}

// ListFiles 列出指定前缀下的文件（不包括子目录）
// 当引擎未配置 bucket 时，从 prefix 第一段提取 bucket 名
func (s *ObjectStorageService) ListFiles(ctx context.Context, tenantID uint, engineID uint, prefix string) (*ObjectStorageListFilesResult, error) {
	connInfo, bucket, err := s.resolveConnectionInfo(engineID, tenantID)
	if err != nil {
		return nil, err
	}

	client, err := buildMinIOClient(connInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to create object storage client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	sanitizedPrefix := sanitizePrefix(prefix)

	// 当引擎未配置 bucket 时，从 prefix 第一段提取
	if bucket == "" {
		if sanitizedPrefix == "" {
			return &ObjectStorageListFilesResult{Bucket: "", Prefix: "", Files: []ObjectStorageFile{}}, nil
		}
		parts := strings.SplitN(sanitizedPrefix, "/", 2)
		bucket = parts[0]
		if len(parts) > 1 {
			sanitizedPrefix = parts[1]
		} else {
			sanitizedPrefix = ""
		}
	}

	opts := minio.ListObjectsOptions{
		Prefix:    sanitizedPrefix,
		Recursive: false,
	}

	objectCh := client.ListObjects(ctx, bucket, opts)
	files := make([]ObjectStorageFile, 0)

	for object := range objectCh {
		if object.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", object.Err)
		}

		if strings.HasSuffix(object.Key, "/") {
			continue
		}

		relativePath := strings.TrimPrefix(object.Key, sanitizedPrefix)
		if strings.Contains(relativePath, "/") {
			continue
		}

		files = append(files, ObjectStorageFile{
			Name:         object.Key,
			Size:         object.Size,
			LastModified: object.LastModified,
		})

		if len(files) >= 1000 {
			break
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})

	return &ObjectStorageListFilesResult{
		Bucket: bucket,
		Prefix: sanitizedPrefix,
		Files:  files,
	}, nil
}

// buildMinIOClient 从连接信息创建 MinIO 客户端
func buildMinIOClient(connInfo map[string]interface{}) (*minio.Client, error) {
	endpoint := getStringFromConn(connInfo, "endpoint")
	accessKey := getStringFromConn(connInfo, "access_key")
	secretKey := getStringFromConn(connInfo, "secret_key")
	useSSL := getBoolFromConn(connInfo, "use_ssl")

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("incomplete object storage connection info")
	}

	endpoint, schemeSSL := stripEndpointScheme(endpoint)
	if schemeSSL {
		useSSL = true
	}

	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
}

func (s *ObjectStorageService) resolveConnectionInfo(engineID, tenantID uint) (map[string]interface{}, string, error) {
	if s.systemClient == nil {
		return nil, "", ErrSystemIntegrationDisabled
	}
	systemRes, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, "", err
	}
	if systemRes.TenantID != nil && *systemRes.TenantID != 0 && *systemRes.TenantID != tenantID {
		return nil, "", ErrEngineAccessDenied
	}
	if !isObjectStorageType(systemRes.EngineType) {
		return nil, "", ErrObjectStorageNotSupported
	}
	return map[string]interface{}(systemRes.ConnectionInfo), getStringFromConn(map[string]interface{}(systemRes.ConnectionInfo), "bucket"), nil
}

func isObjectStorageType(resourceType string) bool {
	typeLower := strings.ToLower(strings.TrimSpace(resourceType))
	switch typeLower {
	case "s3", "minio", "oss", "object_storage", "object-storage":
		return true
	default:
		return false
	}
}

func sanitizePrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed != "" && !strings.HasSuffix(trimmed, "/") {
		trimmed += "/"
	}
	return trimmed
}

func immediateChildPath(parentPrefix, key string) string {
	if parentPrefix != "" && !strings.HasSuffix(parentPrefix, "/") {
		parentPrefix += "/"
	}
	relative := strings.TrimPrefix(key, parentPrefix)
	relative = strings.TrimPrefix(relative, "/")
	relative = strings.TrimSuffix(relative, "/")
	if relative == "" {
		return ""
	}
	parts := strings.Split(relative, "/")
	child := parts[0]
	if child == "" {
		return ""
	}
	if parentPrefix == "" {
		return child + "/"
	}
	return parentPrefix + child + "/"
}

func directoryNameFromPath(path, parentPrefix string) string {
	if parentPrefix != "" && !strings.HasSuffix(parentPrefix, "/") {
		parentPrefix += "/"
	}
	trimmed := strings.TrimSuffix(path, "/")
	trimmed = strings.TrimPrefix(trimmed, parentPrefix)
	trimmed = strings.TrimSuffix(trimmed, "/")
	return trimmed
}

func getStringFromConn(conn map[string]interface{}, key string) string {
	if conn == nil {
		return ""
	}
	val, ok := conn[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		return strings.TrimSpace(strconv.FormatFloat(v, 'f', -1, 64))
	case float32:
		return strings.TrimSpace(strconv.FormatFloat(float64(v), 'f', -1, 32))
	case int:
		return strconv.Itoa(v)
	case int8:
		return strconv.Itoa(int(v))
	case int16:
		return strconv.Itoa(int(v))
	case int32:
		return strconv.Itoa(int(v))
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func getBoolFromConn(conn map[string]interface{}, key string) bool {
	if conn == nil {
		return false
	}
	val, ok := conn[key]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "1" || lower == "yes"
	case json.Number:
		parsed, err := strconv.ParseFloat(v.String(), 64)
		return err == nil && parsed != 0
	case float64:
		return v != 0
	case float32:
		return v != 0
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint8:
		return v != 0
	case uint16:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	default:
		return false
	}
}

func stripEndpointScheme(endpoint string) (string, bool) {
	ep := strings.TrimSpace(endpoint)
	switch {
	case strings.HasPrefix(strings.ToLower(ep), "https://"):
		return strings.TrimSuffix(ep[8:], "/"), true
	case strings.HasPrefix(strings.ToLower(ep), "http://"):
		return strings.TrimSuffix(ep[7:], "/"), false
	default:
		return strings.TrimSuffix(ep, "/"), false
	}
}
