package service

import (
	"fmt"
	"time"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

type AuthorizationService struct {
	db *gorm.DB
}

func NewAuthorizationService(db *gorm.DB) *AuthorizationService {
	return &AuthorizationService{db: db}
}

// ============================================================
// 请求/响应数据结构
// ============================================================

// AuthorizationListParams 授权列表查询参数
type AuthorizationListParams struct {
	Page     int
	PageSize int
	UserID   int64
	AssetID  int64
	Status   string
}

// AuthorizationWithAsset 带资产名称的授权记录（列表展示用）
type AuthorizationWithAsset struct {
	models.Authorization
	AssetName     string `json:"asset_name"`
	AssetTypeCode string `json:"asset_type_code"`
}

// ============================================================
// 业务方法
// ============================================================

// Create 创建授权记录（内部方法，由 ApplicationService.Approve 在事务中调用）
func (s *AuthorizationService) Create(tx *gorm.DB, tenantID int64, userID int64, assetID int64, applicationID *int64, expiresAt *time.Time) (*models.Authorization, error) {
	db := tx
	if db == nil {
		db = s.db
	}
	auth := &models.Authorization{
		TenantID:      tenantID,
		AssetID:       assetID,
		ApplicationID: applicationID,
		UserID:        userID,
		ExpiresAt:     expiresAt,
		Status:        models.AuthorizationStatusPending,
	}
	if err := db.Create(auth).Error; err != nil {
		return nil, err
	}
	return auth, nil
}

// List 授权列表（管理员视角）
func (s *AuthorizationService) List(tenantID uint, params AuthorizationListParams) ([]AuthorizationWithAsset, int64, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}

	query := s.db.Table("asset.authorizations auth").
		Select("auth.*, a.name AS asset_name, td.code AS asset_type_code").
		Joins("LEFT JOIN asset.assets a ON a.id = auth.asset_id").
		Joins("LEFT JOIN asset.type_definitions td ON td.id = a.type_id").
		Where("auth.tenant_id = ?", tenantID)

	if params.UserID > 0 {
		query = query.Where("auth.user_id = ?", params.UserID)
	}
	if params.AssetID > 0 {
		query = query.Where("auth.asset_id = ?", params.AssetID)
	}
	if params.Status != "" {
		query = query.Where("auth.status = ?", params.Status)
	}

	var total int64
	query.Count(&total)

	var results []AuthorizationWithAsset
	err := query.
		Order("auth.created_at DESC").
		Offset((params.Page - 1) * params.PageSize).
		Limit(params.PageSize).
		Scan(&results).Error

	return results, total, err
}

// Get 获取单条授权记录
func (s *AuthorizationService) Get(tenantID uint, id int64) (*AuthorizationWithAsset, error) {
	var result AuthorizationWithAsset
	err := s.db.Table("asset.authorizations auth").
		Select("auth.*, a.name AS asset_name, td.code AS asset_type_code").
		Joins("LEFT JOIN asset.assets a ON a.id = auth.asset_id").
		Joins("LEFT JOIN asset.type_definitions td ON td.id = a.type_id").
		Where("auth.id = ? AND auth.tenant_id = ?", id, tenantID).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if result.ID == 0 {
		return nil, fmt.Errorf("授权记录不存在")
	}
	return &result, nil
}

// ExpireOverdue moves expired grants into revocation fulfillment.
func (s *AuthorizationService) ExpireOverdue() (int64, error) {
	now := time.Now().UTC()
	result := s.db.Model(&models.Authorization{}).
		Where("status IN ? AND expires_at IS NOT NULL AND expires_at <= ?",
			[]string{models.AuthorizationStatusPending, models.AuthorizationStatusEffective}, now).
		Updates(map[string]interface{}{
			"status":          models.AuthorizationStatusRevocationPending,
			"next_attempt_at": now,
		})
	return result.RowsAffected, result.Error
}

// Revoke 撤销授权
func (s *AuthorizationService) Revoke(tenantID uint, revokedBy uint, id int64) error {
	now := time.Now().UTC()
	revokedByInt64 := int64(revokedBy)

	result := s.db.Model(&models.Authorization{}).
		Where("id = ? AND tenant_id = ? AND status IN ?", id, tenantID,
			[]string{models.AuthorizationStatusPending, models.AuthorizationStatusEffective}).
		Updates(map[string]interface{}{
			"status":          models.AuthorizationStatusRevocationPending,
			"revoked_by":      revokedByInt64,
			"next_attempt_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("授权记录不存在或已撤销")
	}
	return nil
}
