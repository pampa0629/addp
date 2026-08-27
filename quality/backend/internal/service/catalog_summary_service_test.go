package service

import (
	"context"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"github.com/addp/quality/internal/repository"
)

type catalogSummaryRepositoryFake struct {
	facts map[string]repository.CatalogSummaryFact
}

func (f catalogSummaryRepositoryFake) Resolve(_ context.Context, _ int64, _ []models.CatalogSummaryReference) (map[string]repository.CatalogSummaryFact, error) {
	return f.facts, nil
}

func TestCatalogSummaryResolvePreservesOrderAndOnlyUsesCurrentSuccessfulResult(t *testing.T) {
	observed := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	reference := models.CatalogSummaryReference{EngineID: 7, SchemaName: "public", TableName: "orders"}
	service := NewCatalogSummaryService(catalogSummaryRepositoryFake{facts: map[string]repository.CatalogSummaryFact{
		qualityCatalogSummaryKey(reference): {
			Task:       models.CheckTask{ID: 31, EngineID: 7, SchemaName: "public", Table: "orders", LastExecutionID: "execution-1", LastExecutionStatus: commonExecution.ExecutionStatusSuccess, LastRunAt: &observed},
			Execution:  &commonExecution.TaskExecution{Status: commonExecution.ExecutionStatusSuccess, Metadata: commonModels.JSONMap{"schema_version": "addp.quality.execution-result/v1", "quality_score": 97.5}},
			OpenIssues: 2,
		},
	}})
	result, err := service.Resolve(context.Background(), 9, []models.CatalogSummaryReference{reference, {EngineID: 7, SchemaName: "public", TableName: "missing"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || !result.Results[0].Configured || result.Results[0].QualityScore == nil || *result.Results[0].QualityScore != 97.5 || result.Results[0].OpenIssueCount != 2 || result.Results[1].Configured {
		t.Fatalf("unexpected summaries: %#v", result.Results)
	}
}

func TestCatalogSummaryResolveRejectsUnstructuredReferences(t *testing.T) {
	service := NewCatalogSummaryService(catalogSummaryRepositoryFake{})
	for _, reference := range []models.CatalogSummaryReference{
		{EngineID: 0, SchemaName: "public", TableName: "orders"},
		{EngineID: 7, SchemaName: "", TableName: "orders"},
		{EngineID: 7, SchemaName: " public", TableName: "orders"},
	} {
		if _, err := service.Resolve(context.Background(), 9, []models.CatalogSummaryReference{reference}); err != ErrInvalidCatalogSummaryRequest {
			t.Fatalf("reference %#v error = %v", reference, err)
		}
	}
}
