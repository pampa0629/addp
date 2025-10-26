package service

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"errors"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonLogger "github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	commonUtils "github.com/addp/common/utils"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	_ "github.com/lib/pq" // PostgreSQL driver for connection tests
)

var (
	ErrSystemIntegrationDisabled = errors.New("system integration not available")
	ErrResourceAccessDenied      = errors.New("resource not accessible")
)

// resourceCacheEntry 缓存条目，包含资源和过期时间
type resourceCacheEntry struct {
	resource  *commonModels.Resource
	expiresAt time.Time
}

// LocalResourceService 提供 Transfer 模块的本地存储引擎管理能力
type LocalResourceService struct {
	repo            *repository.LocalResourceRepository
	systemClient    *commonClient.SystemClient
	logger          *slog.Logger
	cfg             *config.Config
	cacheMu         sync.RWMutex
	resourceCache   map[uint]*resourceCacheEntry
	cacheTTL        time.Duration // 缓存生存时间，默认 2 分钟（Transfer 使用较短TTL）
	eventSubscriber *events.ResourceEventSubscriber
}

// NewLocalResourceService 创建 Service
func NewLocalResourceService(db *gorm.DB, cfg *config.Config, redisClient *redis.Client) *LocalResourceService {
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" && cfg.InternalAPIKey != "" {
		systemClient = commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)
	}

	service := &LocalResourceService{
		repo:          repository.NewLocalResourceRepository(db),
		systemClient:  systemClient,
		logger:        commonLogger.With("component", "local_resource_service"),
		cfg:           cfg,
		resourceCache: make(map[uint]*resourceCacheEntry),
		cacheTTL:      2 * time.Minute, // Transfer 使用 2 分钟 TTL
	}

	// 初始化 Redis 事件订阅器
	if redisClient != nil {
		service.eventSubscriber = events.NewResourceEventSubscriber(
			redisClient,
			service.handleResourceChangeEvent,
			service.logger,
		)
		// 启动订阅（在后台 goroutine 中）
		go func() {
			if err := service.eventSubscriber.Start(); err != nil {
				service.logger.Error("资源事件订阅器启动失败", "error", err)
			}
		}()
		service.logger.Info("资源事件订阅器已启动")
	} else {
		service.logger.Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	return service
}

// List 返回指定租户下的本地存储引擎列表
func (s *LocalResourceService) List(tenantID uint, resourceType string) ([]models.LocalResource, error) {
	resources, err := s.repo.List(tenantID, resourceType)
	if err != nil {
		return nil, err
	}
	// 解密所有资源的敏感信息
	for i := range resources {
		if err := s.decryptConnectionInfo(resources[i].ConnectionInfo); err != nil {
			s.logger.Warn("failed to decrypt connection info", "resource_id", resources[i].ID, "error", err)
			// 解密失败不影响其他资源
		}
	}
	return resources, nil
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

// GetSystemResource 获取 System 模块的资源详情（包含解密信息，带缓存）
func (s *LocalResourceService) GetSystemResource(resourceID, tenantID uint) (*commonModels.Resource, error) {
	if s.systemClient == nil {
		s.logger.Warn("system client unavailable when fetching system resource", "resource_id", resourceID)
		return nil, ErrSystemIntegrationDisabled
	}

	// 检查缓存
	s.cacheMu.RLock()
	entry, ok := s.resourceCache[resourceID]
	s.cacheMu.RUnlock()

	// 如果缓存命中且未过期，返回缓存数据
	if ok && entry != nil && entry.resource != nil && time.Now().Before(entry.expiresAt) {
		if tenantID == 0 || entry.resource.TenantID == tenantID {
			resourceCopy := *entry.resource
			s.logger.Debug("资源连接信息命中缓存",
				"resource_id", resourceID,
				"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
			)
			return &resourceCopy, nil
		}
	}

	// 缓存未命中或已过期，从 System API 获取
	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		return nil, err
	}

	if resource.TenantID != 0 && resource.TenantID != tenantID {
		return nil, ErrResourceAccessDenied
	}

	// 更新缓存
	s.cacheResource(resource)
	s.logger.Info("通过内部 API 获取资源连接信息成功",
		"resource_id", resourceID,
		"resource_type", resource.ResourceType,
	)

	return resource, nil
}

// Get 获取指定资源
func (s *LocalResourceService) Get(id, tenantID uint) (*models.LocalResource, error) {
	resource, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	// 解密敏感信息
	if err := s.decryptConnectionInfo(resource.ConnectionInfo); err != nil {
		s.logger.Warn("failed to decrypt connection info", "resource_id", id, "error", err)
		// 解密失败不返回错误，但记录日志
	}
	return resource, nil
}

// Create 创建资源
func (s *LocalResourceService) Create(resource *models.LocalResource) error {
	// 加密敏感信息
	if err := s.encryptConnectionInfo(resource.ConnectionInfo); err != nil {
		return fmt.Errorf("failed to encrypt connection info: %w", err)
	}
	return s.repo.Create(resource)
}

// Update 更新资源
func (s *LocalResourceService) Update(resource *models.LocalResource) error {
	// 加密敏感信息
	if err := s.encryptConnectionInfo(resource.ConnectionInfo); err != nil {
		return fmt.Errorf("failed to encrypt connection info: %w", err)
	}
	return s.repo.Update(resource)
}

