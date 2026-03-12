package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Embedding 向量嵌入模型（多模态）
// 用于存储多模态对象（文本、图片、视频、音频、文档）的向量嵌入
type Embedding struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	EngineID      uint           `gorm:"not null;index:idx_engine_bucket" json:"engine_id"`               // 引擎ID（来自 system.engines）
	Bucket        string         `gorm:"size:255;not null;index:idx_engine_bucket" json:"bucket"`         // 存储桶名称
	Path          string         `gorm:"type:text;not null" json:"path"`                                  // 目录路径（以/结尾，不含bucket和文件名）
	Name          string         `gorm:"size:255;not null" json:"name"`                                   // 文件名
	Fingerprint   string         `gorm:"size:64;not null;uniqueIndex:idx_fingerprint_modality" json:"fingerprint"` // 数据指纹（使用 common.models.GenerateObjectFingerprint）
	DataUpdatedAt *time.Time     `gorm:"index" json:"data_updated_at"`                                    // 对象的最后修改时间（从 MinIO LastModified）
	Embedding     []float32      `gorm:"type:vector(1024);not null" json:"-"`                             // 向量嵌入（维度 1024，不返回给前端）
	Modality      string         `gorm:"size:20;not null;uniqueIndex:idx_fingerprint_modality" json:"modality"` // 模态类型（text/image/audio/video/document）
	Model         string         `gorm:"size:100;not null" json:"model"`                                  // 向量模型名称
	FileSize      int64          `json:"file_size"`                                                       // 文件大小（字节）
	ContentType   string         `gorm:"size:255" json:"content_type"`                                    // MIME类型
	Metadata      datatypes.JSON `json:"metadata"`                                                        // 额外元数据
	TenantID      *uint          `gorm:"index" json:"tenant_id"`                                          // 租户ID（用于多租户隔离）
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// TableName 指定表名（在 manager schema 中）
func (Embedding) TableName() string {
	return "manager.embeddings"
}

// EmbeddingStatus 嵌入状态
type EmbeddingStatus struct {
	EngineID         uint   `json:"engine_id"`
	Bucket           string `json:"bucket"`
	TotalObjects     int    `json:"total_objects"`      // 总对象数
	VectorizedCount  int    `json:"vectorized_count"`   // 已向量化数量
	UpToDateCount    int    `json:"up_to_date_count"`   // 最新（无需更新）数量
	OutdatedCount    int    `json:"outdated_count"`     // 过期（需要更新）数量
	NotVectorized    int    `json:"not_vectorized"`     // 未向量化数量
	LastVectorizedAt *time.Time `json:"last_vectorized_at"` // 最后向量化时间
}

// EmbeddingTask 向量化任务定义（任务定义型）
// 定义一个可反复执行的对象存储向量化任务，执行记录写入 common.task_executions
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

	CreatedBy *uint `json:"created_by,omitempty"`

	// EmbeddingTask 特有字段
	EngineID  uint   `gorm:"not null;index" json:"engine_id"`
	Bucket    string `gorm:"size:255;not null" json:"bucket"`
	Prefix    string `gorm:"type:text" json:"prefix,omitempty"`
	Recursive bool   `gorm:"default:true" json:"recursive"`

	// 向量化配置
	Modality  string `gorm:"size:20" json:"modality,omitempty"`   // 限定模态类型，空表示自动检测
	FileTypes string `gorm:"size:255" json:"file_types,omitempty"` // 文件扩展名过滤（逗号分隔，如 "jpg,png,pdf"）

	// 时间戳
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名（在 manager schema 中）
func (EmbeddingTask) TableName() string {
	return "manager.embedding_tasks"
}
