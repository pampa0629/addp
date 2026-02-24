package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

type ApplicationService struct {
	db      *gorm.DB
	authSvc *AuthorizationService
}

func NewApplicationService(db *gorm.DB, authSvc *AuthorizationService) *ApplicationService {
	return &ApplicationService{db: db, authSvc: authSvc}
}

// ============================================================
// 请求/响应数据结构
// ============================================================

// CreateApplicationReq 提交申请请求
type CreateApplicationReq struct {
	AssetID     int64  `json:"asset_id" binding:"required"`
	ApplicantID int64  `json:"applicant_id" binding:"required"`
	Reason      string `json:"reason" binding:"required"`
	DurationDay int    `json:"duration_day"` // 0 时默认 30 天
}

// ApproveApplicationReq 审批通过请求
type ApproveApplicationReq struct {
	Note string `json:"note"` // 可选备注
}

// RejectApplicationReq 审批驳回请求
type RejectApplicationReq struct {
	Reason string `json:"reason" binding:"required"` // 必填驳回原因
}

// ApplicationListParams 申请列表查询参数
type ApplicationListParams struct {
	Page          int
	PageSize      int
	Status        string // pending/approved/rejected/revoked（向后兼容）
	DisplayStatus string // 综合状态过滤：pending/authorized/expired/revoked/rejected
	AssetID       int64
	ApplicantID   int64
}

// ApplicationWithAuth 带资产名称和关联授权信息的申请记录（列表/详情展示用）
type ApplicationWithAuth struct {
	models.Application
	AssetName     string     `json:"asset_name"`
	AssetTypeCode string     `json:"asset_type_code"`
	AuthID        *int64     `json:"auth_id,omitempty"`
	AuthIsActive  *bool      `json:"auth_is_active,omitempty"`
	AuthExpiresAt *time.Time `json:"auth_expires_at,omitempty"`
	AuthRevokedAt *time.Time `json:"auth_revoked_at,omitempty"`
}

// ============================================================
// 业务方法
// ============================================================

// Create 提交资产申请，含重复申请校验
func (s *ApplicationService) Create(tenantID uint, req *CreateApplicationReq) (*models.Application, error) {
	// 校验资产存在且已上架
	var asset models.Asset
	if err := s.db.Where("id = ? AND tenant_id = ? AND status = 'published'", req.AssetID, tenantID).
		First(&asset).Error; err != nil {
		return nil, fmt.Errorf("资产不存在或未上架")
	}

	// 重复申请校验：是否已有 pending 申请
	var pendingCount int64
	s.db.Model(&models.Application{}).
		Where("tenant_id = ? AND asset_id = ? AND applicant_id = ? AND status = 'pending'",
			tenantID, req.AssetID, req.ApplicantID).
		Count(&pendingCount)
	if pendingCount > 0 {
		return nil, errors.New("已有待审核申请，请勿重复提交")
	}

	// 重复申请校验：是否已有有效授权
	var authCount int64
	s.db.Model(&models.Authorization{}).
		Where("tenant_id = ? AND asset_id = ? AND user_id = ? AND is_active = true AND (expires_at IS NULL OR expires_at > NOW())",
			tenantID, req.AssetID, req.ApplicantID).
		Count(&authCount)
	if authCount > 0 {
		return nil, errors.New("已有有效授权，无需重复申请")
	}

	durationDay := req.DurationDay
	if durationDay <= 0 {
		durationDay = 30
	}

	app := &models.Application{
		TenantID:    int64(tenantID),
		AssetID:     req.AssetID,
		ApplicantID: req.ApplicantID,
		Reason:      req.Reason,
		DurationDay: durationDay,
		Status:      "pending",
	}
	if err := s.db.Create(app).Error; err != nil {
		return nil, err
	}
	return app, nil
}

// List 申请列表（管理员视角，支持多维度过滤，LEFT JOIN 授权信息）
func (s *ApplicationService) List(tenantID uint, params ApplicationListParams) ([]ApplicationWithAuth, int64, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}

	query := s.db.Table("asset.applications app").
		Select(`app.*,
			a.name AS asset_name,
			td.code AS asset_type_code,
			auth.id AS auth_id,
			auth.is_active AS auth_is_active,
			auth.expires_at AS auth_expires_at,
			auth.revoked_at AS auth_revoked_at`).
		Joins("LEFT JOIN asset.assets a ON a.id = app.asset_id").
		Joins("LEFT JOIN asset.type_definitions td ON td.id = a.type_id").
		Joins("LEFT JOIN asset.authorizations auth ON auth.application_id = app.id").
		Where("app.tenant_id = ?", tenantID)

	// display_status 优先，映射为具体 SQL 条件
	switch params.DisplayStatus {
	case "pending":
		query = query.Where("app.status = 'pending'")
	case "authorized":
		query = query.Where("app.status = 'approved' AND auth.is_active = true AND (auth.expires_at IS NULL OR auth.expires_at > NOW())")
	case "expired":
		query = query.Where("app.status = 'approved' AND auth.is_active = true AND auth.expires_at IS NOT NULL AND auth.expires_at <= NOW()")
	case "revoked":
		query = query.Where("app.status = 'approved' AND auth.is_active = false")
	case "rejected":
		query = query.Where("app.status = 'rejected'")
	default:
		// 兼容旧的 status 参数
		if params.Status != "" {
			query = query.Where("app.status = ?", params.Status)
		}
	}

	if params.AssetID > 0 {
		query = query.Where("app.asset_id = ?", params.AssetID)
	}
	if params.ApplicantID > 0 {
		query = query.Where("app.applicant_id = ?", params.ApplicantID)
	}

	var total int64
	query.Count(&total)

	var results []ApplicationWithAuth
	err := query.
		Order("app.created_at DESC").
		Offset((params.Page - 1) * params.PageSize).
		Limit(params.PageSize).
		Scan(&results).Error

	return results, total, err
}

