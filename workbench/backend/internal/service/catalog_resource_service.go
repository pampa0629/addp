package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/addp/workbench/internal/models"
	"github.com/google/uuid"
)

const (
	defaultCatalogChangeLimit = 200
	maxCatalogChangeLimit     = 500
	maxCatalogReferences      = 200
)

var ErrInvalidCatalogResourceRequest = errors.New("invalid Workbench catalog resource request")

type CatalogResourceRepository interface {
	ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error)
	ListDataApplications(context.Context, int64, []string) ([]models.CatalogDataApplicationRecord, error)
	LatestChangeVersions(context.Context, int64, []string) (map[string]int64, error)
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
			SourceIdentity: row.SourceIdentity, Operation: row.Operation,
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
	ids := make([]string, len(references))
	for index, reference := range references {
		if reference.SourceType != models.CatalogSourceTypeDataApplication {
			return nil, ErrInvalidCatalogResourceRequest
		}
		id, err := parseCanonicalUUID(reference.SourceIdentity)
		if err != nil {
			return nil, ErrInvalidCatalogResourceRequest
		}
		ids[index] = id
	}
	applications, err := s.repository.ListDataApplications(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	versions, err := s.repository.LatestChangeVersions(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]models.CatalogDataApplicationRecord, len(applications))
	for _, application := range applications {
		byID[application.ID] = application
	}
	results := make([]models.CatalogReferenceResolution, 0, len(references))
	for index, reference := range references {
		result := models.CatalogReferenceResolution{SourceType: reference.SourceType, SourceIdentity: reference.SourceIdentity}
		if application, exists := byID[ids[index]]; exists {
			version := versions[ids[index]]
			if version <= 0 {
				return nil, fmt.Errorf("Workbench catalog Data Application %s has no change version", ids[index])
			}
			result.Found, result.Status, result.Version = true, application.PublicationStatus, version
			result.Summary = dataApplicationCatalogSummary(application)
			result.DetailPath = "/data-apps/" + ids[index]
		}
		results = append(results, result)
	}
	return &models.ResolveCatalogReferencesResponse{Results: results}, nil
}

func dataApplicationCatalogSummary(application models.CatalogDataApplicationRecord) map[string]any {
	return map[string]any{
		"name":               application.RevisionName,
		"description":        application.RevisionDescription,
		"object_kind":        models.CatalogSourceTypeDataApplication,
		"publication_status": application.PublicationStatus,
		"revision_number":    application.CurrentRevisionNumber,
		"runtime_path":       "/data-apps/" + application.ID,
	}
}

func parseCanonicalUUID(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	id, err := uuid.Parse(trimmed)
	if err != nil || id == uuid.Nil || value != trimmed || id.String() != trimmed {
		return "", ErrInvalidCatalogResourceRequest
	}
	return id.String(), nil
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
