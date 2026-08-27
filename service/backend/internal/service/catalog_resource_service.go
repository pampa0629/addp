package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/service/internal/models"
)

const (
	defaultCatalogChangeLimit = 200
	maxCatalogChangeLimit     = 500
	maxCatalogReferences      = 200
)

var ErrInvalidCatalogResourceRequest = errors.New("invalid catalog resource request")

type CatalogResourceRepository interface {
	ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error)
	ListQueryServices(context.Context, int64, []int64) ([]models.QueryService, error)
	LatestChangeVersions(context.Context, int64, []int64) (map[int64]int64, error)
}

type CatalogResourceService struct{ repository CatalogResourceRepository }

func NewCatalogResourceService(repository CatalogResourceRepository) *CatalogResourceService {
	return &CatalogResourceService{repository: repository}
}

func (s *CatalogResourceService) ListChanges(ctx context.Context, tenantID int64, afterCursor string, limit int) (*models.CatalogResourceChangesResponse, error) {
	if tenantID <= 0 || s == nil || s.repository == nil {
		return nil, ErrInvalidCatalogResourceRequest
	}
	afterID, err := decodeCatalogChangeCursor(afterCursor)
	if err != nil {
		return nil, ErrInvalidCatalogResourceRequest
	}
	if limit == 0 {
		limit = defaultCatalogChangeLimit
	}
	if limit < 1 || limit > maxCatalogChangeLimit {
		return nil, ErrInvalidCatalogResourceRequest
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
		return nil, ErrInvalidCatalogResourceRequest
	}
	ids := make([]int64, len(references))
	for index, reference := range references {
		if reference.SourceType != models.CatalogSourceTypeQueryService {
			return nil, ErrInvalidCatalogResourceRequest
		}
		id, err := parseCanonicalPositiveID(reference.SourceIdentity)
		if err != nil {
			return nil, ErrInvalidCatalogResourceRequest
		}
		ids[index] = id
	}
	queryServices, err := s.repository.ListQueryServices(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	versions, err := s.repository.LatestChangeVersions(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]models.QueryService, len(queryServices))
	for _, queryService := range queryServices {
		byID[int64(queryService.ID)] = queryService
	}
	results := make([]models.CatalogReferenceResolution, 0, len(references))
	for index, reference := range references {
		result := models.CatalogReferenceResolution{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity}
		if queryService, exists := byID[ids[index]]; exists {
			version := versions[ids[index]]
			if version <= 0 {
				return nil, fmt.Errorf("Service catalog Query Service %d has no change version", ids[index])
			}
			result.Found, result.Status, result.Version = true, queryService.Status, version
			result.Summary = queryServiceCatalogSummary(queryService)
			result.DetailPath = "/service/published-services/" + reference.SourceIdentity
		}
		results = append(results, result)
	}
	return &models.ResolveCatalogReferencesResponse{Results: results}, nil
}

func queryServiceCatalogSummary(queryService models.QueryService) map[string]any {
	result := map[string]any{
		"name": queryService.Title, "code": queryService.ServiceName, "object_kind": "query_service",
		"service_status": queryService.Status, "config_type": queryService.ConfigType,
		"access_mode": map[bool]string{true: "public", false: "private"}[queryService.PublicAccess],
	}
	if queryService.EngineID != nil {
		result["engine_id"] = strconv.FormatUint(uint64(*queryService.EngineID), 10)
	}
	if queryService.RuntimeEngineID != nil {
		result["runtime_engine_id"] = strconv.FormatUint(uint64(*queryService.RuntimeEngineID), 10)
	}
	return result
}

func parseCanonicalPositiveID(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 || value != trimmed || strconv.FormatInt(id, 10) != trimmed {
		return 0, ErrInvalidCatalogResourceRequest
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
		return 0, ErrInvalidCatalogResourceRequest
	}
	return id, nil
}
