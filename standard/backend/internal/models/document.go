package models

import "time"

// Document 是标准来源文档的稳定身份。名称、业务版次和文件只存在于 DocumentRevision。
type Document struct {
	ID              int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        int64       `gorm:"not null;index;uniqueIndex:uq_standard_documents_tenant_code" json:"tenant_id"`
	ScopeType       string      `gorm:"size:20;not null;default:'tenant_common';index" json:"scope_type" enums:"platform,tenant_common,domain"`
	OwnerDomainID   *int64      `gorm:"index" json:"owner_domain_id,omitempty"`
	Code            string      `gorm:"size:100;not null;uniqueIndex:uq_standard_documents_tenant_code" json:"code"`
	DocType         string      `gorm:"column:doc_type;size:50;not null;default:'reference'" json:"doc_type" enums:"national,industry,internal,reference"`
	SourceOrg       string      `gorm:"size:200" json:"source_org"`
	StewardID       *int64      `json:"steward_id,omitempty"`
	Tags            StringArray `gorm:"type:jsonb;serializer:json" json:"tags"`
	DraftRevisionID *int64      `gorm:"index" json:"draft_revision_id,omitempty"`
	CreatedBy       int64       `gorm:"not null" json:"created_by"`
	UpdatedBy       *int64      `json:"updated_by,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	Version         int64       `gorm:"not null;default:1" json:"version"`
	LifecycleState  string      `gorm:"size:16;not null;default:'active'" json:"lifecycle_state"`
}

func (Document) TableName() string { return "standard.documents" }

// DocumentRevision 是标准来源文档的一次完整内容快照。
type DocumentRevision struct {
	ID            int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID    int64      `gorm:"not null;index;uniqueIndex:uq_standard_document_revisions_document_no" json:"document_id"`
	RevisionNo    int64      `gorm:"not null;uniqueIndex:uq_standard_document_revisions_document_no" json:"revision_no"`
	Status        string     `gorm:"size:20;not null" json:"status" enums:"draft,in_review,published,withdrawn"`
	Name          string     `gorm:"size:200;not null" json:"name"`
	VersionLabel  string     `gorm:"size:50" json:"version_label"`
	PublishDate   *time.Time `gorm:"type:date" json:"publish_date,omitempty"`
	Description   string     `gorm:"type:text" json:"description"`
	FileKey       string     `gorm:"type:text" json:"file_key"`
	FileName      string     `gorm:"size:255" json:"file_name"`
	FileSize      int64      `json:"file_size"`
	MediaType     string     `gorm:"size:150" json:"media_type"`
	ContentSHA256 string     `gorm:"size:64" json:"content_sha256"`
	ChangeSummary string     `gorm:"type:text;not null" json:"change_summary"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
	SubmittedBy   *int64     `json:"submitted_by,omitempty"`
	SubmittedAt   *time.Time `json:"submitted_at,omitempty"`
	PublishedBy   *int64     `json:"published_by,omitempty"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	CreatedBy     int64      `gorm:"not null" json:"created_by"`
	UpdatedBy     *int64     `json:"updated_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (DocumentRevision) TableName() string { return "standard.document_revisions" }

type DocumentAggregate struct {
	Document
	CurrentRevision       *DocumentRevision `json:"current_revision,omitempty"`
	DraftRevision         *DocumentRevision `json:"draft_revision,omitempty"`
	HasPublicationHistory bool              `json:"has_publication_history"`
}

// DocumentExtraction 保存一次以确定文档修订为输入的 Copilot 提炼批次。
type DocumentExtraction struct {
	ID                 int64                         `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           int64                         `gorm:"not null;index" json:"tenant_id"`
	DocumentRevisionID int64                         `gorm:"not null;index" json:"document_revision_id"`
	Status             string                        `gorm:"size:20;not null;default:'completed'" json:"status" enums:"completed"`
	RequestedBy        int64                         `gorm:"not null" json:"requested_by"`
	CreatedAt          time.Time                     `json:"created_at"`
	Candidates         []DocumentExtractionCandidate `gorm:"foreignKey:ExtractionID;constraint:OnDelete:CASCADE" json:"candidates,omitempty"`
}

func (DocumentExtraction) TableName() string { return "standard.document_extractions" }

type DocumentExtractionCandidate struct {
	ID            int64                        `gorm:"primaryKey;autoIncrement" json:"id"`
	ExtractionID  int64                        `gorm:"not null;index" json:"extraction_id"`
	CandidateType string                       `gorm:"size:20;not null;index" json:"candidate_type" enums:"glossary,element,code_set,metric"`
	Code          string                       `gorm:"size:100;not null" json:"code"`
	Name          string                       `gorm:"size:200;not null" json:"name"`
	Definition    string                       `gorm:"type:text;not null" json:"definition"`
	Payload       JSONB                        `gorm:"type:jsonb;serializer:json" json:"payload" swaggertype:"object"`
	Status        string                       `gorm:"size:20;not null;default:'pending';index" json:"status" enums:"pending,retained,rejected"`
	Version       int64                        `gorm:"not null;default:1" json:"version"`
	ReviewedBy    *int64                       `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time                   `json:"reviewed_at,omitempty"`
	CreatedAt     time.Time                    `json:"created_at"`
	UpdatedAt     time.Time                    `json:"updated_at"`
	Evidences     []DocumentExtractionEvidence `gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE" json:"evidences,omitempty"`
}

func (DocumentExtractionCandidate) TableName() string {
	return "standard.document_extraction_candidates"
}

type DocumentExtractionEvidence struct {
	ID                 int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CandidateID        int64     `gorm:"not null;index" json:"candidate_id"`
	DocumentRevisionID int64     `gorm:"not null;index" json:"document_revision_id"`
	SectionPath        string    `gorm:"type:text;not null" json:"section_path"`
	StartLine          int       `gorm:"not null" json:"start_line"`
	EndLine            int       `gorm:"not null" json:"end_line"`
	Excerpt            string    `gorm:"type:text;not null" json:"excerpt"`
	ExcerptHash        string    `gorm:"size:64;not null" json:"excerpt_hash"`
	CreatedAt          time.Time `json:"created_at"`
}

func (DocumentExtractionEvidence) TableName() string { return "standard.document_extraction_evidences" }

type DocumentFileCleanup struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	ObjectKey     string    `gorm:"type:text;not null;uniqueIndex:uq_standard_document_file_cleanups_object_key" json:"object_key"`
	Attempts      int       `gorm:"not null;default:0" json:"attempts"`
	NextAttemptAt time.Time `gorm:"not null;index" json:"next_attempt_at"`
	LastError     string    `gorm:"type:text" json:"last_error"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (DocumentFileCleanup) TableName() string { return "standard.document_file_cleanups" }

type DocumentElementMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_document_element_mappings_document_element" json:"document_id"`
	ElementID         int64     `gorm:"not null;uniqueIndex:uq_standard_document_element_mappings_document_element" json:"element_id"`
	ReferenceLocation string    `gorm:"type:text" json:"reference_location"`
	Name              string    `gorm:"->" json:"name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (DocumentElementMapping) TableName() string { return "standard.document_element_mappings" }

type DocumentGlossaryMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_document_glossary_mappings_document_glossary" json:"document_id"`
	GlossaryID        int64     `gorm:"not null;uniqueIndex:uq_standard_document_glossary_mappings_document_glossary" json:"glossary_id"`
	ReferenceLocation string    `gorm:"type:text" json:"reference_location"`
	Name              string    `gorm:"->" json:"name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (DocumentGlossaryMapping) TableName() string { return "standard.document_glossary_mappings" }

type DocumentMetricMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DocumentID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_document_metric_mappings_document_metric" json:"document_id"`
	MetricID          int64     `gorm:"not null;uniqueIndex:uq_standard_document_metric_mappings_document_metric" json:"metric_id"`
	ReferenceLocation string    `gorm:"type:text" json:"reference_location"`
	Name              string    `gorm:"->" json:"name"`
	CreatedAt         time.Time `json:"created_at"`
}

