package objectstore

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	commonModels "github.com/addp/common/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

type ClientManager struct {
	db      *gorm.DB
	clients map[uint]*minio.Client
	mu      sync.Mutex
}

func NewClientManager(db *gorm.DB) *ClientManager {
	return &ClientManager{
		db:      db,
		clients: make(map[uint]*minio.Client),
	}
}

func (m *ClientManager) GetByResource(resource *commonModels.Engine) (*minio.Client, error) {
	if resource == nil {
		return nil, fmt.Errorf("object storage resource is nil")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[resource.ID]; ok {
		return client, nil
	}

	client, err := NewMinIOClient(resource.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	m.clients[resource.ID] = client
	return client, nil
}

func (m *ClientManager) Get(engineID, tenantID uint) (*minio.Client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, ok := m.clients[engineID]; ok {
		return client, nil
	}

	var resource commonModels.Engine
	if err := m.db.Where("id = ? AND tenant_id = ?", engineID, tenantID).First(&resource).Error; err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	client, err := NewMinIOClient(resource.ConnectionInfo)
	if err != nil {
		return nil, err
	}
	m.clients[engineID] = client
	return client, nil
}

func (m *ClientManager) FetchObjectContent(
	ctx context.Context,
	engineID, tenantID uint,
	bucket, objectPath string,
	maxSize int64,
) ([]byte, string, error) {
	client, err := m.Get(engineID, tenantID)
	if err != nil {
		return nil, "", err
	}

	obj, err := client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to get object: %w", err)
	}
	defer obj.Close()

	var content []byte
	if maxSize > 0 {
		content = make([]byte, maxSize)
		n, err := io.ReadFull(obj, content)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return nil, "", fmt.Errorf("failed to read object: %w", err)
		}
		content = content[:n]
	} else {
		content, err = io.ReadAll(obj)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read object: %w", err)
		}
	}

	return content, DetectMimeType(objectPath, content), nil
}

func NewMinIOClient(connInfo commonModels.ConnectionInfo) (*minio.Client, error) {
	cfg, err := ParseConfig(connInfo)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}
	return client, nil
}

func DetectMimeType(pathValue string, content []byte) string {
	ext := path.Ext(pathValue)
	if ext != "" {
		return "application/octet-stream"
	}
	return "application/octet-stream"
}

type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	PathStyle bool
}

func ParseConfig(info commonModels.ConnectionInfo) (*Config, error) {
	cfg := &Config{}

	cfg.Endpoint = stringFromConn(info, "endpoint")
	cfg.AccessKey = stringFromConn(info, "access_key")
	cfg.SecretKey = stringFromConn(info, "secret_key")
	cfg.Region = stringFromConn(info, "region")
	cfg.UseSSL = boolFromConn(info, "use_ssl")
	cfg.PathStyle = boolFromConn(info, "path_style")

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("object storage endpoint is empty")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("object storage credentials missing")
	}

	return cfg, nil
}

func stringFromConn(info commonModels.ConnectionInfo, key string) string {
	if raw, ok := info[key]; ok {
		switch v := raw.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case float64:
			return fmt.Sprintf("%.0f", v)
		case int64:
			return fmt.Sprintf("%d", v)
		case int:
			return fmt.Sprintf("%d", v)
		case bool:
			if v {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func boolFromConn(info commonModels.ConnectionInfo, key string) bool {
	if raw, ok := info[key]; ok {
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			lower := strings.ToLower(strings.TrimSpace(v))
			return lower == "true" || lower == "1" || lower == "yes"
		case float64:
			return v != 0
		case int:
			return v != 0
		case int64:
			return v != 0
		}
	}
	return false
}
