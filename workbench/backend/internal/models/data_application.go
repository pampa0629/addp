package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

const (
	DataApplicationSnapshotSchemaVersion  = "addp.workbench_data_application/v1"
	PublicationStatusUnpublished          = "unpublished"
	PublicationStatusPublished            = "published"
	PublicationStatusOffline              = "offline"
	ApplicationDisplayModeDesktop         = "desktop"
	ApplicationDisplayModeWallboard       = "wallboard"
	ApplicationRefreshIntervalDisabled    = 0
	ApplicationRefreshInterval30Seconds   = 30
	ApplicationRefreshInterval60Seconds   = 60
	ApplicationRefreshInterval300Seconds  = 300
	ApplicationVisibleSectionTitle        = "title"
	ApplicationVisibleSectionParameters   = "parameters"
	ApplicationVisibleSectionQueryActions = "query_actions"
)

type DataApplication struct {
	ID                    string         `gorm:"type:uuid;primaryKey"`
	TenantID              int64          `gorm:"not null;index:idx_workbench_application_owner,priority:1"`
	OwnerUserID           int64          `gorm:"not null;index:idx_workbench_application_owner,priority:2"`
	Name                  string         `gorm:"type:varchar(200);not null"`
	Description           string         `gorm:"type:text;not null"`
	DraftSnapshot         datatypes.JSON `gorm:"type:jsonb;not null"`
	DraftContentHash      string         `gorm:"type:varchar(71);not null"`
	PublicationStatus     string         `gorm:"type:varchar(32);not null"`
	CurrentRevisionNumber *int64
	CurrentRevisionHash   string `gorm:"type:varchar(71);not null"`
	Version               int64  `gorm:"not null"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (DataApplication) TableName() string { return "workbench.data_applications" }

type DataApplicationRevision struct {
	ID             string         `gorm:"type:uuid;primaryKey"`
	ApplicationID  string         `gorm:"type:uuid;not null;uniqueIndex:ux_workbench_application_revision,priority:1"`
	TenantID       int64          `gorm:"not null;index"`
	RevisionNumber int64          `gorm:"not null;uniqueIndex:ux_workbench_application_revision,priority:2"`
	Name           string         `gorm:"type:varchar(200);not null"`
	Description    string         `gorm:"type:text;not null"`
	Snapshot       datatypes.JSON `gorm:"type:jsonb;not null"`
	ContentHash    string         `gorm:"type:varchar(71);not null"`
	PublishedBy    int64          `gorm:"not null"`
	PublishedAt    time.Time      `gorm:"not null"`
}

func (DataApplicationRevision) TableName() string {
	return "workbench.data_application_revisions"
}

type DataApplicationSnapshot struct {
	SchemaVersion     string                            `json:"schema_version" binding:"required"`
	Page              DataApplicationPage               `json:"page" binding:"required"`
	Components        []DataApplicationComponent        `json:"components" binding:"required"`
	Parameters        []DataApplicationParameter        `json:"parameters"`
	ParameterBindings []DataApplicationParameterBinding `json:"parameter_bindings"`
	SelectionBindings []DataApplicationSelectionBinding `json:"selection_bindings"`
}

type DataApplicationPage struct {
	ID                     string                           `json:"id" binding:"required"`
	Title                  string                           `json:"title" binding:"required"`
	DisplayMode            string                           `json:"display_mode" binding:"required" enums:"desktop,wallboard"`
	RefreshIntervalSeconds *int                             `json:"refresh_interval_seconds" binding:"required" enums:"0,30,60,300"`
	VisibleSections        []string                         `json:"visible_sections" binding:"required" enums:"title,parameters,query_actions"`
	Placements             []DataApplicationComponentLayout `json:"placements" binding:"required"`
}

type DataApplicationComponentLayout struct {
	ComponentID string `json:"component_id" binding:"required"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Width       int    `json:"width" binding:"required"`
	Height      int    `json:"height" binding:"required"`
}

type DataApplicationComponent struct {
	ID                     string                     `json:"id" binding:"required"`
	Title                  string                     `json:"title" binding:"required"`
	Description            string                     `json:"description"`
	ServiceRef             ServiceReference           `json:"service_ref" binding:"required"`
	ContractFingerprint    string                     `json:"contract_fingerprint" binding:"required"`
	ParameterDefinitions   []ViewParameterDefinition  `json:"parameter_definitions"`
	QueryTemplate          ViewQueryTemplate          `json:"query_template" binding:"required"`
	DefaultParameterValues map[string]json.RawMessage `json:"default_parameter_values"`
	RendererType           string                     `json:"renderer_type" binding:"required" enums:"table,chart,map"`
	RendererConfig         json.RawMessage            `json:"renderer_config" binding:"required" swaggertype:"object"`
}

type DataApplicationParameter struct {
	Key          string          `json:"key" binding:"required"`
	Label        string          `json:"label" binding:"required"`
	ControlType  string          `json:"control_type" binding:"required"`
	Required     bool            `json:"required"`
	DefaultValue json.RawMessage `json:"default_value,omitempty" swaggertype:"object"`
}

type DataApplicationParameterBinding struct {
	ApplicationParameterKey string `json:"application_parameter_key" binding:"required"`
	ComponentID             string `json:"component_id" binding:"required"`
	ComponentParameterKey   string `json:"component_parameter_key" binding:"required"`
}

type DataApplicationSelectionBinding struct {
	SourceComponentID string                               `json:"source_component_id" binding:"required"`
	Assignments       []DataApplicationSelectionAssignment `json:"assignments" binding:"required"`
}

type DataApplicationSelectionAssignment struct {
	SourceField             string `json:"source_field" binding:"required"`
	ApplicationParameterKey string `json:"application_parameter_key" binding:"required"`
}

type DataApplicationCreateRequest struct {
	Name          string   `json:"name" binding:"required"`
	Description   string   `json:"description"`
	SourceViewIDs []string `json:"source_view_ids" binding:"required"`
}

type DataApplicationUpdateRequest struct {
	Name        string                  `json:"name" binding:"required"`
	Description string                  `json:"description"`
	Snapshot    DataApplicationSnapshot `json:"snapshot" binding:"required"`
	Version     int64                   `json:"version" binding:"required"`
}

type DataApplicationVersionRequest struct {
	Version int64 `json:"version" binding:"required"`
}

type DataApplicationSummaryResponse struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	PublicationStatus     string    `json:"publication_status" enums:"unpublished,published,offline"`
	CurrentRevisionNumber *int64    `json:"current_revision_number"`
	HasUnpublishedChanges bool      `json:"has_unpublished_changes"`
	Version               int64     `json:"version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type DataApplicationResponse struct {
	DataApplicationSummaryResponse
	TenantID    int64                   `json:"tenant_id"`
	OwnerUserID int64                   `json:"owner_user_id"`
	Snapshot    DataApplicationSnapshot `json:"snapshot"`
}

type DataApplicationRuntimeResponse struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	RevisionNumber int64                   `json:"revision_number"`
	Snapshot       DataApplicationSnapshot `json:"snapshot"`
	PublishedAt    time.Time               `json:"published_at"`
}
