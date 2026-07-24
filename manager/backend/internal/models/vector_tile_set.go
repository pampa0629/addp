package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

// VectorTileSetTask generates a business PMTiles item. Manager owns the task, not the result.
type VectorTileSetTask struct {
	ID                  uint                 `gorm:"primaryKey" json:"id"`
	TenantID            uint                 `gorm:"not null;index:idx_vector_tile_set_tasks_tenant" json:"tenant_id"`
	Name                string               `gorm:"size:255;not null" json:"name"`
	Description         string               `gorm:"type:text" json:"description,omitempty"`
	Enabled             bool                 `gorm:"not null" json:"enabled"`
	Schedule            string               `gorm:"size:255" json:"-"`
	NextRunAt           *time.Time           `json:"-"`
	LastRunAt           *time.Time           `json:"last_run_at,omitempty"`
	LastExecutionID     *string              `gorm:"size:36;index:idx_vector_tile_set_tasks_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string              `gorm:"size:50" json:"last_execution_status,omitempty"`
	Config              commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy           *uint                `json:"created_by,omitempty"`
	CreatedAt           time.Time            `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt           time.Time            `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt           gorm.DeletedAt       `gorm:"index:idx_vector_tile_set_tasks_deleted_at" json:"-"`
}

func (VectorTileSetTask) TableName() string { return "manager.vector_tile_set_tasks" }
