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

	commonLogger "github.com/addp/common/logger"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	// ErrObjectStorageNotSupported 指定资源不是对象存储
	ErrObjectStorageNotSupported = errors.New("resource is not object storage")
	// ErrObjectStorageBucketRequired 对象存储连接信息缺少 bucket
	ErrObjectStorageBucketRequired = errors.New("bucket is required")
)

// ObjectStorageDirectory 目录条目
type ObjectStorageDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ObjectStorageBrowseResult 目录浏览结果
type ObjectStorageBrowseResult struct {
	Bucket      string                   `json:"bucket"`
	Prefix      string                   `json:"prefix"`
	Directories []ObjectStorageDirectory `json:"directories"`
}

// ObjectStorageService 提供对象存储的辅助能力
type ObjectStorageService struct {
	localResourceService *LocalResourceService
	logger               *slog.Logger
}

// NewObjectStorageService 构造函数
func NewObjectStorageService(localResourceService *LocalResourceService) *ObjectStorageService {
	return &ObjectStorageService{
		localResourceService: localResourceService,
		logger:               commonLogger.With("component", "object_storage_service"),
	}
}

// ListDirectories 列出指定前缀下的子目录
func (s *ObjectStorageService) ListDirectories(ctx context.Context, tenantID uint, scope string, resourceID uint, prefix string) (*ObjectStorageBrowseResult, error) {
	connInfo, bucket, err := s.resolveConnectionInfo(scope, resourceID, tenantID)
	if err != nil {
		return nil, err
	}

	if bucket == "" {
		return nil, ErrObjectStorageBucketRequired
	}

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

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create object storage client: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

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

func (s *ObjectStorageService) resolveConnectionInfo(scope string, resourceID, tenantID uint) (map[string]interface{}, string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "local":
		resource, err := s.localResourceService.Get(resourceID, tenantID)
		if err != nil {
			return nil, "", err
		}
		if !isObjectStorageType(resource.ResourceType) {
			return nil, "", ErrObjectStorageNotSupported
		}
		return map[string]interface{}(resource.ConnectionInfo), getStringFromConn(map[string]interface{}(resource.ConnectionInfo), "bucket"), nil

	case "system":
		systemRes, err := s.localResourceService.GetSystemResource(resourceID, tenantID)
		if err != nil {
			return nil, "", err
		}
		if !isObjectStorageType(systemRes.ResourceType) {
			return nil, "", ErrObjectStorageNotSupported
		}
		return map[string]interface{}(systemRes.ConnectionInfo), getStringFromConn(map[string]interface{}(systemRes.ConnectionInfo), "bucket"), nil
	default:
		return nil, "", fmt.Errorf("invalid scope: %s", scope)
	}
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
