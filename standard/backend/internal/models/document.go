package models

import "time"

// Document 标准文档
type Document struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64      `gorm:"not null;index" json:"tenant_id"`
	Name        string     `gorm:"size:200;not null" json:"name"`
	DocType     string     `gorm:"column:doc_type;size:50;default:'reference'" json:"doc_type"` // national/industry/internal/reference
	SourceOrg   string     `gorm:"size:200" json:"source_org"`
	Version     string     `gorm:"size:50" json:"version"`
	PublishDate *time.Time `json:"publish_date,omitempty"`
	Description string     `gorm:"type:text" json:"description"`
	FileKey     string     `gorm:"type:text" json:"file_key"` // MinIO 存储路径
	FileName    string     `gorm:"size:200" json:"file_name"`
	FileSize    int64      `json:"file_size"`
	CreatedBy   int64      `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64     `json:"updated_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// DocumentFileCleanup records obsolete objects that still require physical deletion.
type DocumentFileCleanup struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ObjectKey     string    `gorm:"type:text;not null;uniqueIndex:uq_standard_document_file_cleanups_object_key" json:"object_key"`
	Attempts      int       `gorm:"not null;default:0" json:"attempts"`
	NextAttemptAt time.Time `gorm:"not null;index" json:"next_attempt_at"`
	LastError     string    `gorm:"type:text" json:"last_error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (DocumentFileCleanup) TableName() string {
	return "standard.document_file_cleanups"
}

func (Document) TableName() string {
	return "standard.documents"
}

// DocumentElementMapping 文档与数据元关联
type DocumentElementMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_document_element_mappings_document_element" json:"document_id"`
	ElementID         int64     `gorm:"not null;uniqueIndex:uq_standard_document_element_mappings_document_element" json:"element_id"`
	ReferenceLocation string    `gorm:"type:text" json:"reference_location"`
	Name              string    `gorm:"->" json:"name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (DocumentElementMapping) TableName() string {
	return "standard.document_element_mappings"
}

// DocumentGlossaryMapping 文档与术语关联
type DocumentGlossaryMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_document_glossary_mappings_document_glossary" json:"document_id"`
	GlossaryID        int64     `gorm:"not null;uniqueIndex:uq_standard_document_glossary_mappings_document_glossary" json:"glossary_id"`
	ReferenceLocation string    `gorm:"type:text" json:"reference_location"`
	Name              string    `gorm:"->" json:"name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (DocumentGlossaryMapping) TableName() string {
	return "standard.document_glossary_mappings"
}

// DocumentMetricMapping 文档与指标关联
type DocumentMetricMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_document_metric_mappings_document_metric" json:"document_id"`
	MetricID          int64     `gorm:"not null;uniqueIndex:uq_standard_document_metric_mappings_document_metric" json:"metric_id"`
	ReferenceLocation string    `gorm:"type:text" json:"reference_location"`
	Name              string    `gorm:"->" json:"name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (DocumentMetricMapping) TableName() string {
	return "standard.document_metric_mappings"
}

// CreateDocumentRequest 创建文档请求
type CreateDocumentRequest struct {
	Name        string `json:"name" binding:"required"`
	DocType     string `json:"doc_type"`
	SourceOrg   string `json:"source_org"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// UpdateDocumentRequest 更新文档请求
type UpdateDocumentRequest struct {
	Name        string `json:"name"`
	DocType     string `json:"doc_type"`
	SourceOrg   string `json:"source_org"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// SetDocumentMappingsRequest 设置文档关联请求
type SetDocumentMappingsRequest struct {
	ElementIDs  []int64           `json:"element_ids"`
	GlossaryIDs []int64           `json:"glossary_ids"`
	MetricIDs   []int64           `json:"metric_ids"`
	Locations   map[string]string `json:"locations"` // key: "element_1" / "glossary_2" / "metric_3"
}
