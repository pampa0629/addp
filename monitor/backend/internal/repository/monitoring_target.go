package repository

import (
	"context"
	"errors"

	"github.com/addp/monitor/internal/metricsdiscovery"
	"github.com/addp/monitor/internal/models"
	"gorm.io/gorm"
)

var (
	ErrTargetConflict = errors.New("monitoring target version conflict")
	ErrTargetNotFound = errors.New("monitoring target not found")
	ErrTargetBusy     = errors.New("monitoring target write busy")
)

type MonitoringTargetRepository struct{ db *gorm.DB }

func NewMonitoringTargetRepository(db *gorm.DB) *MonitoringTargetRepository {
	return &MonitoringTargetRepository{db: db}
}

func TargetProjection(row models.MonitoringTarget) metricsdiscovery.NodeTarget {
	return metricsdiscovery.NodeTarget{ID: row.ID, Version: row.Version,
		Subject: metricsdiscovery.NodeSubject{Kind: "node", NodeID: row.NodeID}, MonitorKind: row.MonitorKind,
		Source: metricsdiscovery.NodeSource{Type: row.SourceType, Endpoint: row.Endpoint}, Enabled: row.Enabled}
}

func (r *MonitoringTargetRepository) Snapshot(ctx context.Context) ([]metricsdiscovery.NodeTarget, error) {
	var rows []models.MonitoringTarget
	if r == nil || r.db == nil {
		return nil, metricsdiscovery.ErrUnconfigured
	}
	if err := r.db.WithContext(ctx).Order("id").Limit(metricsdiscovery.TargetLimit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]metricsdiscovery.NodeTarget, len(rows))
	for i, row := range rows {
		result[i] = TargetProjection(row)
	}
	if err := metricsdiscovery.CheckReservations(result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *MonitoringTargetRepository) Get(ctx context.Context, id string) (metricsdiscovery.NodeTarget, error) {
	if r == nil || r.db == nil {
		return metricsdiscovery.NodeTarget{}, metricsdiscovery.ErrUnconfigured
	}
	var row models.MonitoringTarget
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return metricsdiscovery.NodeTarget{}, ErrTargetNotFound
	}
	return TargetProjection(row), err
}

// Mutate serializes all writers, including creates, so budget checks cannot race.
// The callback checks current enabled physical endpoints under the same lock.
func (r *MonitoringTargetRepository) Mutate(ctx context.Context, target metricsdiscovery.NodeTarget, create, remove bool, validate func(context.Context, []metricsdiscovery.NodeTarget) error) (metricsdiscovery.NodeTarget, error) {
	if r == nil || r.db == nil {
		return target, metricsdiscovery.ErrUnconfigured
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var acquired bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?, ?)", 1094992976, 6001).Scan(&acquired).Error; err != nil {
			return err
		}
		if !acquired {
			return ErrTargetBusy
		}
		current, err := NewMonitoringTargetRepository(tx).Snapshot(ctx)
		if err != nil {
			return err
		}
		found := false
		next := make([]metricsdiscovery.NodeTarget, 0, len(current)+1)
		for _, old := range current {
			if old.ID != target.ID {
				next = append(next, old)
				continue
			}
			found = true
			if create || old.Version != target.Version || old.Subject != target.Subject || old.MonitorKind != target.MonitorKind {
				return ErrTargetConflict
			}
		}
		if !create && !found {
			return ErrTargetNotFound
		}
		if !remove {
			if create {
				target.Version = 1
			} else {
				target.Version++
			}
			next = append(next, target)
		}
		if err := metricsdiscovery.CheckReservations(next); err != nil {
			return err
		}
		// Removing collection intent must remain possible with offline sources.
		if !remove && target.Enabled && validate != nil {
			if err := validate(ctx, next); err != nil {
				return err
			}
		}
		if remove {
			return tx.Where("id = ? AND version = ?", target.ID, target.Version).Delete(&models.MonitoringTarget{}).Error
		}
		row := models.MonitoringTarget{ID: target.ID, NodeID: target.Subject.NodeID, MonitorKind: target.MonitorKind,
			SourceType: target.Source.Type, Endpoint: target.Source.Endpoint, Enabled: target.Enabled, Version: target.Version}
		if create {
			return tx.Create(&row).Error
		}
		result := tx.Model(&models.MonitoringTarget{}).Where("id = ? AND version = ?", target.ID, target.Version-1).
			Updates(map[string]any{"source_type": row.SourceType, "endpoint": row.Endpoint, "enabled": row.Enabled, "version": row.Version})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTargetConflict
		}
		return nil
	})
	return target, err
}