// Get 获取单条申请详情（含关联授权信息）
func (s *ApplicationService) Get(tenantID uint, id int64) (*ApplicationWithAuth, error) {
	var result ApplicationWithAuth
	err := s.db.Table("asset.applications app").
		Select(`app.*,
			a.name AS asset_name,
			td.code AS asset_type_code,
			auth.id AS auth_id,
			auth.is_active AS auth_is_active,
			auth.expires_at AS auth_expires_at,
			auth.revoked_at AS auth_revoked_at`).
		Joins("LEFT JOIN asset.assets a ON a.id = app.asset_id").
		Joins("LEFT JOIN asset.type_definitions td ON td.id = a.type_id").
		Joins("LEFT JOIN asset.authorizations auth ON auth.application_id = app.id").
		Where("app.id = ? AND app.tenant_id = ?", id, tenantID).
		Scan(&result).Error
	if err != nil {
		return nil, err
	}
	if result.ID == 0 {
		return nil, fmt.Errorf("申请记录不存在")
	}
	return &result, nil
}

// Approve 审批通过：更新申请状态并创建授权记录（事务）
func (s *ApplicationService) Approve(tenantID uint, reviewerID uint, id int64, req *ApproveApplicationReq) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var app models.Application
		if err := tx.Where("id = ? AND tenant_id = ? AND status = 'pending'", id, tenantID).
			First(&app).Error; err != nil {
			return fmt.Errorf("申请记录不存在或状态不是待审批")
		}

		now := time.Now()
		reviewerInt64 := int64(reviewerID)
		expiresAt := now.Add(time.Duration(app.DurationDay) * 24 * time.Hour)

		// 更新申请状态
		if err := tx.Model(&app).Updates(map[string]interface{}{
			"status":      "approved",
			"reviewer_id": reviewerInt64,
			"review_note": req.Note,
			"reviewed_at": now,
			"expires_at":  expiresAt,
		}).Error; err != nil {
			return err
		}

		// 创建授权记录
		appID := app.ID
		auth := &models.Authorization{
			TenantID:      int64(tenantID),
			AssetID:       app.AssetID,
			ApplicationID: &appID,
			UserID:        app.ApplicantID,
			Credential:    "", // Phase 5 扩展数据服务 token
			ExpiresAt:     &expiresAt,
			IsActive:      true,
		}
		return tx.Create(auth).Error
	})
}

// Reject 审批驳回
func (s *ApplicationService) Reject(tenantID uint, reviewerID uint, id int64, req *RejectApplicationReq) error {
	if req.Reason == "" {
		return errors.New("驳回原因不能为空")
	}

	now := time.Now()
	reviewerInt64 := int64(reviewerID)

	result := s.db.Model(&models.Application{}).
		Where("id = ? AND tenant_id = ? AND status = 'pending'", id, tenantID).
		Updates(map[string]interface{}{
			"status":      "rejected",
			"reviewer_id": reviewerInt64,
			"review_note": req.Reason,
			"reviewed_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("申请记录不存在或状态不是待审批")
	}
	return nil
}

// RevokeByApplication 通过申请 ID 撤销对应的授权（事务）
func (s *ApplicationService) RevokeByApplication(tenantID uint, revokedBy uint, appID int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 验证申请存在且状态为 approved
		var app models.Application
		if err := tx.Where("id = ? AND tenant_id = ? AND status = 'approved'", appID, tenantID).
			First(&app).Error; err != nil {
			return fmt.Errorf("申请记录不存在或状态不是已通过")
		}

		// 找到对应的活跃授权记录
		var auth models.Authorization
		if err := tx.Where("application_id = ? AND tenant_id = ? AND is_active = true", appID, tenantID).
			First(&auth).Error; err != nil {
			return fmt.Errorf("未找到有效的授权记录")
		}

		now := time.Now()
		revokedByInt64 := int64(revokedBy)

		// 撤销授权（application.status 保持 approved 不变，撤销语义由 auth.is_active 表达）
		return tx.Model(&auth).Updates(map[string]interface{}{
			"is_active":  false,
			"revoked_at": now,
			"revoked_by": revokedByInt64,
		}).Error
	})
}
