package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
)

const maxSourceEntryReferences = 200

type CatalogSourceReference struct {
	SourceModule   string `json:"source_module" enums:"meta,model,standard,service,develop"`
	SourceType     string `json:"source_type" enums:"data_item,entity,logical_table,metric,query_service,dev_task"`
	SourceIdentity string `json:"source_identity"`
}

type SourceEntryResolution struct {
	CatalogSourceReference
	Found bool          `json:"found"`
	Entry *EntrySummary `json:"entry,omitempty"`
}

type SourceEntryResolutionResult struct {
	Results []SourceEntryResolution `json:"results"`
}

func (s *EntryService) ResolveSourceEntries(ctx context.Context, tenantID int64, access EntryAccess, references []CatalogSourceReference) (*SourceEntryResolutionResult, error) {
	if tenantID <= 0 || len(references) == 0 || len(references) > maxSourceEntryReferences {
		return nil, ErrInvalidSourceReference
	}
	normalized := make([]CatalogSourceReference, 0, len(references))
	for _, reference := range references {
		reference.SourceModule = strings.TrimSpace(reference.SourceModule)
		reference.SourceType = strings.TrimSpace(reference.SourceType)
		reference.SourceIdentity = strings.TrimSpace(reference.SourceIdentity)
		if !validCatalogSourceReference(reference) {
			return nil, ErrInvalidSourceReference
		}
		normalized = append(normalized, reference)
	}

	type sourceEntryRow struct {
		models.Entry
		SourceModule     string
		SourceType       string
		SourceIdentity   string
		SourceStatus     string
		ObservedSnapshot commonModels.JSONMap
	}
	query := s.visibleEntriesQuery(ctx, tenantID, access).
		Select("entries.*, source.source_module, source.source_type, source.source_identity, source.source_status, source.observed_snapshot").
		Joins("JOIN catalog.source_bindings AS source ON source.catalog_entry_id = entries.id AND source.tenant_id = entries.tenant_id AND source.is_current = ?", true).
		Where("entries.entry_status = ?", models.EntryStatusActive)
	conditions := make([]string, 0, len(normalized))
	conditionArgs := make([]any, 0, len(normalized)*3)
	for _, reference := range normalized {
		conditions = append(conditions, "(source.source_module = ? AND source.source_type = ? AND source.source_identity = ?)")
		conditionArgs = append(conditionArgs, reference.SourceModule, reference.SourceType, reference.SourceIdentity)
	}
	query = query.Where("("+strings.Join(conditions, " OR ")+")", conditionArgs...)
	var rows []sourceEntryRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve Catalog source entries: %w", err)
	}
	byReference := make(map[string]EntrySummary, len(rows))
	for _, row := range rows {
		displayName, _ := row.ObservedSnapshot["name"].(string)
		if row.BusinessName != nil && strings.TrimSpace(*row.BusinessName) != "" {
			displayName = *row.BusinessName
		}
		engineID, _ := numericInt64(row.ObservedSnapshot["engine_id"])
		byReference[sourceReferenceKey(row.SourceModule, row.SourceType, row.SourceIdentity)] = EntrySummary{
			Entry: row.Entry, DisplayName: displayName, SourceStatus: row.SourceStatus,
			SourceIdentity: row.SourceIdentity, SourceEngineID: engineID,
		}
	}
	results := make([]SourceEntryResolution, 0, len(normalized))
	for _, reference := range normalized {
		resolution := SourceEntryResolution{CatalogSourceReference: reference}
		if entry, ok := byReference[sourceReferenceKey(reference.SourceModule, reference.SourceType, reference.SourceIdentity)]; ok {
			entryCopy := entry
			resolution.Found = true
			resolution.Entry = &entryCopy
		}
		results = append(results, resolution)
	}
	return &SourceEntryResolutionResult{Results: results}, nil
}

func validCatalogSourceReference(reference CatalogSourceReference) bool {
	if reference.SourceIdentity == "" || len(reference.SourceIdentity) > 255 {
		return false
	}
	validPair := (reference.SourceModule == models.SourceModuleMeta && reference.SourceType == models.SourceTypeDataItem) ||
		(reference.SourceModule == models.SourceModuleModel && (reference.SourceType == models.SourceTypeEntity || reference.SourceType == models.SourceTypeLogicalTable)) ||
		(reference.SourceModule == models.SourceModuleStandard && reference.SourceType == models.SourceTypeMetric) ||
		(reference.SourceModule == models.SourceModuleService && reference.SourceType == models.SourceTypeQueryService) ||
		(reference.SourceModule == models.SourceModuleDevelop && reference.SourceType == models.SourceTypeDevTask)
	if !validPair {
		return false
	}
	if reference.SourceModule == models.SourceModuleMeta {
		return true
	}
	id, err := strconv.ParseInt(reference.SourceIdentity, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == reference.SourceIdentity
}

func sourceReferenceKey(module, resourceType, identity string) string {
	return module + "\x00" + resourceType + "\x00" + identity
}
