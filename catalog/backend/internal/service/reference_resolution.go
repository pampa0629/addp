package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

// CatalogReferenceResolution is the exact, tenant-scoped view exposed to the
// Asset runtime. It is a validation result, not a replicated Asset fact.
type CatalogReferenceResolution struct {
	ID               uuid.UUID `json:"id"`
	Found            bool      `json:"found"`
	Selectable       bool      `json:"selectable"`
	Publishable      bool      `json:"publishable"`
	EntryType        string    `json:"entry_type,omitempty"`
	SourceModule     string    `json:"source_module,omitempty"`
	SourceType       string    `json:"source_type,omitempty"`
	SourceIdentity   string    `json:"source_identity,omitempty"`
	DisplayName      string    `json:"display_name,omitempty"`
	EntryStatus      string    `json:"entry_status,omitempty"`
	GovernanceStatus string    `json:"governance_status,omitempty"`
	SourceStatus     string    `json:"source_status,omitempty"`
	Version          int64     `json:"version,string" swaggertype:"string"`
}

func (s *EntryService) ResolveReferences(ctx context.Context, tenantID int64, ids []uuid.UUID) ([]CatalogReferenceResolution, error) {
	if tenantID <= 0 || len(ids) == 0 || len(ids) > 200 {
		return nil, ErrInvalidEntryUpdate
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, ErrInvalidEntryUpdate
		}
		if _, exists := seen[id]; exists {
			return nil, ErrInvalidEntryUpdate
		}
		seen[id] = struct{}{}
	}

	type referenceRow struct {
		models.Entry
		SourceModule     string
		SourceType       string
		SourceIdentity   string
		SourceStatus     string
		ObservedSnapshot commonModels.JSONMap
	}
	var rows []referenceRow
	if err := s.db.WithContext(ctx).Table("catalog.entries AS entries").
		Select("entries.*, source.source_module, source.source_type, source.source_identity, source.source_status, source.observed_snapshot").
		Joins("LEFT JOIN catalog.source_bindings AS source ON source.tenant_id = entries.tenant_id AND source.catalog_entry_id = entries.id AND source.is_current = ?", true).
		Where("entries.tenant_id = ? AND entries.id IN ?", tenantID, ids).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Catalog references: %w", err)
	}
	byID := make(map[uuid.UUID]referenceRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	results := make([]CatalogReferenceResolution, 0, len(ids))
	for _, id := range ids {
		result := CatalogReferenceResolution{ID: id}
		row, exists := byID[id]
		if !exists {
			results = append(results, result)
			continue
		}
		result.Found = true
		result.EntryType = row.EntryType
		result.SourceModule = row.SourceModule
		result.SourceType = row.SourceType
		result.SourceIdentity = row.SourceIdentity
		result.EntryStatus = row.EntryStatus
		result.GovernanceStatus = row.GovernanceStatus
		result.SourceStatus = row.SourceStatus
		result.Version = row.Version
		if row.BusinessName != nil {
			result.DisplayName = strings.TrimSpace(*row.BusinessName)
		}
		if result.DisplayName == "" {
			result.DisplayName, _ = row.ObservedSnapshot["name"].(string)
		}
		result.Selectable = row.EntryStatus == models.EntryStatusActive &&
			row.GovernanceStatus != models.GovernanceStatusDeprecated &&
			row.SourceStatus == models.SourceStatusActive
		result.Publishable = result.Selectable &&
			(row.GovernanceStatus == models.GovernanceStatusCurated || row.GovernanceStatus == models.GovernanceStatusCertified)
		results = append(results, result)
	}
	return results, nil
}
