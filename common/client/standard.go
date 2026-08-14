package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/addp/common/dataquality"
)

// ErrTenantReferenceNotFound deliberately conflates a missing resource with a
// cross-tenant resource so callers cannot use validation APIs for tenant probing.
var ErrTenantReferenceNotFound = errors.New("tenant reference not found")

// ErrStandardReferenceDeleting means the Standard owner has frozen new bindings
// while coordinating a hard delete.
var ErrStandardReferenceDeleting = errors.New("standard reference deleting")

// StandardClient is the Bearer-only client for tenant-owned Standard APIs.
type StandardClient struct{ tenantHTTPClient }

func NewStandardClient(baseURL string, tokenSource ServiceTokenProvider, httpClient *http.Client) *StandardClient {
	return &StandardClient{tenantHTTPClient: newTenantHTTPClient(baseURL, tokenSource, httpClient)}
}

func (c *StandardClient) WithTenantID(tenantID uint) *StandardClient {
	if c == nil {
		return nil
	}
	return &StandardClient{tenantHTTPClient: c.tenantHTTPClient.withTenantID(tenantID)}
}

type ElementResponse struct {
	ID             int64                `json:"id"`
	TenantID       int64                `json:"tenant_id"`
	Name           string               `json:"name"`
	Code           string               `json:"code"`
	DataType       string               `json:"data_type"`
	CodeSetID      *int64               `json:"code_set_id"`
	QualityRules   dataquality.Document `json:"quality_rules"`
	LifecycleState string               `json:"lifecycle_state"`
}

type ElementSummary struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}

type ElementCandidate struct {
	ID           int64                `json:"id"`
	Name         string               `json:"name"`
	Code         string               `json:"code"`
	QualityRules dataquality.Document `json:"quality_rules"`
}

type elementListResponse struct {
	Data []ElementSummary `json:"data"`
}

type elementCandidateListResponse struct {
	Data  []ElementCandidate `json:"data"`
	Total int64              `json:"total"`
}

type tenantReferenceResponse struct {
	ID             int64  `json:"id"`
	TenantID       int64  `json:"tenant_id"`
	LifecycleState string `json:"lifecycle_state"`
}

func (c *StandardClient) validateTenantReference(ctx context.Context, path, resourceName string) error {
	var resource tenantReferenceResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resource); err != nil {
		return fmt.Errorf("standard validate %s: %w", resourceName, err)
	}
	if c.tenantID == nil || resource.TenantID != int64(*c.tenantID) {
		return fmt.Errorf("standard validate %s: %w", resourceName, ErrTenantReferenceNotFound)
	}
	if resource.LifecycleState != "active" {
		return fmt.Errorf("standard validate %s: %w", resourceName, ErrStandardReferenceDeleting)
	}
	return nil
}

func (c *StandardClient) ValidateElement(ctx context.Context, elementID int64) error {
	var element ElementResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d", elementID), nil, &element); err != nil {
		return fmt.Errorf("standard validate element: %w", err)
	}
	if c.tenantID == nil || element.TenantID != int64(*c.tenantID) {
		return fmt.Errorf("standard validate element: %w", ErrTenantReferenceNotFound)
	}
	if element.LifecycleState != "active" {
		return fmt.Errorf("standard validate element: %w", ErrStandardReferenceDeleting)
	}
	return nil
}

func (c *StandardClient) GetElement(ctx context.Context, elementID int64) (*ElementResponse, error) {
	var element ElementResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d", elementID), nil, &element); err != nil {
		return nil, fmt.Errorf("standard get element: %w", err)
	}
	return &element, nil
}

func (c *StandardClient) ListElementSummaries(ctx context.Context, elementIDs []int64) ([]ElementSummary, error) {
	if len(elementIDs) == 0 {
		return []ElementSummary{}, nil
	}
	values := make([]string, len(elementIDs))
	for index, elementID := range elementIDs {
		if elementID <= 0 {
			return nil, errors.New("standard list elements requires positive ids")
		}
		values[index] = strconv.FormatInt(elementID, 10)
	}
	query := url.Values{
		"ids":       []string{strings.Join(values, ",")},
		"page":      []string{"1"},
		"page_size": []string{"100"},
	}
	var response elementListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/standard/elements?"+query.Encode(), nil, &response); err != nil {
		return nil, fmt.Errorf("standard list elements: %w", err)
	}
	return response.Data, nil
}

func (c *StandardClient) ListElementCandidates(ctx context.Context, keyword string, page, pageSize int) ([]ElementCandidate, int64, error) {
	query := url.Values{
		"keyword":   []string{keyword},
		"page":      []string{strconv.Itoa(page)},
		"page_size": []string{strconv.Itoa(pageSize)},
	}
	var response elementCandidateListResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/standard/elements?"+query.Encode(), nil, &response); err != nil {
		return nil, 0, fmt.Errorf("standard list element candidates: %w", err)
	}
	for index := range response.Data {
		if err := response.Data[index].QualityRules.Validate(); err != nil {
			return nil, 0, fmt.Errorf("standard returned invalid element candidate quality rules: %w", err)
		}
	}
	return response.Data, response.Total, nil
}

func (c *StandardClient) GetElementQualityRules(ctx context.Context, elementID int64) (*dataquality.Document, error) {
	var result dataquality.Document
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d/quality-rules", elementID), nil, &result); err != nil {
		return nil, fmt.Errorf("standard get element quality rules: %w", err)
	}
	if err := result.Validate(); err != nil {
		return nil, fmt.Errorf("standard returned invalid element quality rules: %w", err)
	}
	return &result, nil
}

func (c *StandardClient) ValidateDomain(ctx context.Context, domainID int64) error {
	return c.validateTenantReference(ctx, fmt.Sprintf("/api/v1/standard/domains/%d", domainID), "domain")
}

func (c *StandardClient) ValidateDimensionHierarchy(ctx context.Context, hierarchyID int64) error {
	return c.validateTenantReference(ctx, fmt.Sprintf("/api/v1/standard/dimension-hierarchies/%d", hierarchyID), "hierarchy")
}

func (c *StandardClient) ValidateMetric(ctx context.Context, metricID int64) error {
	return c.validateTenantReference(ctx, fmt.Sprintf("/api/v1/standard/metrics/%d", metricID), "metric")
}
