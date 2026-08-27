package models

import (
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

const (
	EntryTypeDataItem            = "data_item"
	EntryTypeBusinessEntity      = "business_entity"
	EntryTypeLogicalModel        = "logical_model"
	EntryTypeMetric              = "metric"
	EntryTypeDataService         = "data_service"
	EntryTypeDevelopmentArtifact = "development_artifact"

	EntryStatusActive = "active"
	EntryStatusMerged = "merged"

	GovernanceStatusDiscovered = "discovered"
	GovernanceStatusCurated    = "curated"
	GovernanceStatusCertified  = "certified"
	GovernanceStatusDeprecated = "deprecated"

	VisibilityInventory  = "inventory"
	VisibilityDepartment = "department"
	VisibilityTenant     = "tenant"

	SourceModuleMeta       = "meta"
	SourceModuleModel      = "model"
	SourceModuleStandard   = "standard"
	SourceModuleService    = "service"
	SourceModuleDevelop    = "develop"
	SourceTypeDataItem     = "data_item"
	SourceTypeEntity       = "entity"
	SourceTypeLogicalTable = "logical_table"
	SourceTypeMetric       = "metric"
	SourceTypeQueryService = "query_service"
	SourceTypeDevTask      = "dev_task"
	SourceStatusActive     = "active"
	SourceStatusMissing    = "missing"

	SemanticTypeDomain    = "domain"
	SemanticTypeGlossary  = "glossary"
	SemanticRolePrimary   = "primary"
	SemanticRoleSecondary = "secondary"
	SemanticRoleApplies   = "applies"

	ResponsibilityRoleAccountableDepartment = "accountable_department"
	ResponsibilityRoleBusinessOwner         = "business_owner"
	ResponsibilityRoleDataSteward           = "data_steward"
	ResponsibilityRoleTechnicalOwner        = "technical_owner"
	ResponsibilitySubjectDepartment         = "department"
	ResponsibilitySubjectUser               = "user"
	ResponsibilityStatusActive              = "active"
	ResponsibilityStatusNeedsTransfer       = "needs_transfer"

	GovernanceTaskTypeResponsibilityTransfer       = "responsibility_transfer"
	GovernanceTaskStatusOpen                       = "open"
	GovernanceTaskStatusResolved                   = "resolved"
	GovernanceTaskReasonSubjectNotFound            = "subject_not_found"
	GovernanceTaskReasonSubjectNotReferenceable    = "subject_not_referenceable"
	GovernanceTaskResolutionReferenceRestored      = "reference_restored"
	GovernanceTaskResolutionResponsibilityReplaced = "responsibility_replaced"

	EntryMarkTypeFavorite  = "favorite"
	EntryMarkTypeFollowing = "following"
)

type Entry struct {
	ID                          uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID                    int64      `gorm:"not null;index" json:"-"`
	EntryType                   string     `gorm:"size:32;not null" json:"entry_type"`
	EntryStatus                 string     `gorm:"size:16;not null" json:"entry_status"`
	MergedIntoEntryID           *uuid.UUID `gorm:"type:uuid" json:"merged_into_entry_id,omitempty"`
	RecommendedSuccessorEntryID *uuid.UUID `gorm:"type:uuid" json:"recommended_successor_entry_id,omitempty"`
	BusinessName                *string    `gorm:"type:text" json:"business_name,omitempty"`
	BusinessDescription         *string    `gorm:"type:text" json:"business_description,omitempty"`
	GovernanceStatus            string     `gorm:"size:16;not null;index" json:"governance_status"`
	Visibility                  string     `gorm:"size:16;not null;index" json:"visibility"`
	Version                     int64      `gorm:"not null" json:"version"`
	CreatedAt                   time.Time  `json:"created_at"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

func (Entry) TableName() string { return "catalog.entries" }

type SourceBinding struct {
	ID                uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID          int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID    uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	SourceModule      string               `gorm:"size:32;not null" json:"source_module"`
	SourceType        string               `gorm:"size:32;not null" json:"source_type"`
	SourceIdentity    string               `gorm:"size:255;not null" json:"source_identity"`
	SourceStatus      string               `gorm:"size:16;not null;index" json:"source_status"`
	SourceVersion     string               `gorm:"size:20;not null" json:"source_version"`
	IsCurrent         bool                 `gorm:"not null;default:true" json:"is_current"`
	BoundAt           time.Time            `gorm:"not null" json:"bound_at"`
	MissingAt         *time.Time           `json:"missing_at,omitempty"`
	ReplacedBindingID *uuid.UUID           `gorm:"type:uuid" json:"replaced_binding_id,omitempty"`
	MissingReason     *string              `gorm:"size:64" json:"missing_reason,omitempty"`
	ObservedSnapshot  commonModels.JSONMap `gorm:"type:jsonb;not null" json:"observed_snapshot"`
	ObservedAt        time.Time            `gorm:"not null" json:"observed_at"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

func (SourceBinding) TableName() string { return "catalog.source_bindings" }

type Component struct {
	ID               uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID         int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID   uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	ComponentKey     string               `gorm:"type:text;not null" json:"component_key"`
	DisplayName      string               `gorm:"type:text;not null" json:"display_name"`
	DataType         string               `gorm:"type:text" json:"data_type,omitempty"`
	ComponentStatus  string               `gorm:"size:16;not null" json:"component_status"`
	Ordinal          int                  `gorm:"not null;default:0" json:"ordinal"`
	ObservedSnapshot commonModels.JSONMap `gorm:"type:jsonb;not null" json:"observed_snapshot"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

func (Component) TableName() string { return "catalog.components" }

type Responsibility struct {
	ID               uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID         int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID   uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	Role             string               `gorm:"size:32;not null" json:"role"`
	SubjectType      string               `gorm:"size:32;not null" json:"subject_type"`
	SubjectID        int64                `gorm:"not null" json:"subject_id,string" swaggertype:"string"`
	Status           string               `gorm:"size:16;not null;default:'active'" json:"status"`
	ObservedSnapshot commonModels.JSONMap `gorm:"type:jsonb;not null" json:"observed_snapshot"`
	VerifiedAt       time.Time            `gorm:"not null" json:"verified_at"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

func (Responsibility) TableName() string { return "catalog.responsibilities" }

type GovernanceTask struct {
	ID                 uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID           int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID     uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	TaskType           string               `gorm:"size:48;not null" json:"task_type"`
	ResponsibilityRole string               `gorm:"size:32;not null" json:"responsibility_role"`
	SubjectType        string               `gorm:"size:32;not null" json:"subject_type"`
	SubjectID          int64                `gorm:"not null" json:"subject_id,string" swaggertype:"string"`
	Status             string               `gorm:"size:16;not null;index" json:"status"`
	Reason             string               `gorm:"size:64;not null" json:"reason"`
	ObservedSnapshot   commonModels.JSONMap `gorm:"type:jsonb;not null" json:"observed_snapshot"`
	OpenedAt           time.Time            `gorm:"not null" json:"opened_at"`
	ResolvedAt         *time.Time           `json:"resolved_at,omitempty"`
	Resolution         *string              `gorm:"size:64" json:"resolution,omitempty"`
	CreatedAt          time.Time            `json:"created_at"`
	UpdatedAt          time.Time            `json:"updated_at"`
}

func (GovernanceTask) TableName() string { return "catalog.governance_tasks" }

type EntryMark struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID       int64     `gorm:"not null;index" json:"-"`
	UserID         int64     `gorm:"not null;index" json:"-"`
	CatalogEntryID uuid.UUID `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	MarkType       string    `gorm:"size:16;not null" json:"mark_type"`
	CreatedAt      time.Time `json:"created_at"`
}

func (EntryMark) TableName() string { return "catalog.entry_marks" }

type Collection struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID       int64     `gorm:"not null;index" json:"-"`
	ProjectGroupID int64     `gorm:"not null;index" json:"project_group_id,string" swaggertype:"string"`
	Name           string    `gorm:"type:text;not null" json:"name"`
	Description    string    `gorm:"type:text;not null" json:"description"`
	Version        int64     `gorm:"not null" json:"version"`
	CreatedBy      int64     `gorm:"not null" json:"created_by,string" swaggertype:"string"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (Collection) TableName() string { return "catalog.collections" }

type CollectionEntry struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID       int64     `gorm:"not null;index" json:"-"`
	CollectionID   uuid.UUID `gorm:"type:uuid;not null;index" json:"collection_id"`
	CatalogEntryID uuid.UUID `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	AddedBy        int64     `gorm:"not null" json:"added_by,string" swaggertype:"string"`
	CreatedAt      time.Time `json:"created_at"`
}

func (CollectionEntry) TableName() string { return "catalog.collection_entries" }

type CollectionAuditEvent struct {
	ID           uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID     int64                `gorm:"not null;index" json:"-"`
	CollectionID uuid.UUID            `gorm:"type:uuid;not null;index" json:"collection_id"`
	EventType    string               `gorm:"size:64;not null" json:"event_type"`
	ActorID      int64                `gorm:"not null" json:"actor_id,string" swaggertype:"string"`
	Details      commonModels.JSONMap `gorm:"type:jsonb;not null" json:"details"`
	CreatedAt    time.Time            `gorm:"not null" json:"created_at"`
}

func (CollectionAuditEvent) TableName() string { return "catalog.collection_audit_events" }

type SemanticAssociation struct {
	ID               uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID         int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID   uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	SemanticType     string               `gorm:"size:16;not null" json:"semantic_type"`
	SemanticID       int64                `gorm:"not null" json:"semantic_id,string" swaggertype:"string"`
	RelationRole     string               `gorm:"size:16;not null" json:"relation_role"`
	ObservedVersion  int64                `gorm:"not null" json:"observed_version"`
	ObservedSnapshot commonModels.JSONMap `gorm:"type:jsonb;not null" json:"observed_snapshot"`
	VerifiedAt       time.Time            `gorm:"not null" json:"verified_at"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

func (SemanticAssociation) TableName() string { return "catalog.semantic_associations" }

type ComponentElementAssociation struct {
	ID               uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID         int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID   uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	ComponentID      uuid.UUID            `gorm:"type:uuid;not null;index" json:"component_id"`
	ElementID        int64                `gorm:"not null" json:"element_id,string" swaggertype:"string"`
	ObservedVersion  int64                `gorm:"not null" json:"observed_version"`
	ObservedSnapshot commonModels.JSONMap `gorm:"type:jsonb;not null" json:"observed_snapshot"`
	VerifiedAt       time.Time            `gorm:"not null" json:"verified_at"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
}

func (ComponentElementAssociation) TableName() string {
	return "catalog.component_element_associations"
}

type SourceCheckpoint struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"-"`
	TenantID     int64     `gorm:"not null" json:"-"`
	SourceModule string    `gorm:"size:32;not null" json:"source_module"`
	FeedName     string    `gorm:"size:64;not null" json:"feed_name"`
	Cursor       string    `gorm:"type:text;not null" json:"cursor"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (SourceCheckpoint) TableName() string { return "catalog.source_checkpoints" }

type ProjectionTask struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"-"`
	TenantID       int64     `gorm:"not null;index" json:"-"`
	CatalogEntryID uuid.UUID `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	Projection     string    `gorm:"size:32;not null" json:"projection"`
	Status         string    `gorm:"size:16;not null;index" json:"status"`
	AttemptCount   int       `gorm:"not null;default:0" json:"attempt_count"`
	AvailableAt    time.Time `gorm:"not null" json:"available_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (ProjectionTask) TableName() string { return "catalog.projection_tasks" }

type AuditEvent struct {
	ID             uuid.UUID            `gorm:"type:uuid;primaryKey" json:"id"`
	TenantID       int64                `gorm:"not null;index" json:"-"`
	CatalogEntryID uuid.UUID            `gorm:"type:uuid;not null;index" json:"catalog_entry_id"`
	EventType      string               `gorm:"size:64;not null" json:"event_type"`
	ActorType      string               `gorm:"size:32;not null" json:"actor_type"`
	ActorID        string               `gorm:"size:255;not null" json:"actor_id"`
	Details        commonModels.JSONMap `gorm:"type:jsonb;not null" json:"details"`
	CreatedAt      time.Time            `json:"created_at"`
}

func (AuditEvent) TableName() string { return "catalog.audit_events" }
