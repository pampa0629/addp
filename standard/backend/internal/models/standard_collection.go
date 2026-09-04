package models

import "time"

const (
	CollectionAssignmentOwner      = "owner"
	CollectionAssignmentMaintainer = "maintainer"
	CollectionAssignmentReviewer   = "reviewer"

	CollectionMemberElement  = "element"
	CollectionMemberCodeSet  = "code_set"
	CollectionMemberMetric   = "metric"
	CollectionMemberGlossary = "glossary"
	CollectionMemberDocument = "document"

	CollectionEventCreated             = "created"
	CollectionEventDraftCreated        = "draft_created"
	CollectionEventDraftUpdated        = "draft_updated"
	CollectionEventSubmitted           = "submitted"
	CollectionEventReturned            = "returned"
	CollectionEventPublished           = "published"
	CollectionEventAssignmentsReplaced = "assignments_replaced"
)

// StandardCollection 是标准集稳定身份。名称、说明和成员清单只存在于修订中。
type StandardCollection struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID        int64     `gorm:"not null;index;uniqueIndex:uq_standard_collections_tenant_code" json:"tenant_id"`
	Code            string    `gorm:"size:100;not null;uniqueIndex:uq_standard_collections_tenant_code" json:"code"`
	DraftRevisionID *int64    `gorm:"index" json:"draft_revision_id,omitempty"`
	CreatedBy       int64     `gorm:"not null" json:"created_by"`
	UpdatedBy       *int64    `json:"updated_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Version         int64     `gorm:"not null;default:1" json:"version"`
}

func (StandardCollection) TableName() string { return "standard.standard_collections" }

// StandardCollectionRevision 是名称、说明和成员清单的一次不可变发布快照。
type StandardCollectionRevision struct {
	ID            int64                      `gorm:"primaryKey;autoIncrement" json:"id"`
	CollectionID  int64                      `gorm:"not null;index;uniqueIndex:uq_standard_collection_revisions_collection_no" json:"collection_id"`
	RevisionNo    int64                      `gorm:"not null;uniqueIndex:uq_standard_collection_revisions_collection_no" json:"revision_no"`
	Status        string                     `gorm:"size:20;not null" json:"status" enums:"draft,in_review,published,withdrawn"`
	Name          string                     `gorm:"size:200;not null" json:"name"`
	Description   string                     `gorm:"type:text;not null" json:"description"`
	ChangeSummary string                     `gorm:"type:text;not null" json:"change_summary"`
	SubmittedBy   *int64                     `json:"submitted_by,omitempty"`
	SubmittedAt   *time.Time                 `json:"submitted_at,omitempty"`
	PublishedBy   *int64                     `json:"published_by,omitempty"`
	PublishedAt   *time.Time                 `json:"published_at,omitempty"`
	CreatedBy     int64                      `gorm:"not null" json:"created_by"`
	UpdatedBy     *int64                     `json:"updated_by,omitempty"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
	Members       []StandardCollectionMember `gorm:"-" json:"members"`
}

func (StandardCollectionRevision) TableName() string { return "standard.standard_collection_revisions" }

type StandardCollectionMember struct {
	ID                   int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CollectionRevisionID int64     `gorm:"not null;index;uniqueIndex:uq_standard_collection_members_revision_member" json:"collection_revision_id"`
	MemberType           string    `gorm:"size:30;not null;uniqueIndex:uq_standard_collection_members_revision_member" json:"member_type" enums:"element,code_set,metric,glossary,document"`
	MemberID             int64     `gorm:"not null;uniqueIndex:uq_standard_collection_members_revision_member" json:"member_id"`
	CreatedBy            int64     `gorm:"not null" json:"created_by"`
	CreatedAt            time.Time `json:"created_at"`
	Name                 string    `gorm:"-" json:"name,omitempty"`
	Code                 string    `gorm:"-" json:"code,omitempty"`
}

func (StandardCollectionMember) TableName() string { return "standard.standard_collection_members" }

