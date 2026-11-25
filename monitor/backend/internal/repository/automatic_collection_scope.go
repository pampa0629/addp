package repository

import (
	"context"
	"time"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

type AutomaticCollectionRepository struct{ db *gorm.DB }

func NewAutomaticCollectionRepository(db *gorm.DB) *AutomaticCollectionRepository {
	return &AutomaticCollectionRepository{db: db}
}

func (r *AutomaticCollectionRepository) Snapshot(ctx context.Context) (map[string]models.AutomaticCollectionScope, error) {
	var rows []models.AutomaticCollectionScope
	if err := r.db.WithContext(ctx).Limit(metricsdiscovery.AutomaticScopeLimit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > metricsdiscovery.AutomaticScopeLimit {
		return nil, metricsdiscovery.ErrBudgetExceeded
	}
	result := make(map[string]models.AutomaticCollectionScope, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func (r *AutomaticCollectionRepository) Paused(ctx context.Context) (map[string]bool, error) {
	rows, err := r.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	paused := make(map[string]bool)
	for id, row := range rows {
		if !row.Enabled {
			paused[id] = true
		}
	}
	return paused, nil
}

func (r *AutomaticCollectionRepository) Save(ctx context.Context, scope metricsdiscovery.AutomaticScope, enabled bool, version int64) (models.AutomaticCollectionScope, error) {
	var result models.AutomaticCollectionScope
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAutomaticCollection(tx); err != nil {
			return err
		}
		current, err := NewAutomaticCollectionRepository(tx).Snapshot(ctx)
		if err != nil {
			return err
		}
		result = current[scope.ID]
		if result.Version == 0 {
			result = models.AutomaticCollectionScope{AutomaticScope: scope, Version: 1, Enabled: true}
		}
		if result.Version != version {
			return ErrTargetConflict
		}
		if result.Enabled == enabled {
			return nil
		}
		result.Enabled, result.Version, result.ChangedAt = enabled, version+1, time.Now().UTC()
		if current[scope.ID].Version == 0 {
			if len(current) == metricsdiscovery.AutomaticScopeLimit {
				return metricsdiscovery.ErrBudgetExceeded
			}
			return tx.Create(&result).Error
		}
		return tx.Model(&result).Where("version = ?", version).Select("enabled", "version", "changed_at").Updates(&result).Error
	})
	return result, err
}

// Serialize policy changes with commits based on current metric evidence.
func LockAutomaticCollection(tx *gorm.DB) error {
	if tx.Dialector.Name() == "mysql" {
		return lockMySQLConfiguration(tx, "monitor.automatic-collection")
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", models.AutomaticCollectionLockNamespace, models.AutomaticCollectionLockID).Error
}
