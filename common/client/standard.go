package client

import (
	"context"
	"fmt"
	"net/http"
)

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
	ID           int64                  `json:"id"`
	TenantID     int64                  `json:"tenant_id"`
	Name         string                 `json:"name"`
	Code         string                 `json:"code"`
	DataType     string                 `json:"data_type"`
	CodeSetID    *int64                 `json:"code_set_id"`
	QualityRules map[string]interface{} `json:"quality_rules"`
}

func (c *StandardClient) ValidateElement(ctx context.Context, elementID int64) error {
	var element ElementResponse
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d", elementID), nil, &element); err != nil {
		return fmt.Errorf("standard validate element: %w", err)
	}
	if c.tenantID == nil || element.TenantID != int64(*c.tenantID) {
		return fmt.Errorf("element_id %d belongs to tenant %d, not current tenant", elementID, element.TenantID)
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

func (c *StandardClient) GetElementQualityRules(ctx context.Context, elementID int64) (map[string]interface{}, error) {
	var result struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/standard/elements/%d/quality-rules", elementID), nil, &result); err != nil {
		return nil, fmt.Errorf("standard get element quality rules: %w", err)
	}
	return result.Data, nil
}
