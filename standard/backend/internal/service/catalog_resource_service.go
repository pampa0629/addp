package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/common/domainreference"
	"github.com/addp/standard/internal/models"
)

const (
	defaultCatalogChangeLimit = 200
	maxCatalogChangeLimit     = 500
	maxCatalogReferences      = 200
)

var ErrInvalidCatalogResourceRequest = errors.New("invalid catalog resource request")

type CatalogResourceRepository interface {
	ListDomainStandardReferences(context.Context, int64, int64, string, int, int) (*domainreference.Page, error)
	ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error)
	ListResources(context.Context, int64, []models.CatalogReference) ([]models.CatalogReferenceResolution, error)
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
	for _, reference := range references {
		if !models.ValidCatalogSourceType(reference.SourceType) {
			return nil, ErrInvalidCatalogResourceRequest
		}
		if _, err := parseCanonicalPositiveID(reference.SourceIdentity); err != nil {
			return nil, ErrInvalidCatalogResourceRequest
		}
	}
	resources, err := s.repository.ListResources(ctx, tenantID, references)
	if err != nil {
		return nil, err
	}
	byKey := map[string]models.CatalogReferenceResolution{}
	for _, resource := range resources {
		byKey[resource.SourceType+":"+resource.SourceIdentity] = resource
	}
	results := make([]models.CatalogReferenceResolution, 0, len(references))
	for _, reference := range references {
		result, ok := byKey[reference.SourceType+":"+reference.SourceIdentity]
		if !ok {
			result = models.CatalogReferenceResolution{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity}
		}
		results = append(results, result)
	}
	return &models.ResolveCatalogReferencesResponse{Results: results}, nil
}

func displayMetricRevision(metric models.MetricDefinitionAggregate) *models.MetricDefinitionRevision {
	if metric.CurrentRevision != nil {
		return metric.CurrentRevision
	}
	return metric.LatestRevision
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
