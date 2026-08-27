package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

const (
	defaultCatalogChangeLimit = 200
	maxCatalogChangeLimit     = 500
	maxCatalogReferences      = 200
)

type CatalogResourceService struct {
	repository CatalogResourceRepository
}

type CatalogResourceRepository interface {
	ListChanges(context.Context, int64, int64, int) ([]repository.CatalogResourceChangeRow, error)
	ListEntities(context.Context, int64, []int64) ([]models.Entity, error)
	ListLogicalTables(context.Context, int64, []int64) ([]models.LogicalTable, error)
}

func NewCatalogResourceService(repository CatalogResourceRepository) *CatalogResourceService {
	return &CatalogResourceService{repository: repository}
}

func (s *CatalogResourceService) ListChanges(ctx context.Context, tenantID int64, afterCursor string, limit int) (*models.CatalogResourceChangesResponse, error) {
	if tenantID <= 0 || s == nil || s.repository == nil {
		return nil, invalidRequest()
	}
	afterID, err := decodeCatalogChangeCursor(afterCursor)
	if err != nil {
		return nil, invalidRequest()
	}
	if limit == 0 {
		limit = defaultCatalogChangeLimit
	}
	if limit < 1 || limit > maxCatalogChangeLimit {
		return nil, invalidRequest()
	}
	rows, err := s.repository.ListChanges(ctx, tenantID, afterID, limit+1)
	if err != nil {
		return nil, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	changes := make([]models.CatalogResourceChange, 0, len(rows))
	nextID := afterID
	for _, row := range rows {
		changes = append(changes, models.CatalogResourceChange{
			ChangeID: encodeCatalogChangeCursor(row.ID), SourceType: row.SourceType,
			SourceIdentity: strconv.FormatInt(row.SourceIdentity, 10), Operation: row.Operation,
			SourceVersion: fmt.Sprintf("%020d", row.ID), ObservedAt: row.ObservedAt,
			Snapshot: map[string]any(row.Snapshot),
		})
		nextID = row.ID
	}
	return &models.CatalogResourceChangesResponse{
		SchemaVersion: models.CatalogResourceChangesSchemaVersion,
		Changes:       changes, NextCursor: encodeCatalogChangeCursor(nextID), HasMore: hasMore,
	}, nil
}

func (s *CatalogResourceService) Resolve(ctx context.Context, tenantID int64, references []models.CatalogReference) (*models.ResolveCatalogReferencesResponse, error) {
	if tenantID <= 0 || s == nil || s.repository == nil || len(references) == 0 || len(references) > maxCatalogReferences {
		return nil, invalidRequest()
	}
	entityIDs := make([]int64, 0, len(references))
	logicalTableIDs := make([]int64, 0, len(references))
	parsedIDs := make([]int64, len(references))
	for index, reference := range references {
		id, err := parseCanonicalPositiveID(reference.SourceIdentity)
		if err != nil {
			return nil, invalidRequest()
		}
		switch reference.SourceType {
		case models.CatalogSourceTypeEntity:
			entityIDs = append(entityIDs, id)
		case models.CatalogSourceTypeLogicalTable:
			logicalTableIDs = append(logicalTableIDs, id)
		default:
			return nil, invalidRequest()
		}
		parsedIDs[index] = id
	}
	entities, err := s.repository.ListEntities(ctx, tenantID, entityIDs)
	if err != nil {
		return nil, err
	}
	logicalTables, err := s.repository.ListLogicalTables(ctx, tenantID, logicalTableIDs)
	if err != nil {
		return nil, err
	}
	entityByID := make(map[int64]models.Entity, len(entities))
	for _, entity := range entities {
		entityByID[entity.ID] = entity
	}
	logicalTableByID := make(map[int64]models.LogicalTable, len(logicalTables))
	for _, logicalTable := range logicalTables {
		logicalTableByID[logicalTable.ID] = logicalTable
	}
	results := make([]models.CatalogReferenceResolution, 0, len(references))
	for index, reference := range references {
		id := parsedIDs[index]
		result := models.CatalogReferenceResolution{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity}
		if reference.SourceType == models.CatalogSourceTypeEntity {
			if entity, exists := entityByID[id]; exists {
				result.Found, result.Status, result.Version = true, entity.Status, entity.Version
				result.Summary = entityCatalogSummary(entity)
				result.DetailPath = "/modeling/entities/" + reference.SourceIdentity
			}
		} else if logicalTable, exists := logicalTableByID[id]; exists {
			result.Found, result.Status, result.Version = true, logicalTable.Status, logicalTable.Version
			result.Summary = logicalTableCatalogSummary(logicalTable)
			result.DetailPath = "/modeling/logical-tables/" + reference.SourceIdentity
		}
		results = append(results, result)
	}
	return &models.ResolveCatalogReferencesResponse{Results: results}, nil
}

func entityCatalogSummary(entity models.Entity) map[string]any {
	result := map[string]any{"name": entity.Name, "code": entity.Code, "object_kind": "entity", "model_status": entity.Status}
	if entity.DomainID != nil {
		result["domain_id"] = strconv.FormatInt(*entity.DomainID, 10)
	}
	return result
}

func logicalTableCatalogSummary(logicalTable models.LogicalTable) map[string]any {
	result := map[string]any{
		"name": logicalTable.Name, "code": logicalTable.Code, "object_kind": "logical_table",
		"model_status": logicalTable.Status, "table_type": logicalTable.TableType, "layer": logicalTable.Layer,
	}
	if logicalTable.DomainID != nil {
		result["domain_id"] = strconv.FormatInt(*logicalTable.DomainID, 10)
	}
	if logicalTable.EntityID != nil {
		result["entity_id"] = strconv.FormatInt(*logicalTable.EntityID, 10)
	}
	return result
}

func parseCanonicalPositiveID(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 || value != trimmed || strconv.FormatInt(id, 10) != trimmed {
		return 0, fmt.Errorf("invalid canonical positive ID")
	}
	return id, nil
}

func encodeCatalogChangeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeCatalogChangeCursor(cursor string) (int64, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id < 0 || strconv.FormatInt(id, 10) != string(raw) {
		return 0, fmt.Errorf("invalid cursor")
	}
	return id, nil
}
