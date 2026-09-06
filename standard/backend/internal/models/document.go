package models

import "time"

const (
	CandidateComparisonNew                = "new"
	CandidateComparisonExact              = "exact"
	CandidateComparisonContentConflict    = "content_conflict"
	CandidateComparisonScopeConflict      = "scope_conflict"
	CandidateGroupStatePending            = "pending"
	CandidateGroupStateRetained           = "retained"
	CandidateGroupStateRejected           = "rejected"
	CandidateGroupStateFormalized         = "formalized"
	CandidateFormalizationCreatedIdentity = "created_identity"
	CandidateFormalizationCreatedRevision = "created_revision"
	CandidateFormalizationLinkedExisting  = "linked_existing"
)

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
	ID            int64                                  `gorm:"primaryKey;autoIncrement" json:"id"`
	ExtractionID  int64                                  `gorm:"not null;index" json:"extraction_id"`
	CandidateType string                                 `gorm:"size:20;not null;index" json:"candidate_type" enums:"glossary,element,code_set,metric"`
	Code          string                                 `gorm:"size:100;not null" json:"code"`
	Name          string                                 `gorm:"size:200;not null" json:"name"`
	Definition    string                                 `gorm:"type:text;not null" json:"definition"`
	Payload       DocumentExtractionCandidatePayload     `gorm:"type:jsonb;serializer:json" json:"payload"`
	Status        string                                 `gorm:"size:20;not null;default:'pending';index" json:"status" enums:"pending,retained,rejected"`
	Version       int64                                  `gorm:"not null;default:1" json:"version"`
	ReviewedBy    *int64                                 `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time                             `json:"reviewed_at,omitempty"`
	CreatedAt     time.Time                              `json:"created_at"`
	UpdatedAt     time.Time                              `json:"updated_at"`
	Evidences     []DocumentExtractionEvidence           `gorm:"foreignKey:CandidateID;constraint:OnDelete:CASCADE" json:"evidences,omitempty"`
	Formalization *DocumentCandidateFormalization        `gorm:"foreignKey:CandidateID;constraint:OnDelete:RESTRICT" json:"formalization,omitempty"`
	Comparison    *DocumentExtractionCandidateComparison `gorm:"-" json:"comparison,omitempty"`
}

func (DocumentExtractionCandidate) TableName() string {
	return "standard.document_extraction_candidates"
}

// DocumentExtractionCandidateOccurrence 保留一个聚合候选在确定提炼批次中的原始出现事实。
type DocumentExtractionCandidateOccurrence struct {
	CandidateID        int64                           `json:"candidate_id"`
	ExtractionID       int64                           `json:"extraction_id"`
	DocumentRevisionID int64                           `json:"document_revision_id"`
	RequestedBy        int64                           `json:"requested_by"`
	ExtractedAt        time.Time                       `json:"extracted_at"`
	Status             string                          `json:"status" enums:"pending,retained,rejected"`
	Version            int64                           `json:"version"`
	ReviewedBy         *int64                          `json:"reviewed_by,omitempty"`
	ReviewedAt         *time.Time                      `json:"reviewed_at,omitempty"`
	Evidences          []DocumentExtractionEvidence    `json:"evidences"`
	Formalization      *DocumentCandidateFormalization `json:"formalization,omitempty"`
}

// DocumentExtractionCandidateGroup 是跨提炼批次按确定性语义指纹生成的只读候选聚合投影。
type DocumentExtractionCandidateGroup struct {
	SemanticFingerprint string                                  `json:"semantic_fingerprint"`
	State               string                                  `json:"state" enums:"pending,retained,rejected,formalized"`
	OccurrenceCount     int                                     `json:"occurrence_count"`
	FirstSeenAt         time.Time                               `json:"first_seen_at"`
	LastSeenAt          time.Time                               `json:"last_seen_at"`
	Candidate           DocumentExtractionCandidate             `json:"candidate"`
	Occurrences         []DocumentExtractionCandidateOccurrence `json:"occurrences"`
}

type DocumentExtractionCandidateGroupStatusCounts struct {
	Pending    int64 `json:"pending"`
	Retained   int64 `json:"retained"`
	Rejected   int64 `json:"rejected"`
	Formalized int64 `json:"formalized"`
}

type PaginatedDocumentExtractionCandidateGroupResponse struct {
	Data         []DocumentExtractionCandidateGroup           `json:"data"`
	Total        int64                                        `json:"total"`
	Page         int                                          `json:"page"`
	PageSize     int                                          `json:"page_size"`
	TotalPages   int                                          `json:"total_pages"`
	StatusCounts DocumentExtractionCandidateGroupStatusCounts `json:"status_counts"`
}

// DocumentCandidateFormalization 是 retained 候选到受治理标准修订的一对一不可变事实。
type DocumentCandidateFormalization struct {
	CandidateID          int64     `gorm:"primaryKey" json:"candidate_id"`
	Action               string    `gorm:"size:24;not null" json:"action" enums:"created_identity,created_revision,linked_existing"`
	StandardID           int64     `gorm:"not null;index" json:"standard_id"`
	StandardCode         string    `gorm:"size:100;not null" json:"standard_code"`
	RevisionID           int64     `gorm:"not null;index" json:"revision_id"`
	RevisionNo           int64     `gorm:"not null" json:"revision_no"`
	TargetRevisionStatus string    `gorm:"size:20;not null" json:"target_revision_status" enums:"draft,in_review,published,withdrawn"`
	ChangeSummary        string    `gorm:"type:text;not null" json:"change_summary"`
	CreatedBy            int64     `gorm:"not null" json:"created_by"`
	CreatedAt            time.Time `json:"created_at"`
}

func (DocumentCandidateFormalization) TableName() string {
	return "standard.document_candidate_formalizations"
}

// DocumentExtractionCandidatePayload 是 Standard 与 Copilot 共用的强类型候选补充契约。
type DocumentExtractionCandidatePayload struct {
	DataType           *string                                  `json:"data_type,omitempty" enums:"string,int,bigint,float,decimal,date,datetime,bool,json,text"`
	ValueDomainKind    *string                                  `json:"value_domain_kind,omitempty" enums:"unrestricted,range,enumeration"`
	CodeSetCode        *string                                  `json:"code_set_code,omitempty"`
	Unit               *string                                  `json:"unit,omitempty"`
	CalculationFormula *string                                  `json:"calculation_formula,omitempty"`
	StatisticalScope   *string                                  `json:"statistical_scope,omitempty"`
	Aggregation        *string                                  `json:"aggregation,omitempty"`
	Dimensions         []string                                 `json:"dimensions,omitempty"`
	Items              []DocumentExtractionCandidatePayloadItem `json:"items,omitempty"`
}

type DocumentExtractionCandidatePayloadItem struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// DocumentExtractionCandidateComparison 是候选与当前同类型、同编码标准的读取时投影，不持久化。
type DocumentExtractionCandidateComparison struct {
	Result         string                                  `json:"result" enums:"new,exact,content_conflict,scope_conflict"`
	StandardID     int64                                   `json:"standard_id,omitempty"`
	Code           string                                  `json:"code,omitempty"`
	Name           string                                  `json:"name,omitempty"`
	ScopeType      string                                  `json:"scope_type,omitempty" enums:"platform,tenant_common,domain"`
	OwnerDomainID  *int64                                  `json:"owner_domain_id,omitempty"`
	RevisionID     int64                                   `json:"revision_id,omitempty"`
	RevisionNo     int64                                   `json:"revision_no,omitempty"`
	RevisionStatus string                                  `json:"revision_status,omitempty" enums:"draft,in_review,published,withdrawn"`
	Differences    []DocumentExtractionCandidateDifference `json:"differences"`
}

// DocumentExtractionCandidateDifference 显式返回一个候选字段与当前标准字段的两侧值。
type DocumentExtractionCandidateDifference struct {
	Field          string                                     `json:"field" enums:"scope_type,owner_domain_id,name,definition,data_type,value_domain_kind,code_set_code,unit,value_type,items,statistical_caliber,semantic_formula"`
	CandidateValue DocumentExtractionCandidateComparisonValue `json:"candidate_value"`
	StandardValue  DocumentExtractionCandidateComparisonValue `json:"standard_value"`
}

// DocumentExtractionCandidateComparisonValue 使用显式判别类型表达字符串、整数、码值项或空值。
type DocumentExtractionCandidateComparisonValue struct {
	Kind    string                                      `json:"kind" enums:"empty,text,integer,code_items"`
	Text    *string                                     `json:"text,omitempty"`
	Integer *int64                                      `json:"integer,omitempty"`
	Items   []DocumentExtractionCandidateComparisonItem `json:"items,omitempty"`
}

type DocumentExtractionCandidateComparisonItem struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
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

type FormalizeDocumentExtractionCandidateRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
	MetricType    string `json:"metric_type,omitempty" enums:"atomic,derived,composite"`
}

type DocumentCandidateFormalizationResponse struct {
	DocumentCandidateFormalization
	CandidateType    string `json:"candidate_type" enums:"glossary,element,code_set,metric"`
	CandidateVersion int64  `json:"candidate_version"`
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
