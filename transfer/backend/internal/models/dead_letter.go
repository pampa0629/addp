package models

import "time"

// DeadLetter 是业务 Kafka 记录被显式审计跳过前写入 Infra PostgreSQL 的控制索引。
// 原始 key/value/headers 仅保存在 payload_topic 指向的 Infra Kafka record 中。
type DeadLetter struct {
	Identity         string     `gorm:"type:uuid;primaryKey" json:"identity"`
	TenantID         uint       `gorm:"not null;index:idx_transfer_dead_letters_task_observed,priority:1;index:idx_transfer_dead_letters_error,priority:1" json:"tenant_id"`
	TaskID           uint       `gorm:"not null;index:idx_transfer_dead_letters_task_observed,priority:2" json:"task_id"`
	ApplyIdentity    string     `gorm:"type:uuid;not null;uniqueIndex:uq_transfer_dead_letters_source_record,priority:1" json:"apply_identity"`
	FirstExecutionID string     `gorm:"type:varchar(255);not null" json:"first_execution_id"`
	LastExecutionID  string     `gorm:"type:varchar(255);not null" json:"last_execution_id"`
	SourceIdentity   string     `gorm:"type:text;not null;uniqueIndex:uq_transfer_dead_letters_source_record,priority:2" json:"source_identity"`
	SourceTopic      string     `gorm:"type:varchar(249);not null" json:"source_topic"`
	SourcePartition  string     `gorm:"type:varchar(255);not null;uniqueIndex:uq_transfer_dead_letters_source_record,priority:3" json:"source_partition"`
	SourceOffset     int64      `gorm:"not null;uniqueIndex:uq_transfer_dead_letters_source_record,priority:4" json:"source_offset"`
	SourceTimestamp  *time.Time `json:"source_timestamp,omitempty"`
	ErrorCode        string     `gorm:"type:varchar(128);not null;index:idx_transfer_dead_letters_error,priority:3" json:"error_code"`
	ErrorCategory    string     `gorm:"type:varchar(128);not null;index:idx_transfer_dead_letters_error,priority:2" json:"error_category"`
	ErrorMessage     string     `gorm:"type:text;not null" json:"error_message"`
	PayloadTopic     string     `gorm:"type:varchar(249);not null" json:"payload_topic"`
	PayloadPartition int32      `gorm:"not null" json:"payload_partition"`
	PayloadOffset    int64      `gorm:"not null" json:"payload_offset"`
	PayloadAvailable bool       `gorm:"not null;default:true" json:"payload_available"`
	FirstObservedAt  time.Time  `gorm:"not null" json:"first_observed_at"`
	LastObservedAt   time.Time  `gorm:"not null;index:idx_transfer_dead_letters_task_observed,priority:3,sort:desc" json:"last_observed_at"`
	OccurrenceCount  uint64     `gorm:"not null;default:1" json:"occurrence_count"`
	CreatedAt        time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

func (DeadLetter) TableName() string { return "transfer.dead_letters" }

// DeadLetterPayloadReference 是 availability reconciler 使用的当前 Infra Kafka 引用快照。
type DeadLetterPayloadReference struct {
	Identity  string
	Topic     string
	Partition int32
	Offset    int64
}

// DeadLetterListRequest 是 owner task 下的 DLQ 控制索引查询条件。
type DeadLetterListRequest struct {
	Page             int    `form:"page"`
	PageSize         int    `form:"page_size"`
	SourcePartition  string `form:"source_partition"`
	ErrorCategory    string `form:"error_category"`
	ErrorCode        string `form:"error_code"`
	PayloadAvailable *bool  `form:"payload_available"`
}

// DeadLetterView 是公开 API 返回的安全控制索引，不包含 Infra Kafka payload reference。
type DeadLetterView struct {
	Identity         string     `json:"identity"`
	FirstExecutionID string     `json:"first_execution_id"`
	LastExecutionID  string     `json:"last_execution_id"`
	SourceTopic      string     `json:"source_topic"`
	SourcePartition  string     `json:"source_partition"`
	SourceOffset     int64      `json:"source_offset"`
	SourceTimestamp  *time.Time `json:"source_timestamp,omitempty"`
	ErrorCode        string     `json:"error_code"`
	ErrorCategory    string     `json:"error_category"`
	ErrorMessage     string     `json:"error_message"`
	PayloadAvailable bool       `json:"payload_available"`
	FirstObservedAt  time.Time  `json:"first_observed_at"`
	LastObservedAt   time.Time  `json:"last_observed_at"`
	OccurrenceCount  uint64     `json:"occurrence_count"`
}

func NewDeadLetterView(deadLetter DeadLetter) DeadLetterView {
	return DeadLetterView{
		Identity: deadLetter.Identity, FirstExecutionID: deadLetter.FirstExecutionID, LastExecutionID: deadLetter.LastExecutionID,
		SourceTopic: deadLetter.SourceTopic, SourcePartition: deadLetter.SourcePartition, SourceOffset: deadLetter.SourceOffset,
		SourceTimestamp: deadLetter.SourceTimestamp, ErrorCode: deadLetter.ErrorCode, ErrorCategory: deadLetter.ErrorCategory,
		ErrorMessage: deadLetter.ErrorMessage, PayloadAvailable: deadLetter.PayloadAvailable,
		FirstObservedAt: deadLetter.FirstObservedAt, LastObservedAt: deadLetter.LastObservedAt, OccurrenceCount: deadLetter.OccurrenceCount,
	}
}

// DeadLetterListResponse 仅用于 Swagger 描述统一分页响应。
type DeadLetterListResponse struct {
	Data       []DeadLetterView `json:"data"`
	Total      int64            `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	TotalPages int              `json:"total_pages"`
}
