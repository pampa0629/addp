package models

import "time"

const (
	MaterializationBatchPreparing  = "preparing"
	MaterializationBatchPrepared   = "prepared"
	MaterializationBatchSealed     = "sealed"
	MaterializationBatchPublishing = "publishing"
	MaterializationBatchPublished  = "published"
	MaterializationBatchFailed     = "failed"
	MaterializationBatchAborted    = "aborted"
)

// MaterializationBatch records one controlled physical-table replacement.
// The physical DDL is derived from an approved LogicalTable; callers never
// submit SQL or physical table names through the execution API.
type MaterializationBatch struct {
	ID                  string     `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID            int64      `gorm:"not null;index" json:"tenant_id"`
	LogicalTableID      int64      `gorm:"not null;index" json:"logical_table_id"`
	LogicalTableVersion int64      `gorm:"not null" json:"logical_table_version"`
	EngineID            int64      `gorm:"not null" json:"engine_id"`
	TargetParentLocator string     `gorm:"type:text;not null" json:"target_parent_locator"`
	TargetName          string     `gorm:"size:63;not null" json:"target_name"`
	StagingName         string     `gorm:"size:63;not null" json:"staging_name"`
	SchemaFingerprint   string     `gorm:"size:64;not null" json:"schema_fingerprint"`
	Status              string     `gorm:"size:20;not null;index" json:"status"`
	PrepareExecutionID  string     `gorm:"size:255;not null;uniqueIndex" json:"prepare_execution_id"`
	WriterExecutionID   *string    `gorm:"size:255;index" json:"writer_execution_id,omitempty"`
	SealExecutionID     *string    `gorm:"size:255;uniqueIndex" json:"seal_execution_id,omitempty"`
	PublishExecutionID  *string    `gorm:"size:255;index" json:"publish_execution_id,omitempty"`
	PublishedAt         *time.Time `json:"published_at,omitempty"`
	CreatedAt           time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"not null" json:"updated_at"`
}

func (MaterializationBatch) TableName() string { return "model.materialization_batches" }
