package models

import "time"

const (
	MetricImplementationDraft     = "draft"
	MetricImplementationPublished = "published"
	MetricImplementationWithdrawn = "withdrawn"
)

// MetricImplementation owns its concurrency version independently of its source table.
type MetricImplementation struct {
	ID                 int64                          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID           int64                          `gorm:"not null;index" json:"tenant_id"`
	FactTableID        int64                          `gorm:"not null;index" json:"fact_table_id"`
	MetricDefinitionID int64                          `gorm:"not null;index" json:"metric_definition_id"`
	Name               string                         `gorm:"size:200;not null" json:"name"`
	Note               string                         `gorm:"type:text" json:"note"`
	Version            int64                          `gorm:"not null;default:1" json:"version"`
	CreatedBy          int64                          `gorm:"not null" json:"created_by"`
	UpdatedBy          *int64                         `json:"updated_by,omitempty"`
	CreatedAt          time.Time                      `json:"created_at"`
	UpdatedAt          time.Time                      `json:"updated_at"`
	Revisions          []MetricImplementationRevision `gorm:"-" json:"revisions"`
}

func (MetricImplementation) TableName() string { return "model.metric_implementations" }

// Published content is immutable; withdrawal only changes availability.
type MetricImplementationRevision struct {
	ID                         int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID                   int64          `gorm:"not null;index" json:"tenant_id"`
	ImplementationID           int64          `gorm:"not null;index" json:"implementation_id"`
	RevisionNo                 int64          `gorm:"not null" json:"revision_no"`
	MetricDefinitionRevisionID int64          `gorm:"not null" json:"metric_definition_revision_id"`
	Contract                   MetricContract `gorm:"type:jsonb;serializer:json;not null" json:"contract"`
	DependencySnapshot         JSONB          `gorm:"type:jsonb;serializer:json;not null" json:"dependency_snapshot"`
	DependencyHash             string         `gorm:"not null" json:"dependency_hash"`
	Status                     string         `gorm:"not null" json:"status" enums:"draft,published,withdrawn"`
	PublishedAt                *time.Time     `json:"published_at,omitempty"`
	CreatedAt                  time.Time      `json:"created_at"`
	UpdatedAt                  time.Time      `json:"updated_at"`
}

func (MetricImplementationRevision) TableName() string {
	return "model.metric_implementation_revisions"
}

type CreateMetricImplementationRequest struct {
	FactTableID        int64  `json:"fact_table_id" binding:"required,gt=0"`
	MetricDefinitionID int64  `json:"metric_definition_id" binding:"required,gt=0"`
	Name               string `json:"name" binding:"required,max=200"`
	Note               string `json:"note"`
}

type SaveMetricImplementationRevisionRequest struct {
	Version                    int64          `json:"version" binding:"required,gt=0"`
	MetricDefinitionRevisionID int64          `json:"metric_definition_revision_id" binding:"required,gt=0"`
	Contract                   MetricContract `json:"contract" binding:"required"`
}

type MetricImplementationVersionRequest struct {
	Version int64 `json:"version" binding:"required,gt=0"`
}
