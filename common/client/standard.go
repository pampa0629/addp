package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/addp/common/dataquality"
)

// ErrTenantReferenceNotFound deliberately conflates a missing resource with a
// cross-tenant resource so callers cannot use validation APIs for tenant probing.
var ErrTenantReferenceNotFound = errors.New("tenant reference not found")

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
	ID           int64                `json:"id"`
	TenantID     int64                `json:"tenant_id"`
	Name         string               `json:"name"`
	Code         string               `json:"code"`
	DataType     string               `json:"data_type"`
	CodeSetID    *int64               `json:"code_set_id"`
	QualityRules dataquality.Document `json:"quality_rules"`
}

type tenantReferenceResponse struct {
	ID       int64 `json:"id"`
	TenantID int64 `json:"tenant_id"`
}

func (c *StandardClient) validateTenantReference(ctx context.Context, path, resourceName string) error {
	var resource tenantReferenceResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resource); err != nil {
		return fmt.Errorf("standard validate %s: %w", resourceName, err)
	}
	if c.tenantID == nil || resource.TenantID != int64(*c.tenantID) {
		return fmt.Errorf("standard validate %s: %w", resourceName, ErrTenantReferenceNotFound)
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
	return nil
}

func (c *StandardClient) GetElement(ctx context.Context, elementID int64) (*ElementResponse, error) {
	var element ElementResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d", elementID), nil, &element); err != nil {
		return nil, fmt.Errorf("standard get element: %w", err)
	}
	return &element, nil
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
