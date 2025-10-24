package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"errors"

	commonClient "github.com/addp/common/client"
	commonLogger "github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"

	_ "github.com/lib/pq" // PostgreSQL driver for connection tests
)

var ErrSystemIntegrationDisabled = errors.New("system integration not available")

// LocalResourceService 提供 Transfer 模块的本地存储引擎管理能力
type LocalResourceService struct {
	repo         *repository.LocalResourceRepository
	systemClient *commonClient.SystemClient
	logger       *slog.Logger
	cfg          *config.Config
}

// NewLocalResourceService 创建 Service
func NewLocalResourceService(db *gorm.DB, cfg *config.Config) *LocalResourceService {
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	return &LocalResourceService{
		repo:         repository.NewLocalResourceRepository(db),
		systemClient: systemClient,
		logger:       commonLogger.With("component", "local_resource_service"),
		cfg:          cfg,
	}
}

// List 返回指定租户下的本地存储引擎列表
func (s *LocalResourceService) List(tenantID uint, resourceType string) ([]models.LocalResource, error) {
	return s.repo.List(tenantID, resourceType)
}

// ListSystemResources 获取 System 模块的存储引擎（需启用集成）
func (s *LocalResourceService) ListSystemResources(resourceType string, tenantID uint) ([]commonModels.Resource, error) {
	if s.systemClient == nil {
		s.logger.Warn("system client unavailable when listing system resources")
		return nil, ErrSystemIntegrationDisabled
	}

	resources, err := s.systemClient.ListResources(resourceType, tenantID)
	if err != nil {
		return nil, err
	}

	filtered := make([]commonModels.Resource, 0, len(resources))
	for _, res := range resources {
		if res.IsActive {
			filtered = append(filtered, res)
		}
	}

	return filtered, nil
}

// Get 获取指定资源
func (s *LocalResourceService) Get(id, tenantID uint) (*models.LocalResource, error) {
	return s.repo.GetByID(id, tenantID)
}

// Create 创建资源
func (s *LocalResourceService) Create(resource *models.LocalResource) error {
	return s.repo.Create(resource)
}

// Update 更新资源
func (s *LocalResourceService) Update(resource *models.LocalResource) error {
	return s.repo.Update(resource)
}

// Delete 删除资源
func (s *LocalResourceService) Delete(id, tenantID uint) error {
	return s.repo.Delete(id, tenantID)
}

// TestConnectionBeforeCreate 测试尚未入库的配置
func (s *LocalResourceService) TestConnectionBeforeCreate(resourceType string, connInfo models.JSONMap) error {
	return s.testConnection(resourceType, connInfo)
}

// TestConnection 测试已存在资源的连接
func (s *LocalResourceService) TestConnection(id, tenantID uint) error {
	resource, err := s.Get(id, tenantID)
	if err != nil {
		return err
	}
	return s.testConnection(resource.ResourceType, resource.ConnectionInfo)
}

// SyncToSystem 将本地配置推送到 System 模块，返回在 System 中创建的资源
func (s *LocalResourceService) SyncToSystem(resource *models.LocalResource) (*commonModels.Resource, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system integration not available")
	}

	payload := map[string]interface{}{
		"name":            resource.Name,
		"resource_type":   resource.ResourceType,
		"description":     resource.Description,
		"connection_info": resource.ConnectionInfo,
	}

	if resource.TenantID != 0 {
		payload["tenant_id"] = resource.TenantID
	}
	if resource.CreatedBy != nil {
		payload["created_by"] = *resource.CreatedBy
	}

	resp, err := s.systemClient.CreateResource(payload)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *LocalResourceService) testConnection(resourceType string, connInfo models.JSONMap) error {
	switch resourceType {
	case "postgresql":
		return s.testPostgreSQL(connInfo)
	case "minio", "s3":
		return s.testObjectStorage(connInfo)
	default:
		return fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

func (s *LocalResourceService) normalizeHost(host string) string {
	if host == "localhost" || host == "127.0.0.1" {
		if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
			return alias
		}
		return "127.0.0.1"
	}
	return host
}

func (s *LocalResourceService) testPostgreSQL(connInfo models.JSONMap) error {
	host, _ := connInfo["host"].(string)
	host = s.normalizeHost(host)

	var port int
	switch v := connInfo["port"].(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			port = parsed
		}
	}
	if port == 0 {
		port = 5432
	}

	database, _ := connInfo["database"].(string)
	user, _ := connInfo["user"].(string)
	password, _ := connInfo["password"].(string)
	sslMode, _ := connInfo["sslmode"].(string)
	if sslMode == "" {
		sslMode = "disable"
	}

	if host == "" || user == "" || database == "" {
		return fmt.Errorf("missing required fields: host, user, database")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, database, sslMode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

func (s *LocalResourceService) testObjectStorage(connInfo models.JSONMap) error {
	endpoint, _ := connInfo["endpoint"].(string)
	if endpoint == "" {
		return fmt.Errorf("missing required field: endpoint")
	}
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	if accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: access_key, secret_key")
	}

	useSSL := false
	if val, ok := connInfo["use_ssl"].(bool); ok {
		useSSL = val
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := client.ListBuckets(ctx); err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	return nil
}
