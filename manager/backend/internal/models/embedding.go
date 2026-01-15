package models

import (
	"time"

	"gorm.io/datatypes"
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

// EmbeddingTask 向量化任务执行记录
// 用于记录批量向量化任务的执行历史和详细结果
type EmbeddingTask struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TaskID      string         `gorm:"size:255;not null;uniqueIndex" json:"task_id"`     // 任务ID（embedding-{engineID}-{type}-{timestamp}）
	TaskType    string         `gorm:"size:20;not null;index" json:"task_type"`          // 任务类型：object（单文件）、directory（目录批量）
	EngineID    uint           `gorm:"not null;index:idx_engine_tenant" json:"engine_id"` // 引擎ID
	Bucket      string         `gorm:"size:255;not null" json:"bucket"`                  // 存储桶
	ObjectKey   string         `gorm:"type:text" json:"object_key,omitempty"`            // 对象路径（单文件任务）
	Prefix      string         `gorm:"type:text" json:"prefix,omitempty"`                // 目录前缀（目录任务）
	Recursive   bool           `json:"recursive"`                                        // 是否递归（目录任务）
	Status      string         `gorm:"size:20;not null;index" json:"status"`             // 任务状态：pending、running、completed、failed
	StartedAt   *time.Time     `json:"started_at,omitempty"`                             // 开始时间
	CompletedAt *time.Time     `json:"completed_at,omitempty"`                           // 完成时间
	Duration    int64          `json:"duration,omitempty"`                               // 执行时长（毫秒）
	Total       int            `json:"total"`                                            // 总文件数
	Vectorized  int            `json:"vectorized"`                                       // 成功向量化数
	Skipped     int            `json:"skipped"`                                          // 跳过数
	Failed      int            `json:"failed"`                                           // 失败数
	Errors      datatypes.JSON `json:"errors,omitempty"`                                 // 错误详情（JSON数组）
	TenantID    *uint          `gorm:"index:idx_engine_tenant" json:"tenant_id"`         // 租户ID
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TableName 指定表名（在 manager schema 中）
func (EmbeddingTask) TableName() string {
	return "manager.embedding_tasks"
}
