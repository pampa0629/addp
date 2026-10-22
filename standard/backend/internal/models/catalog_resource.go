package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

// ValidCatalogSourceType is the closed set of independently cataloged standards.
func ValidCatalogSourceType(kind string) bool {
	switch kind {
	case "metric", "glossary", "element", "code_set", "document":
		return true
	}
	return false
}

// ProjectCatalogResource deliberately discards the professional definition body.
// Typed decoding preserves int64 identity precision even when projecting aggregates.
func ProjectCatalogResource(kind string, aggregate any) (CatalogReferenceResolution, error) {
	var value struct {
		ID              int64                   `json:"id"`
		Version         int64                   `json:"version"`
		Code            string                  `json:"code"`
		ScopeType       string                  `json:"scope_type"`
		OwnerDomainID   *int64                  `json:"owner_domain_id"`
		CategoryID      *int64                  `json:"category_id"`
		LifecycleState  string                  `json:"lifecycle_state"`
		CurrentRevision *catalogDisplayRevision `json:"current_revision"`
		LatestRevision  *catalogDisplayRevision `json:"latest_revision"`
	}
	data, err := json.Marshal(aggregate)
	if err != nil {
		return CatalogReferenceResolution{}, err
	}
	if err = json.Unmarshal(data, &value); err != nil {
		return CatalogReferenceResolution{}, err
	}
	revision := value.CurrentRevision
	if revision == nil {
		revision = value.LatestRevision
	}
	if !ValidCatalogSourceType(kind) || value.ID <= 0 {
		return CatalogReferenceResolution{}, fmt.Errorf("invalid catalog standard")
	}
	summary := map[string]any{"name": value.Code, "code": value.Code, "object_kind": kind,
		"scope_type": value.ScopeType, "lifecycle_state": value.LifecycleState, "definition_complete": false, "is_current": value.CurrentRevision != nil}
	result := CatalogReferenceResolution{SourceType: kind, SourceIdentity: strconv.FormatInt(value.ID, 10), Found: true, Version: value.Version, Summary: summary}
	paths := map[string]string{"metric": "metrics", "element": "elements", "glossary": "glossaries", "code_set": "code-sets", "document": "documents"}
	result.DetailPath = "/standard/" + paths[kind] + "/" + result.SourceIdentity
	if value.OwnerDomainID != nil {
		summary["domain_id"] = strconv.FormatInt(*value.OwnerDomainID, 10)
	}
	if value.CategoryID != nil {
		summary["category_id"] = strconv.FormatInt(*value.CategoryID, 10)
	}
	if revision != nil {
		summary["name"], summary["revision_status"], result.Status = revision.Name, revision.Status, revision.Status
		summary["revision_id"], summary["revision_no"] = strconv.FormatInt(revision.ID, 10), revision.RevisionNo
		definition := revision.Definition
		if kind == "code_set" {
			definition = revision.Description
		}
		summary["definition_complete"] = strings.TrimSpace(revision.Name) != "" && (kind == "document" || strings.TrimSpace(definition) != "")
		if kind == "metric" {
			summary["metric_type"] = revision.MetricType
		}
		if revision.UnitID != nil {
			summary["unit_id"] = strconv.FormatInt(*revision.UnitID, 10)
		}
	}
	return result, nil
}

type catalogDisplayRevision struct {
	ID          int64  `json:"id"`
	RevisionNo  int64  `json:"revision_no"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Definition  string `json:"definition"`
	Description string `json:"description"`
	MetricType  string `json:"metric_type"`
	UnitID      *int64 `json:"unit_id"`
}
