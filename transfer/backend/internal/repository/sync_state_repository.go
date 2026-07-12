package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

var ErrSyncStateFenced = errors.New("transfer sync state fencing token or state version changed")

type SyncStateRepository struct{ db *gorm.DB }

func NewSyncStateRepository(db *gorm.DB) *SyncStateRepository { return &SyncStateRepository{db: db} }

func (r *SyncStateRepository) Get(ctx context.Context, taskID uint, sourceIdentity, partition string) (*models.SyncState, error) {
	var state models.SyncState
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND source_identity = ? AND partition = ?", taskID, sourceIdentity, partition).
		First(&state).Error
	return &state, err
}

func (r *SyncStateRepository) GetByID(ctx context.Context, id uint) (*models.SyncState, error) {
	var state models.SyncState
	if err := r.db.WithContext(ctx).First(&state, id).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *SyncStateRepository) AssertFence(ctx context.Context, id uint, fencingToken uint64) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&models.SyncState{}).
		Where("id = ? AND fencing_token = ?", id, fencingToken).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrSyncStateFenced
	}
	return nil
}

func (r *SyncStateRepository) CommitPosition(ctx context.Context, id uint, expectedVersion, fencingToken uint64, position models.JSONMap, executionID string) error {
	result := r.db.WithContext(ctx).Model(&models.SyncState{}).
		Where("id = ? AND state_version = ? AND fencing_token = ?", id, expectedVersion, fencingToken).
		Updates(map[string]interface{}{
			"position":             position,
			"state_version":        expectedVersion + 1,
			"updated_execution_id": executionID,
		})
	if result.Error != nil {
		return fmt.Errorf("commit transfer sync position: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrSyncStateFenced
	}
	return nil
}
