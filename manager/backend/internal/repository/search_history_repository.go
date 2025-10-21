package repository

import (
	"errors"
	"strings"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// SearchHistoryRepository 提供检索历史的持久化操作
type SearchHistoryRepository struct {
	db *gorm.DB
}

func NewSearchHistoryRepository(db *gorm.DB) *SearchHistoryRepository {
	return &SearchHistoryRepository{db: db}
}

// Upsert 按用户及查询内容更新或创建历史记录
func (r *SearchHistoryRepository) Upsert(userID uint, tenantID *uint, query string) error {
	if userID == 0 {
		return errors.New("invalid user id")
	}
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return errors.New("query cannot be empty")
	}

	var history models.SearchHistory
	err := r.db.Where("user_id = ? AND query = ?", userID, trimmed).First(&history).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		history = models.SearchHistory{
			UserID:   userID,
			TenantID: tenantID,
			Query:    trimmed,
		}
		if err := r.db.Create(&history).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return r.db.
					Model(&models.SearchHistory{}).
					Where("user_id = ? AND query = ?", userID, trimmed).
					Updates(map[string]interface{}{
						"tenant_id": tenantID,
						"query":     trimmed,
					}).Error
			}
			return err
		}
		return nil
	case err != nil:
		return err
	default:
		history.TenantID = tenantID
		history.Query = trimmed
		return r.db.Model(&history).Updates(map[string]interface{}{
			"tenant_id": tenantID,
			"query":     trimmed,
		}).Error
	}
}

// List 获取指定用户的最近历史记录，按更新时间倒序
func (r *SearchHistoryRepository) List(userID uint, limit int) ([]models.SearchHistory, error) {
	if userID == 0 {
		return nil, errors.New("invalid user id")
	}
	query := r.db.Where("user_id = ?", userID).Order("updated_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var histories []models.SearchHistory
	if err := query.Find(&histories).Error; err != nil {
		return nil, err
	}
	return histories, nil
}

// DeleteByID 删除单条历史记录，确保按用户隔离
func (r *SearchHistoryRepository) DeleteByID(userID, historyID uint) error {
	if userID == 0 || historyID == 0 {
		return errors.New("invalid identifier")
	}
	return r.db.Where("user_id = ? AND id = ?", userID, historyID).Delete(&models.SearchHistory{}).Error
}

// DeleteAll 移除用户的全部历史记录
func (r *SearchHistoryRepository) DeleteAll(userID uint) error {
	if userID == 0 {
		return errors.New("invalid user id")
	}
	return r.db.Where("user_id = ?", userID).Delete(&models.SearchHistory{}).Error
}

// TrimByUser 仅保留最近 keep 条记录，其余删除
func (r *SearchHistoryRepository) TrimByUser(userID uint, keep int) error {
	if userID == 0 {
		return errors.New("invalid user id")
	}
	if keep <= 0 {
		return r.DeleteAll(userID)
	}

	var ids []uint
	if err := r.db.
		Model(&models.SearchHistory{}).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Offset(keep).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return r.db.Where("id IN ?", ids).Delete(&models.SearchHistory{}).Error
}
