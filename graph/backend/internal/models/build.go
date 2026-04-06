package models

import (
	"time"

	"gorm.io/datatypes"
)

// BuildTask 图谱构建任务（配置和统计，执行追踪交由 Monitor 模块）
type BuildTask struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	TenantID            uint           `gorm:"not null;index" json:"tenant_id"`
	GraphID             uint           `gorm:"not null;index" json:"graph_id"`
	ExecutionID         string         `gorm:"size:64" json:"execution_id"` // 关联 common.task_executions.execution_id
	Name                string         `gorm:"not null" json:"name"`
	Description         string         `json:"description"`
	Status              string         `gorm:"not null;default:'pending'" json:"status"` // pending/running/completed/failed/cancelled
	ConfidenceThreshold float64        `gorm:"not null;default:0.70" json:"confidence_threshold"`
	ChunkSize           int            `gorm:"not null;default:1000" json:"chunk_size"`    // 每个 chunk 的字符数
	ChunkOverlap        int            `gorm:"not null;default:200" json:"chunk_overlap"`  // 相邻 chunk 的重叠字符数
	DocContextSize      int            `gorm:"not null;default:200" json:"doc_context_size"` // 文档头部上下文字符数
	Stats               datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"stats"` // {total_materials,processed,auto_written,pending_review,approved,rejected}
	ErrorMessage        string         `json:"error_message"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	StartedAt           *time.Time     `json:"started_at"`
	CompletedAt         *time.Time     `json:"completed_at"`

	Graph     *KnowledgeGraph  `gorm:"foreignKey:GraphID" json:"graph,omitempty"`
	Materials []BuildMaterial  `gorm:"foreignKey:TaskID" json:"materials,omitempty"`
}

// BuildMaterial 构建材料（一个构建任务关联的原始文件）
type BuildMaterial struct {
	ID              uint           `gorm:"primaryKey" json:"id"`
	TaskID          uint           `gorm:"not null;index" json:"task_id"`
	TenantID        uint           `gorm:"not null;index" json:"tenant_id"`
	GraphID         uint           `gorm:"not null;index" json:"graph_id"`
	Type            string         `gorm:"not null" json:"type"`       // document/url
	FileName        string         `json:"file_name"`
	FilePath        string         `json:"file_path"`  // MinIO path (graph/build/ 前缀)
	FileSize        int64          `json:"file_size"`
	// 分块进度（支持断点续传）
	TotalChunks     int            `gorm:"default:0" json:"total_chunks"`      // 总 chunk 数（首次分块时写入）
	ProcessedChunks int            `gorm:"default:0" json:"processed_chunks"`  // 已处理 chunk 数
	Status          string         `gorm:"not null;default:'pending'" json:"status"` // pending/processing/completed/failed
	Stats           datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"stats"` // {total_chunks,processed_chunks,auto_written,queued_review,entities_count,relations_count}
	ErrorMessage    string         `json:"error_message"`
	CreatedAt       time.Time      `json:"created_at"`
	ProcessedAt     *time.Time     `json:"processed_at"`
}

// ReviewItem 待审核项（低置信度抽取结果，等待人工确认）
type ReviewItem struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	TaskID       uint           `gorm:"not null;index" json:"task_id"`
	MaterialID   uint           `gorm:"index" json:"material_id"`
	TenantID     uint           `gorm:"not null;index" json:"tenant_id"`
	GraphID      uint           `gorm:"not null;index" json:"graph_id"`
	ItemType     string         `gorm:"not null" json:"item_type"` // entity/relation
	// entity content: {type, unique_key_field, unique_key_value, properties}
	// relation content: {type, source_type, source_unique_field, source_unique_value, target_type, target_unique_field, target_unique_value, properties}
	Content      datatypes.JSON `gorm:"type:jsonb;not null" json:"content"`
	Confidence   float64        `gorm:"not null" json:"confidence"` // 0.0 ~ 1.0
	SourceText   string         `json:"source_text"` // 原始文本片段（审核上下文）
	Status       string         `gorm:"not null;default:'pending'" json:"status"` // pending/approved/rejected/modified
	FinalContent datatypes.JSON `gorm:"type:jsonb" json:"final_content"` // 修改后内容（status=modified 时）
	Neo4jID      string         `json:"neo4j_id"` // 写入 Neo4j 后的节点/关系 elementId
	ReviewedBy   *uint          `json:"reviewed_by"`
	ReviewedAt   *time.Time     `json:"reviewed_at"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// BuildTaskStats 构建任务统计（用于 JSON 序列化）
type BuildTaskStats struct {
	TotalMaterials  int `json:"total_materials"`
	Processed       int `json:"processed"`
	AutoWritten     int `json:"auto_written"`
	PendingReview   int `json:"pending_review"`
	Approved        int `json:"approved"`
	Rejected        int `json:"rejected"`
}

// BuildMaterialStats 材料统计
type BuildMaterialStats struct {
	TotalChunks     int `json:"total_chunks"`
	ProcessedChunks int `json:"processed_chunks"`
	AutoWritten     int `json:"auto_written"`
	QueuedReview    int `json:"queued_review"`
	EntitiesCount   int `json:"entities_count"`
	RelationsCount  int `json:"relations_count"`
}

// BuildTask 状态常量
const (
	BuildStatusPending   = "pending"
	BuildStatusRunning   = "running"
	BuildStatusCompleted = "completed"
	BuildStatusFailed    = "failed"
	BuildStatusCancelled = "cancelled"
)

// ReviewItem 状态常量
const (
	ReviewStatusPending  = "pending"
	ReviewStatusApproved = "approved"
	ReviewStatusRejected = "rejected"
	ReviewStatusModified = "modified"
)

// ReviewItem 类型常量
const (
	ReviewItemEntity   = "entity"
	ReviewItemRelation = "relation"
)
