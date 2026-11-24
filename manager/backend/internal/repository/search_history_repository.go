package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const recentSearchesFirst = "CASE WHEN updated_at IS NULL THEN 1 ELSE 0 END, updated_at DESC, id DESC"

// SearchHistoryRepository persists personal search history within a tenant.
type SearchHistoryRepository struct {
	db *gorm.DB
}

func NewSearchHistoryRepository(db *gorm.DB) *SearchHistoryRepository {
	return &SearchHistoryRepository{db: db}
}

func (r *SearchHistoryRepository) Record(ctx context.Context, tenantID, userID uint, query string, keep int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Concurrent first-page searches from the same user must share the retention limit.
		if tx.Dialector.Name() == "mysql" {
			// The unique owner row also serializes the very first searches.
			if err := tx.Exec(`INSERT INTO manager.search_history_owners(tenant_id,user_id) VALUES(?,?)
ON DUPLICATE KEY UPDATE user_id=VALUES(user_id)`, tenantID, userID).Error; err != nil {
				return err
			}
		} else {
			lockKey := fmt.Sprintf("manager.search_histories:%d:%d", tenantID, userID)
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockKey).Error; err != nil {
				return err
			}
		}

		now := time.Now().UTC()
		history := models.SearchHistory{TenantID: tenantID, UserID: userID, Query: query, CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}, {Name: "query"}},
			DoUpdates: clause.AssignmentColumns([]string{"updated_at"}),
		}).Create(&history).Error; err != nil {
			return err
		}

		// Only the oldest entry can cross the limit in steady state. Materialize
		// IDs before deletion so both dialects can prune the same target table.
		var retained []uint
		if err := tx.Model(&models.SearchHistory{}).Where("tenant_id = ? AND user_id = ?", tenantID, userID).
			Order(recentSearchesFirst).Limit(keep).Pluck("id", &retained).Error; err != nil {
			return err
		}
		remove := tx.Where("tenant_id = ? AND user_id = ?", tenantID, userID)
		if len(retained) > 0 {
			remove = remove.Where("id NOT IN ?", retained)
		}
		return remove.Delete(&models.SearchHistory{}).Error
	})
}

func (r *SearchHistoryRepository) List(ctx context.Context, tenantID, userID uint, limit int) ([]models.SearchHistory, error) {
	var histories []models.SearchHistory
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Order(recentSearchesFirst).Limit(limit).Find(&histories).Error
	return histories, err
}

func (r *SearchHistoryRepository) DeleteByID(ctx context.Context, tenantID, userID, historyID uint) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ? AND id = ?", tenantID, userID, historyID).
		Delete(&models.SearchHistory{}).Error
}

func (r *SearchHistoryRepository) DeleteAll(ctx context.Context, tenantID, userID uint) error {
	return r.db.WithContext(ctx).Where("tenant_id = ? AND user_id = ?", tenantID, userID).
		Delete(&models.SearchHistory{}).Error
}
