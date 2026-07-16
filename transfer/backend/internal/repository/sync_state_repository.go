package repository

import (
	"context"
	"errors"
	"fmt"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

func (r *SyncStateRepository) List(ctx context.Context, taskID uint, sourceIdentity string) ([]models.SyncState, error) {
	var states []models.SyncState
	err := r.db.WithContext(ctx).
		Where("task_id = ? AND source_identity = ?", taskID, sourceIdentity).
		Order("partition ASC").Find(&states).Error
	return states, err
}

// ClaimContinuousPartition 将 partition state 绑定到当前有效 runtime lease fencing token。
func (r *SyncStateRepository) ClaimContinuousPartition(
	ctx context.Context,
	taskID uint,
	sourceIdentity, partition, positionType, positionVersion, owner string,
	fencingToken uint64,
) (*models.SyncState, error) {
	var state models.SyncState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var leaseCount int64
		if err := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ? AND lease_until > CURRENT_TIMESTAMP", taskID, owner, fencingToken).
			Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", taskID, models.TaskDesiredStateRunning).
			Count(&leaseCount).Error; err != nil {
			return err
		}
		if leaseCount != 1 {
			return ErrSyncStateFenced
		}
		initial := models.SyncState{
			TaskID: taskID, SourceIdentity: sourceIdentity, Partition: partition,
			PositionType: positionType, PositionVersion: positionVersion,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("task_id = ? AND source_identity = ? AND partition = ?", taskID, sourceIdentity, partition).
			First(&state).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if state.PositionType != positionType || state.PositionVersion != positionVersion {
			return fmt.Errorf("continuous sync state position identity drift for partition %q: %s/%s", partition, state.PositionType, state.PositionVersion)
		}
		if state.FencingToken != fencingToken {
			if err := tx.Model(&state).Update("fencing_token", fencingToken).Error; err != nil {
				return err
			}
			state.FencingToken = fencingToken
		}
		return nil
	})
	return &state, err
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
			"position":              position,
			"state_version":         expectedVersion + 1,
			"updated_execution_id":  executionID,
			"position_committed_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return fmt.Errorf("commit transfer sync position: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrSyncStateFenced
	}
	return nil
}

func (r *SyncStateRepository) CommitContinuousPosition(
	ctx context.Context,
	id, taskID uint,
	expectedVersion, fencingToken uint64,
	owner string,
	position models.JSONMap,
	executionID string,
) error {
	result := r.db.WithContext(ctx).Model(&models.SyncState{}).
		Where("id = ? AND task_id = ? AND state_version = ? AND fencing_token = ?", id, taskID, expectedVersion, fencingToken).
		Where("EXISTS (SELECT 1 FROM transfer.runtime_leases l WHERE l.task_id = ? AND l.owner_instance_id = ? AND l.fencing_token = ? AND l.lease_until > CURRENT_TIMESTAMP)", taskID, owner, fencingToken).
		Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", taskID, models.TaskDesiredStateRunning).
		Updates(map[string]interface{}{
			"position":              position,
			"state_version":         expectedVersion + 1,
			"updated_execution_id":  executionID,
			"position_committed_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return fmt.Errorf("commit continuous transfer sync position: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrSyncStateFenced
	}
	return nil
}
