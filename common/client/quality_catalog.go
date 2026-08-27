package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type QualityClient struct{ tenantHTTPClient }

func NewQualityClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *QualityClient {
	return &QualityClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *QualityClient) WithTenantID(tenantID uint) *QualityClient {
	if c == nil {
		return nil
	}
	return &QualityClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

type QualityCatalogSummaryReference struct {
	EngineID   int64  `json:"engine_id"`
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
}

type QualityCatalogSummaryResolution struct {
	Reference           QualityCatalogSummaryReference `json:"reference"`
	Configured          bool                           `json:"configured"`
	CheckTaskID         int64                          `json:"check_task_id,omitempty"`
	LastExecutionID     string                         `json:"last_execution_id,omitempty"`
	LastExecutionStatus string                         `json:"last_execution_status,omitempty"`
	QualityScore        *float64                       `json:"quality_score,omitempty"`
	OpenIssueCount      int64                          `json:"open_issue_count"`
	LastObservedAt      *time.Time                     `json:"last_observed_at,omitempty"`
	DetailPath          string                         `json:"detail_path,omitempty"`
}

type ResolveQualityCatalogSummariesResponse struct {
	Results []QualityCatalogSummaryResolution `json:"results"`
}

func (c *QualityClient) ResolveCatalogSummaries(ctx context.Context, references []QualityCatalogSummaryReference) (*ResolveQualityCatalogSummariesResponse, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || len(references) == 0 || len(references) > 200 {
		return nil, errors.New("Quality catalog summary resolution requires tenant context and 1 to 200 references")
	}
	for _, reference := range references {
		if reference.EngineID <= 0 || strings.TrimSpace(reference.SchemaName) == "" || strings.TrimSpace(reference.SchemaName) != reference.SchemaName || strings.TrimSpace(reference.TableName) == "" || strings.TrimSpace(reference.TableName) != reference.TableName {
			return nil, errors.New("Quality catalog summary resolution contains an invalid reference")
		}
	}
	var response ResolveQualityCatalogSummariesResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/quality/runtime/catalog-summaries/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("Quality resolve catalog summaries: %w", err)
	}
	if len(response.Results) != len(references) {
		return nil, errors.New("Quality catalog summary resolution returned a result count mismatch")
	}
	for index, result := range response.Results {
		if result.Reference != references[index] || result.OpenIssueCount < 0 || (result.Configured && result.CheckTaskID <= 0) || result.QualityScore != nil && (*result.QualityScore < 0 || *result.QualityScore > 100) {
			return nil, errors.New("Quality catalog summary resolution returned an invalid result")
		}
	}
	return &response, nil
}
