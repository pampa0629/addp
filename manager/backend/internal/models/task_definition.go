package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	TaskCategoryManagedQuickView  = "managed_quick_view"
	TaskCategorySpatialBusiness   = "spatial_business"
	TaskResourceRoleSource        = "source"
	TaskResourceRoleTarget        = "target"
	TaskBindingStatusActive       = "active"
	TaskBindingStatusMissing      = "missing"
	TaskBindingIssueMissingEngine = "missing_engine"
	TaskBindingIssueMissingSource = "missing_source"
)

// TaskDefinition is the single persistence model for repeatable Manager
// derivation tasks. task_type selects a typed validator and executor; Config is
// never interpreted as an untyped generic execution contract.
type TaskDefinition struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	TenantID uint   `gorm:"not null;index:idx_manager_task_definitions_tenant_type,priority:1" json:"tenant_id"`
	TaskType string `gorm:"size:80;not null;index:idx_manager_task_definitions_tenant_type,priority:2" json:"task_type"`
	Version  uint   `gorm:"not null;default:1" json:"version"`

	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"not null" json:"enabled"`

	Schedule            string     `gorm:"size:255" json:"-"`
	NextRunAt           *time.Time `json:"-"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastExecutionID     *string    `gorm:"size:36;index:idx_manager_task_definitions_last_execution" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`
	BindingStatus       string     `gorm:"size:16;not null;default:active" json:"binding_status"`
	BindingIssue        string     `gorm:"size:32;not null;default:''" json:"binding_issue,omitempty"`

	SemanticKey string               `gorm:"size:160" json:"semantic_key,omitempty"`
	Config      commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	CreatedBy   *uint                `json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index:idx_manager_task_definitions_deleted_at" json:"-"`
}

func (TaskDefinition) TableName() string { return "manager.task_definitions" }

// TaskResourceBinding is the normalized resource projection used by cleanup
// and engine lifecycle checks. It prevents those consumers from guessing JSON
// paths that differ between task types.
type TaskResourceBinding struct {
	ID               uint      `gorm:"primaryKey" json:"id"`
	TaskDefinitionID uint      `gorm:"not null;index:idx_manager_task_resource_task" json:"task_definition_id"`
	TenantID         uint      `gorm:"not null;index:idx_manager_task_resource_engine,priority:1" json:"tenant_id"`
	Role             string    `gorm:"size:16;not null" json:"role"`
	EngineID         uint      `gorm:"not null;index:idx_manager_task_resource_engine,priority:2" json:"engine_id"`
	Locator          string    `gorm:"type:text;not null" json:"locator"`
	ItemID           *uint     `json:"item_id,omitempty"`
	ItemFingerprint  string    `gorm:"size:64" json:"item_fingerprint,omitempty"`
	Ordinal          int       `gorm:"not null;default:0" json:"ordinal"`
	CreatedAt        time.Time `gorm:"autoCreateTime;not null" json:"created_at"`
}

func (TaskResourceBinding) TableName() string { return "manager.task_resource_bindings" }