type StandardCollectionAssignment struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CollectionID  int64     `gorm:"not null;index;uniqueIndex:uq_standard_collection_assignments_collection_principal_role" json:"collection_id"`
	PrincipalID   int64     `gorm:"not null;uniqueIndex:uq_standard_collection_assignments_collection_principal_role" json:"principal_id"`
	Role          string    `gorm:"size:20;not null;uniqueIndex:uq_standard_collection_assignments_collection_principal_role" json:"role" enums:"owner,maintainer,reviewer"`
	CreatedBy     int64     `gorm:"not null" json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	PrincipalName string    `gorm:"-" json:"principal_name,omitempty"`
	PrincipalCode string    `gorm:"-" json:"principal_code,omitempty"`
	Referenceable bool      `gorm:"-" json:"referenceable"`
}

func (StandardCollectionAssignment) TableName() string {
	return "standard.standard_collection_assignments"
}

// StandardCollectionEvent 是审核状态和职责分配变化的不可变审计事实。
type StandardCollectionEvent struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	CollectionID  int64     `gorm:"not null;index" json:"collection_id"`
	RevisionID    *int64    `gorm:"index" json:"revision_id,omitempty"`
	EventType     string    `gorm:"size:30;not null;index" json:"event_type" enums:"created,draft_created,draft_updated,submitted,returned,published,assignments_replaced"`
	ActorID       int64     `gorm:"not null;index" json:"actor_id"`
	Detail        JSONB     `gorm:"type:jsonb;serializer:json;not null" json:"detail"`
	CreatedAt     time.Time `json:"created_at"`
	ActorName     string    `gorm:"-" json:"actor_name,omitempty"`
	ActorCode     string    `gorm:"-" json:"actor_code,omitempty"`
	Referenceable bool      `gorm:"-" json:"referenceable"`
}

func (StandardCollectionEvent) TableName() string { return "standard.standard_collection_events" }

type StandardCollectionAggregate struct {
	StandardCollection
	CurrentRevision *StandardCollectionRevision    `json:"current_revision,omitempty"`
	DraftRevision   *StandardCollectionRevision    `json:"draft_revision,omitempty"`
	Assignments     []StandardCollectionAssignment `json:"assignments"`
	MyRoles         []string                       `json:"my_roles"`
}

type StandardCollectionMemberInput struct {
	MemberType string `json:"member_type" binding:"required" enums:"element,code_set,metric,glossary,document"`
	MemberID   int64  `json:"member_id" binding:"required,gt=0" minimum:"1"`
}

type CreateStandardCollectionRequest struct {
	Code          string                          `json:"code" binding:"required"`
	Name          string                          `json:"name" binding:"required"`
	Description   string                          `json:"description" binding:"required"`
	ChangeSummary string                          `json:"change_summary" binding:"required"`
	Members       []StandardCollectionMemberInput `json:"members"`
}

type UpdateStandardCollectionRevisionRequest struct {
	Version       int64                           `json:"version" binding:"required,gt=0" minimum:"1"`
	Name          string                          `json:"name" binding:"required"`
	Description   string                          `json:"description" binding:"required"`
	ChangeSummary string                          `json:"change_summary" binding:"required"`
	Members       []StandardCollectionMemberInput `json:"members"`
}

type CreateStandardCollectionRevisionRequest struct {
	Version       int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	ChangeSummary string `json:"change_summary" binding:"required"`
}

type StandardCollectionAssignmentInput struct {
	PrincipalID int64  `json:"principal_id" binding:"required,gt=0" minimum:"1"`
	Role        string `json:"role" binding:"required" enums:"owner,maintainer,reviewer"`
}

type ReplaceStandardCollectionAssignmentsRequest struct {
	Version     int64                               `json:"version" binding:"required,gt=0" minimum:"1"`
	Assignments []StandardCollectionAssignmentInput `json:"assignments" binding:"required,min=1"`
}