func (DocumentMetricMapping) TableName() string { return "standard.document_metric_mappings" }

type CreateDocumentRequest struct {
	ScopeType     string     `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64     `json:"owner_domain_id,omitempty"`
	Code          string     `json:"code" binding:"required"`
	DocType       string     `json:"doc_type" binding:"required" enums:"national,industry,internal,reference"`
	SourceOrg     string     `json:"source_org"`
	StewardID     *int64     `json:"steward_id,omitempty"`
	Tags          []string   `json:"tags"`
	Name          string     `json:"name" binding:"required"`
	VersionLabel  string     `json:"version_label"`
	PublishDate   *time.Time `json:"publish_date,omitempty"`
	Description   string     `json:"description"`
	ChangeSummary string     `json:"change_summary" binding:"required"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

type UpdateDocumentRequest struct {
	Version       int64    `json:"version" binding:"required,gt=0" minimum:"1"`
	ScopeType     string   `json:"scope_type" binding:"required" enums:"tenant_common,domain"`
	OwnerDomainID *int64   `json:"owner_domain_id,omitempty"`
	DocType       string   `json:"doc_type" binding:"required" enums:"national,industry,internal,reference"`
	SourceOrg     string   `json:"source_org"`
	StewardID     *int64   `json:"steward_id,omitempty"`
	Tags          []string `json:"tags"`
}

type CreateDocumentRevisionRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
}

type UpdateDocumentRevisionRequest struct {
	Version       int64      `json:"version" binding:"required,gt=0" minimum:"1"`
	Name          string     `json:"name" binding:"required"`
	VersionLabel  string     `json:"version_label"`
	PublishDate   *time.Time `json:"publish_date,omitempty"`
	Description   string     `json:"description"`
	ChangeSummary string     `json:"change_summary" binding:"required"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`
}

type CreateDocumentExtractionRequest struct {
	Version int64 `json:"version" binding:"required,gt=0" minimum:"1"`
}
type UpdateDocumentExtractionCandidateRequest struct {
	Version int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	Status  string `json:"status" binding:"required" enums:"retained,rejected"`
}

type SetDocumentMappingsRequest struct {
	Version     int64             `json:"version" binding:"required"`
	ElementIDs  []int64           `json:"element_ids"`
	GlossaryIDs []int64           `json:"glossary_ids"`
	MetricIDs   []int64           `json:"metric_ids"`
	Locations   map[string]string `json:"locations"`
}

type CreateLinkedDocumentRequest struct {
	CreateDocumentRequest
	Version int64 `json:"version" binding:"required"`
}
type LinkDocumentRequest struct {
	DocID   int64 `json:"doc_id" binding:"required"`
	Version int64 `json:"version" binding:"required"`
}
type ResourceVersionResponse struct {
	Version int64 `json:"version"`
}
type LinkedDocumentMutationResponse struct {
	Document *DocumentAggregate `json:"document"`
	Version  int64              `json:"version"`
}
