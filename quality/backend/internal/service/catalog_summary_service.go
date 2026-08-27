package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

const maxCatalogSummaryReferences = 200

var ErrInvalidCatalogSummaryRequest = errors.New("invalid quality catalog summary request")

type catalogSummaryRepository interface {
	Resolve(context.Context, int64, []models.CatalogSummaryReference) (map[string]repository.CatalogSummaryFact, error)
}

type CatalogSummaryService struct{ repository catalogSummaryRepository }

func NewCatalogSummaryService(repository catalogSummaryRepository) *CatalogSummaryService {
	return &CatalogSummaryService{repository: repository}
}

func (s *CatalogSummaryService) Resolve(ctx context.Context, tenantID int64, references []models.CatalogSummaryReference) (*models.ResolveCatalogSummariesResponse, error) {
	if tenantID <= 0 || s == nil || s.repository == nil || len(references) == 0 || len(references) > maxCatalogSummaryReferences {
		return nil, ErrInvalidCatalogSummaryRequest
	}
	for _, reference := range references {
		if !validCatalogSummaryReference(reference) {
			return nil, ErrInvalidCatalogSummaryRequest
		}
	}
	facts, err := s.repository.Resolve(ctx, tenantID, references)
	if err != nil {
		return nil, err
	}
	results := make([]models.CatalogSummaryResolution, 0, len(references))
	for _, reference := range references {
		result := models.CatalogSummaryResolution{Reference: reference}
		fact, exists := facts[qualityCatalogSummaryKey(reference)]
		if exists {
			result.Configured = true
			result.CheckTaskID = fact.Task.ID
			result.LastExecutionID = fact.Task.LastExecutionID
			result.LastExecutionStatus = fact.Task.LastExecutionStatus
			result.OpenIssueCount = fact.OpenIssues
			result.LastObservedAt = fact.Task.LastRunAt
			result.QualityScore = repository.QualityScoreFromExecution(fact.Execution)
			if result.LastExecutionID != "" {
				result.DetailPath = "/quality/executions/" + result.LastExecutionID
			} else {
				result.DetailPath = "/quality/check-tasks"
			}
		}
		results = append(results, result)
	}
	return &models.ResolveCatalogSummariesResponse{Results: results}, nil
}

func validCatalogSummaryReference(reference models.CatalogSummaryReference) bool {
	return reference.EngineID > 0 && validCatalogIdentifier(reference.SchemaName) && validCatalogIdentifier(reference.TableName)
}

func validCatalogIdentifier(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed == value && len(value) <= 200 && !strings.ContainsRune(value, '\x00')
}

func qualityCatalogSummaryKey(reference models.CatalogSummaryReference) string {
	return fmt.Sprintf("%d\x00%s\x00%s", reference.EngineID, reference.SchemaName, reference.TableName)
}
