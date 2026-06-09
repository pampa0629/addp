package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

const (
	EmbeddingStatusReady         = "ready"
	EmbeddingStatusOutdated      = "outdated"
	EmbeddingStatusFailed        = "failed"
	EmbeddingStatusUnsupported   = "unsupported"
	EmbeddingStatusMissingSource = "missing_source"

	EmbeddingReasonReady               = "ready"
	EmbeddingReasonSourceChanged       = "source_changed"
	EmbeddingReasonModelChanged        = "model_changed"
	EmbeddingReasonDimensionChanged    = "dimension_changed"
	EmbeddingReasonFileTooLarge        = "file_too_large"
	EmbeddingReasonFormatUnsupported   = "format_unsupported"
	EmbeddingReasonReadFailed          = "read_failed"
	EmbeddingReasonSourceMissing       = "source_missing"
	EmbeddingReasonEmbeddingFailed     = "embedding_failed"
	EmbeddingReasonEmbeddingServiceNil = "embedding_service_unavailable"
)

// Embedding 向量化结果 artifact state。
// 一条记录表达某个 data item 当前留下的向量化结果状态。
type Embedding struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	TenantID        uint       `gorm:"not null;uniqueIndex:idx_embedding_item;index:idx_embeddings_ready_query" json:"tenant_id"`
	ItemFingerprint string     `gorm:"size:64;not null;uniqueIndex:idx_embedding_item" json:"item_fingerprint"`
	ItemID          uint       `gorm:"not null;index" json:"item_id"`
	EngineID        uint       `gorm:"not null;index:idx_embeddings_engine;index:idx_embeddings_ready_query" json:"engine_id"`
	Locator         string     `gorm:"type:text;not null" json:"locator"`
	SourceVersion   string     `gorm:"size:255;not null" json:"source_version"`
	Embedding       []float32  `gorm:"type:vector(1024)" json:"-"`
	Model           string     `gorm:"size:100;not null;index:idx_embeddings_ready_query" json:"model"`
	Dimension       int        `gorm:"not null;index:idx_embeddings_ready_query" json:"dimension"`
	Status          string     `gorm:"size:32;not null;index:idx_embeddings_status;index:idx_embeddings_ready_query;check:ck_embeddings_status,status IN ('ready','outdated','failed','unsupported','missing_source')" json:"status"`
	StatusReason    string     `gorm:"size:64" json:"status_reason,omitempty"`
	ErrorMessage    string     `gorm:"type:text" json:"error_message,omitempty"`
	LastExecutionID *string    `gorm:"size:36" json:"last_execution_id,omitempty"`
	VectorizedAt    *time.Time `json:"vectorized_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// TableName 指定表名（在 manager schema 中）
func (Embedding) TableName() string {
	return "manager.embeddings"
}

// EmbeddingTask 向量化任务定义（任务定义型）。
// 任务范围和策略只保存在 Config 中；执行记录写入 common.task_executions。
type EmbeddingTask struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"not null;index" json:"tenant_id"`

	// 任务基本信息
	Name        string `gorm:"size:255;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`
	Enabled     bool   `gorm:"default:true" json:"enabled"`

	// 最近一次执行信息（回写）
	LastExecutionID     *string    `gorm:"size:36" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `gorm:"size:50" json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	Schedule            string     `gorm:"size:255" json:"schedule,omitempty"`

	CreatedBy *uint `json:"created_by,omitempty"`

	// 向量化任务私有配置，结构见 addp向量化规范：
	// config.target / config.filters / config.embedding。
	Config commonModels.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"config"`

	// 时间戳
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名（在 manager schema 中）
func (EmbeddingTask) TableName() string {
	return "manager.embedding_tasks"
}