// Delete 删除资源
func (s *LocalResourceService) Delete(id, tenantID uint) error {
	return s.repo.Delete(id, tenantID)
}

// TestConnectionBeforeCreate 测试尚未入库的配置（未加密的明文配置）
func (s *LocalResourceService) TestConnectionBeforeCreate(resourceType string, connInfo models.JSONMap) error {
	// 这里传入的是前端的明文配置，直接测试即可
	return s.testConnection(resourceType, connInfo)
}

// TestConnection 测试已存在资源的连接
func (s *LocalResourceService) TestConnection(id, tenantID uint) error {
	resource, err := s.Get(id, tenantID)
	if err != nil {
		return err
	}
	// Get 方法已经解密了 connection_info，可以直接使用
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

// encryptConnectionInfo 加密连接信息中的敏感字段
func (s *LocalResourceService) encryptConnectionInfo(connInfo models.JSONMap) error {
	if len(s.cfg.EncryptionKey) != 32 {
		return fmt.Errorf("encryption key must be 32 bytes, got %d", len(s.cfg.EncryptionKey))
	}

	// 加密 password 字段（PostgreSQL/MySQL）
	if password, ok := connInfo["password"].(string); ok && password != "" {
		encrypted, err := commonUtils.Encrypt(password, s.cfg.EncryptionKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
		connInfo["password"] = encrypted
	}

	// 加密 secret_key 字段（MinIO/S3）
	if secretKey, ok := connInfo["secret_key"].(string); ok && secretKey != "" {
		encrypted, err := commonUtils.Encrypt(secretKey, s.cfg.EncryptionKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt secret_key: %w", err)
		}
		connInfo["secret_key"] = encrypted
	}

	return nil
}

// decryptConnectionInfo 解密连接信息中的敏感字段
func (s *LocalResourceService) decryptConnectionInfo(connInfo models.JSONMap) error {
	if len(s.cfg.EncryptionKey) != 32 {
		return fmt.Errorf("decryption key must be 32 bytes, got %d", len(s.cfg.EncryptionKey))
	}

	// 解密 password 字段（PostgreSQL/MySQL）
	if encryptedPassword, ok := connInfo["password"].(string); ok && encryptedPassword != "" {
		decrypted, err := commonUtils.Decrypt(encryptedPassword, s.cfg.EncryptionKey)
		if err != nil {
			// 如果解密失败，可能是明文密码（向后兼容）
			s.logger.Warn("failed to decrypt password, might be plaintext", "error", err)
			return err
		}
		connInfo["password"] = decrypted
	}

	// 解密 secret_key 字段（MinIO/S3）
	if encryptedSecretKey, ok := connInfo["secret_key"].(string); ok && encryptedSecretKey != "" {
		decrypted, err := commonUtils.Decrypt(encryptedSecretKey, s.cfg.EncryptionKey)
		if err != nil {
			// 如果解密失败，可能是明文密钥（向后兼容）
			s.logger.Warn("failed to decrypt secret_key, might be plaintext", "error", err)
			return err
		}
		connInfo["secret_key"] = decrypted
	}

	return nil
}

// cacheResource 缓存资源
func (s *LocalResourceService) cacheResource(resource *commonModels.Resource) {
	if resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.resourceCache[resourceCopy.ID] = &resourceCacheEntry{
		resource:  &resourceCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// ClearCache 清除所有资源缓存
func (s *LocalResourceService) ClearCache() {
	s.cacheMu.Lock()
	s.resourceCache = make(map[uint]*resourceCacheEntry)
	s.cacheMu.Unlock()
	s.logger.Info("资源缓存已清除")
}

// ClearResourceCache 清除指定资源的缓存
func (s *LocalResourceService) ClearResourceCache(resourceID uint) {
	s.cacheMu.Lock()
	delete(s.resourceCache, resourceID)
	s.cacheMu.Unlock()
	s.logger.Info("资源缓存已清除", "resource_id", resourceID)
}

// handleResourceChangeEvent 处理资源变更事件（Redis 订阅回调）
func (s *LocalResourceService) handleResourceChangeEvent(event events.ResourceChangeEvent) error {
	s.logger.Info("收到资源变更事件",
		"resource_id", event.ResourceID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate:
		// 资源创建：不需要特殊处理，等待下次访问时自动加载
		s.logger.Debug("资源已创建，等待首次访问时加载", "resource_id", event.ResourceID)

	case events.ActionUpdate:
		// 资源更新：清除缓存，强制下次访问时重新获取
		s.ClearResourceCache(event.ResourceID)
		s.logger.Info("资源已更新，缓存已清除", "resource_id", event.ResourceID)

	case events.ActionDelete:
		// 资源删除：清除缓存
		s.ClearResourceCache(event.ResourceID)
		s.logger.Info("资源已删除，缓存已清除", "resource_id", event.ResourceID)

	default:
		s.logger.Warn("未知的资源变更动作", "action", event.Action, "resource_id", event.ResourceID)
	}

	return nil
}

// Stop 停止资源服务（清理资源）
func (s *LocalResourceService) Stop() {
	if s.eventSubscriber != nil {
		s.eventSubscriber.Stop()
		s.logger.Info("资源事件订阅器已停止")
	}
}
