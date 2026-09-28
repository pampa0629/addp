package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RatingService struct {
	db *gorm.DB
}

var (
	ErrRatingAccessDenied  = errors.New("rating access denied")
	ErrRatingAlreadyExists = errors.New("rating already exists")
	ErrRatingNotFound      = errors.New("rating not found")
	ErrRatingAssetHidden   = errors.New("rating asset is not published")
)

func NewRatingService(db *gorm.DB) *RatingService {
	return &RatingService{db: db}
}

// ============================================================
// 请求/响应数据结构
// ============================================================

// RatingWithUser 带资产和用户信息的评价（列表展示用）
type RatingWithUser struct {
	models.Rating
	UserName  string `json:"user_name"`
	AssetName string `json:"asset_name"`
}

// RatingWriteReq 创建或修改评价请求
type RatingWriteReq struct {
	Score   float32  `json:"score" binding:"required,min=1,max=5"`
	Comment string   `json:"comment"`
	Tags    []string `json:"tags"` // 问题反馈标签
}

// RatingListParams 评价列表查询参数
type RatingListParams struct {
	AssetID     int64
	UserID      int64
	HasFeedback bool // 仅查询有问题反馈标签的评价
	IsHandled   *bool
	Page        int
	PageSize    int
}

// ============================================================
// 业务方法
// ============================================================

// List 查询评价列表
func (s *RatingService) List(tenantID uint, params RatingListParams) ([]RatingWithUser, int64, error) {
	if params.Page <= 0 {
		params.Page = 1
	}
	if params.PageSize <= 0 {
		params.PageSize = 20
	}

	query := s.db.Table("asset.ratings r").
		Select("r.*, u.display_name AS user_name, a.name AS asset_name").
		Joins("LEFT JOIN system.users u ON u.id = r.user_id").
		Joins("LEFT JOIN asset.assets a ON a.id = r.asset_id").
		Where("r.tenant_id = ?", tenantID)

	if params.AssetID > 0 {
		query = query.Where("r.asset_id = ?", params.AssetID)
	}
	if params.UserID > 0 {
		query = query.Where("r.user_id = ?", params.UserID)
	}
	if params.HasFeedback {
		query = query.Where("jsonb_array_length(r.tags) > 0")
	}
	if params.IsHandled != nil {
		query = query.Where("r.is_handled = ?", *params.IsHandled)
	}

	var total int64
	query.Count(&total)

	var results []RatingWithUser
	err := query.
		Order("r.created_at DESC").
		Offset((params.Page - 1) * params.PageSize).
		Limit(params.PageSize).
		Scan(&results).Error

	return results, total, err
}

// GetByUser 查询某用户对某资产的评价（不存在返回 nil, nil）
func (s *RatingService) GetByUser(tenantID uint, userID int64, assetID int64) (*models.Rating, error) {
	var r models.Rating
	err := s.db.
		Where("tenant_id = ? AND user_id = ? AND asset_id = ?", tenantID, userID, assetID).
		First(&r).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &r, err
}

func (s *RatingService) Average(tenantID uint, assetID int64) (float64, error) {
	var average float64
	err := s.db.Model(&models.Rating{}).
		Where("tenant_id = ? AND asset_id = ?", tenantID, assetID).
		Select("COALESCE(AVG(score), 0)").Scan(&average).Error
	return average, err
}

func ratingForWrite(tenantID uint, userID, assetID int64, req *RatingWriteReq) models.Rating {
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	return models.Rating{
		TenantID:  int64(tenantID),
		AssetID:   assetID,
		UserID:    userID,
		Score:     req.Score,
		Comment:   req.Comment,
		Tags:      models.JSONBArray(tags),
		IsHandled: false,
		UpdatedAt: time.Now(),
	}
}

func (s *RatingService) withEffectiveAuthorization(tenantID uint, userID, assetID int64, write func(*gorm.DB) error) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var asset models.Asset
		err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Select("id").
			Where("tenant_id = ? AND id = ? AND status = ?", tenantID, assetID, "published").
			First(&asset).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRatingAssetHidden
		}
		if err != nil {
			return err
		}
		var authorization models.Authorization
		err = tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Select("id").
			Where("tenant_id = ? AND user_id = ? AND asset_id = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)",
				tenantID, userID, assetID, models.AuthorizationStatusEffective, time.Now().UTC()).
			First(&authorization).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRatingAccessDenied
		}
		if err != nil {
			return err
		}
		return write(tx)
	})
}

// Create 仅创建本人的评价；唯一索引保证并发重复创建不会变成修改。
func (s *RatingService) Create(tenantID uint, userID, assetID int64, req *RatingWriteReq) (*models.Rating, error) {
	rating := ratingForWrite(tenantID, userID, assetID, req)
	err := s.withEffectiveAuthorization(tenantID, userID, assetID, func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rating)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRatingAlreadyExists
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &rating, nil
}

// Update 仅修改本人的现有评价，不改变管理员处理状态。
func (s *RatingService) Update(tenantID uint, userID, assetID int64, req *RatingWriteReq) (*models.Rating, error) {
	rating := ratingForWrite(tenantID, userID, assetID, req)
	var saved models.Rating
	err := s.withEffectiveAuthorization(tenantID, userID, assetID, func(tx *gorm.DB) error {
		result := tx.Model(&models.Rating{}).
			Where("tenant_id = ? AND user_id = ? AND asset_id = ?", tenantID, userID, assetID).
			Updates(map[string]interface{}{
				"score": rating.Score, "comment": rating.Comment, "tags": rating.Tags, "updated_at": rating.UpdatedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrRatingNotFound
		}
		return tx.Where("tenant_id = ? AND user_id = ? AND asset_id = ?", tenantID, userID, assetID).First(&saved).Error
	})
	if err != nil {
		return nil, err
	}
	return &saved, nil
}

// MarkHandled 管理员标记问题反馈为已处理/未处理
func (s *RatingService) MarkHandled(tenantID uint, id int64, isHandled bool) error {
	result := s.db.Model(&models.Rating{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("is_handled", isHandled)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("评价记录不存在")
	}
	return nil
}

// GetStats 获取资产评价统计（平均分 + 各分段数量）
func (s *RatingService) GetStats(tenantID uint, assetID int64) (map[string]interface{}, error) {
	type statsRow struct {
		AvgScore float64 `json:"avg_score"`
		Count    int64   `json:"count"`
	}
	var stats statsRow
	err := s.db.Table("asset.ratings").
		Select("COALESCE(AVG(score), 0) AS avg_score, COUNT(*) AS count").
		Where("tenant_id = ? AND asset_id = ?", tenantID, assetID).
		Scan(&stats).Error
	if err != nil {
		return nil, err
	}

	// 各分数段分布
	type distRow struct {
		Score int   `json:"score"`
		Count int64 `json:"count"`
	}
	var dist []distRow
	s.db.Table("asset.ratings").
		Select("ROUND(score)::int AS score, COUNT(*) AS count").
		Where("tenant_id = ? AND asset_id = ?", tenantID, assetID).
		Group("ROUND(score)::int").
		Order("score").
		Scan(&dist)

	distribution := make(map[int]int64)
	for _, d := range dist {
		distribution[d.Score] = d.Count
	}
	_ = time.Now() // suppress import if needed

	return map[string]interface{}{
		"avg_score":    stats.AvgScore,
		"count":        stats.Count,
		"distribution": distribution,
	}, nil
}
