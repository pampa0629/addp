package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/lib/pq"
)

type ApplicationService struct {
	repo *repository.ApplicationRepository
}

func NewApplicationService(repo *repository.ApplicationRepository) *ApplicationService {
	return &ApplicationService{repo: repo}
}

// CreateApplication 创建应用
func (s *ApplicationService) CreateApplication(req *models.CreateApplicationRequest, tenantID, userID uint) (*models.Application, error) {
	// 如果未指定限流配置,使用默认值
	if req.RateLimitPerMinute == 0 {
		req.RateLimitPerMinute = 60
	}

	app := &models.Application{
		Name:               req.Name,
		Description:        req.Description,
		TenantID:           tenantID,
		AllowedServices:    pq.StringArray(req.AllowedServices),
		RateLimitPerMinute: req.RateLimitPerMinute,
		Status:             "active",
		CreatedBy:          &userID,
	}

	if err := s.repo.Create(app); err != nil {
		return nil, fmt.Errorf("failed to create application: %w", err)
	}

	return app, nil
}

// GetApplication 获取应用详情
func (s *ApplicationService) GetApplication(id uint) (*models.Application, error) {
	app, err := s.repo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}
	return app, nil
}

// ListApplications 列出租户的应用
func (s *ApplicationService) ListApplications(tenantID uint) ([]models.Application, error) {
	return s.repo.FindByTenantID(tenantID)
}

// UpdateApplication 更新应用
func (s *ApplicationService) UpdateApplication(id uint, req *models.UpdateApplicationRequest) (*models.Application, error) {
	app, err := s.repo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}

	// 更新字段
	if req.Name != "" {
		app.Name = req.Name
	}
	if req.Description != "" {
		app.Description = req.Description
	}
	if req.AllowedServices != nil {
		app.AllowedServices = pq.StringArray(req.AllowedServices)
	}
	if req.RateLimitPerMinute > 0 {
		app.RateLimitPerMinute = req.RateLimitPerMinute
	}
	if req.Status != "" {
		app.Status = req.Status
	}

	if err := s.repo.Update(app); err != nil {
		return nil, fmt.Errorf("failed to update application: %w", err)
	}

	return app, nil
}

// DeleteApplication 删除应用
func (s *ApplicationService) DeleteApplication(id uint) error {
	return s.repo.Delete(id)
}

// GenerateAPIKey 为应用生成 API Key
func (s *ApplicationService) GenerateAPIKey(appID uint, req *models.CreateAPIKeyRequest, userID uint) (*models.APIKey, error) {
	// 验证应用是否存在
	app, err := s.repo.FindByID(appID)
	if err != nil {
		return nil, fmt.Errorf("application not found: %w", err)
	}

	if app.Status != "active" {
		return nil, errors.New("application is not active")
	}

	// 生成随机 API Key (32 字节 = 256 位)
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random key: %w", err)
	}

	// Base64 编码生成可读的 Key
	plainKey := base64.RawURLEncoding.EncodeToString(randomBytes)

	// 添加前缀
	fullKey := fmt.Sprintf("addp_live_%s", plainKey)

	// 计算 SHA256 hash
	hash := sha256.Sum256([]byte(fullKey))
	keyHash := hex.EncodeToString(hash[:])

	// 创建 API Key 记录
	apiKey := &models.APIKey{
		ApplicationID: appID,
		KeyPrefix:     "addp_live_",
		KeyHash:       keyHash,
		Name:          req.Name,
		ExpiresAt:     req.ExpiresAt,
		Status:        "active",
		CreatedBy:     &userID,
		PlainTextKey:  fullKey, // 临时保存明文 Key（仅返回一次）
	}

	if err := s.repo.CreateAPIKey(apiKey); err != nil {
		return nil, fmt.Errorf("failed to create API key: %w", err)
	}

	return apiKey, nil
}

// ListAPIKeys 列出应用的 API Keys
func (s *ApplicationService) ListAPIKeys(appID uint) ([]models.APIKey, error) {
	return s.repo.FindAPIKeysByAppID(appID)
}

// RevokeAPIKey 撤销 API Key
func (s *ApplicationService) RevokeAPIKey(appID, keyID, userID uint) error {
	// 验证 Key 是否属于该应用
	keys, err := s.repo.FindAPIKeysByAppID(appID)
	if err != nil {
		return err
	}

	found := false
	for _, key := range keys {
		if key.ID == keyID {
			found = true
			break
		}
	}

	if !found {
		return errors.New("API key not found or does not belong to this application")
	}

	return s.repo.RevokeAPIKey(keyID, userID)
}

// ValidateAPIKey 验证 API Key（供 Gateway 调用）
func (s *ApplicationService) ValidateAPIKey(keyHash string) (*models.APIKeyValidationResponse, error) {
	apiKey, app, err := s.repo.FindAPIKeyWithApp(keyHash)
	if err != nil {
		return &models.APIKeyValidationResponse{Valid: false}, nil
	}

	// 检查应用状态
	if app.Status != "active" {
		return &models.APIKeyValidationResponse{Valid: false}, nil
	}

	// 检查 Key 是否过期
	if apiKey.ExpiresAt != nil && apiKey.ExpiresAt.Before(time.Now()) {
		return &models.APIKeyValidationResponse{Valid: false}, nil
	}

	// 更新最后使用时间（异步更新,不影响验证性能）
	go s.repo.UpdateAPIKeyLastUsed(apiKey.ID)

	return &models.APIKeyValidationResponse{
		Valid:              true,
		AppID:              app.ID,
		AppName:            app.Name,
		AllowedServices:    app.AllowedServices,
		RateLimitPerMinute: app.RateLimitPerMinute,
		ExpiresAt:          apiKey.ExpiresAt,
	}, nil
}
