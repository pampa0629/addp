package service

import (
	"fmt"
	"time"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RatingService struct {
	db *gorm.DB
}

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

// UpsertRatingReq 提交/更新评价请求
type UpsertRatingReq struct {
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

// Upsert 创建或更新评价（每用户每资产只能有一条）
// 若已存在则更新，否则创建
func (s *RatingService) Upsert(tenantID uint, userID int64, assetID int64, req *UpsertRatingReq) (*models.Rating, error) {
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	rating := models.Rating{
		TenantID:  int64(tenantID),
		AssetID:   assetID,
		UserID:    userID,
		Score:     req.Score,
		Comment:   req.Comment,
		Tags:      models.JSONBArray(tags),
		IsHandled: false,
		UpdatedAt: time.Now(),
	}

	// OnConflict: 若 (asset_id, user_id) 已存在则更新 score/comment/tags/updated_at
	// 注意：is_handled 不在更新列表中，保留管理员设置
	result := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "asset_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"score", "comment", "tags", "updated_at"}),
	}).Create(&rating)

	if result.Error != nil {
		return nil, result.Error
	}

	// 重新查询以获取完整记录（包括 id、created_at 等）
	var saved models.Rating
	if err := s.db.Where("tenant_id = ? AND user_id = ? AND asset_id = ?", tenantID, userID, assetID).First(&saved).Error; err != nil {
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
