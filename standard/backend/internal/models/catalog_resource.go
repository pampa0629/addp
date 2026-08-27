package models

import "time"

const (
	CatalogResourceChangesSchemaVersion = "standard.catalog_resource_changes/v1"
	CatalogSourceTypeMetric             = "metric"
)

type CatalogResourceChangeRow struct {
	ID              int64     `gorm:"primaryKey;autoIncrement;index:idx_standard_catalog_changes_tenant_id,priority:2"`
	TenantID        int64     `gorm:"not null;index:idx_standard_catalog_changes_tenant_id,priority:1;index:idx_standard_catalog_changes_source,priority:1"`
	SourceType      string    `gorm:"size:32;not null;index:idx_standard_catalog_changes_source,priority:2"`
	SourceIdentity  int64     `gorm:"not null;index:idx_standard_catalog_changes_source,priority:3"`
	Operation       string    `gorm:"size:16;not null"`
	ResourceVersion int64     `gorm:"not null"`
	Snapshot        JSONB     `gorm:"type:jsonb;serializer:json;not null"`
	ObservedAt      time.Time `gorm:"not null"`
}

func (CatalogResourceChangeRow) TableName() string { return "standard.catalog_resource_changes" }

type CatalogResourceChange struct {
	ChangeID       string         `json:"change_id"`
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Operation      string         `json:"operation"`
	SourceVersion  string         `json:"source_version"`
	ObservedAt     time.Time      `json:"observed_at"`
	Snapshot       map[string]any `json:"snapshot"`
}

type CatalogResourceChangesResponse struct {
	SchemaVersion string                  `json:"schema_version"`
	Changes       []CatalogResourceChange `json:"changes"`
	NextCursor    string                  `json:"next_cursor"`
	HasMore       bool                    `json:"has_more"`
}

type CatalogReference struct {
	SourceType     string `json:"source_type" binding:"required"`
	SourceIdentity string `json:"source_identity" binding:"required"`
}

type ResolveCatalogReferencesRequest struct {
	References []CatalogReference `json:"references" binding:"required,min=1,max=200,dive"`
}

type CatalogReferenceResolution struct {
	SourceType     string         `json:"source_type"`
	SourceIdentity string         `json:"source_identity"`
	Found          bool           `json:"found"`
	Status         string         `json:"status,omitempty"`
	Version        int64          `json:"version,omitempty"`
	Summary        map[string]any `json:"summary,omitempty"`
	DetailPath     string         `json:"detail_path,omitempty"`
}

type ResolveCatalogReferencesResponse struct {
	Results []CatalogReferenceResolution `json:"results"`
}
