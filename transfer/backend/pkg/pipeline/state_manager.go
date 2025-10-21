package pipeline

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// StateManager 状态管理器
// 负责管理任务执行的 Checkpoint 状态，支持断点续传
type StateManager struct {
	db *gorm.DB
}

// Checkpoint Checkpoint 数据结构
type Checkpoint struct {
	ID          uint                   `gorm:"primaryKey" json:"id"`
	TaskID      uint                   `gorm:"index:idx_checkpoint_task_exec;not null" json:"task_id"`
	ExecutionID uint                   `gorm:"index:idx_checkpoint_task_exec;not null" json:"execution_id"`
	Offset      int64                  `gorm:"not null" json:"offset"`
	PartitionID string                 `gorm:"type:varchar(255)" json:"partition_id"`
	State       map[string]interface{} `gorm:"type:jsonb" json:"state"`
	CreatedAt   time.Time              `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// TableName 指定表名
func (Checkpoint) TableName() string {
	return "transfer.checkpoints"
}

// NewStateManager 创建状态管理器
func NewStateManager(db *gorm.DB) *StateManager {
	return &StateManager{db: db}
}

// SaveCheckpoint 保存 Checkpoint
func (s *StateManager) SaveCheckpoint(checkpoint *Checkpoint) error {
	if checkpoint.TaskID == 0 || checkpoint.ExecutionID == 0 {
		return fmt.Errorf("task_id and execution_id are required")
	}

	checkpoint.CreatedAt = time.Now()
	return s.db.Create(checkpoint).Error
}

// LoadCheckpoint 加载最新的 Checkpoint
func (s *StateManager) LoadCheckpoint(taskID, executionID uint) (*Checkpoint, error) {
	var checkpoint Checkpoint
	err := s.db.Where("task_id = ? AND execution_id = ?", taskID, executionID).
		Order("created_at DESC").
		First(&checkpoint).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("no checkpoint found for task %d execution %d", taskID, executionID)
		}
		return nil, err
	}

	return &checkpoint, nil
}

// LoadLatestCheckpointForTask 加载任务的最新 Checkpoint（跨执行）
func (s *StateManager) LoadLatestCheckpointForTask(taskID uint) (*Checkpoint, error) {
	var checkpoint Checkpoint
	err := s.db.Where("task_id = ?", taskID).
		Order("created_at DESC").
		First(&checkpoint).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("no checkpoint found for task %d", taskID)
		}
		return nil, err
	}

	return &checkpoint, nil
}

// ListCheckpoints 列出指定任务执行的所有 Checkpoint
func (s *StateManager) ListCheckpoints(taskID, executionID uint, limit int) ([]Checkpoint, error) {
	var checkpoints []Checkpoint

	query := s.db.Where("task_id = ? AND execution_id = ?", taskID, executionID).
		Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&checkpoints).Error
	return checkpoints, err
}

// DeleteCheckpoints 删除指定任务执行的所有 Checkpoint
func (s *StateManager) DeleteCheckpoints(taskID, executionID uint) error {
	return s.db.Where("task_id = ? AND execution_id = ?", taskID, executionID).
		Delete(&Checkpoint{}).Error
}

// DeleteCheckpointsForTask 删除任务的所有 Checkpoint
func (s *StateManager) DeleteCheckpointsForTask(taskID uint) error {
	return s.db.Where("task_id = ?", taskID).
		Delete(&Checkpoint{}).Error
}

// GetCheckpointCount 获取 Checkpoint 数量
func (s *StateManager) GetCheckpointCount(taskID, executionID uint) (int64, error) {
	var count int64
	err := s.db.Model(&Checkpoint{}).
		Where("task_id = ? AND execution_id = ?", taskID, executionID).
		Count(&count).Error
	return count, err
}

// CleanupOldCheckpoints 清理旧的 Checkpoint（保留最新 N 个）
func (s *StateManager) CleanupOldCheckpoints(taskID, executionID uint, keepLast int) error {
	// 查询需要保留的 Checkpoint ID
	var keepIDs []uint
	err := s.db.Model(&Checkpoint{}).
		Select("id").
		Where("task_id = ? AND execution_id = ?", taskID, executionID).
		Order("created_at DESC").
		Limit(keepLast).
		Pluck("id", &keepIDs).Error

	if err != nil {
		return err
	}

	if len(keepIDs) == 0 {
		return nil
	}

	// 删除不在保留列表中的记录
	return s.db.Where("task_id = ? AND execution_id = ? AND id NOT IN ?", taskID, executionID, keepIDs).
		Delete(&Checkpoint{}).Error
}
