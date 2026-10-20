package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/addp/common/authorization"
	"github.com/google/uuid"
)

func (c *SystemServiceClient) productionGrantHTTP() (tenantHTTPClient, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 {
		return tenantHTTPClient{}, errors.New("initial production requires immutable Tenant context")
	}
	return newTenantHTTPClient(c.baseURL, c.tenantTokens, c.httpClient).withTenantID(*c.tenantID), nil
}

func productionGrantPath(id string) (string, error) {
	value, err := uuid.Parse(id)
	if err != nil || value == uuid.Nil || value.String() != id {
		return "", errors.New("canonical production execution UUID required")
	}
	return "/api/v1/system/runtime/initial_production_grants/transfer/" + id, nil
}

func (c *SystemServiceClient) ResolveInitialTransferGrant(ctx context.Context, id string) (*authorization.InitialProductionLookup, error) {
	httpClient, err := c.productionGrantHTTP()
	if err != nil {
		return nil, err
	}
	path, err := productionGrantPath(id)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Found  *bool                                  `json:"found"`
		Result *authorization.InitialProductionResult `json:"result"`
	}
	if err := httpClient.doStrictJSON(ctx, http.MethodGet, path, &wire, 64<<10); err != nil {
		return nil, err
	}
	if wire.Found == nil {
		return nil, errors.New("initial production lookup omitted found")
	}
	result := &authorization.InitialProductionLookup{Found: *wire.Found, Result: wire.Result}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	if result.Found && (result.Result.ExecutionID.String() != id || result.Result.TenantID != int64(*c.tenantID)) {
		return nil, errors.New("initial production history identity mismatch")
	}
	return result, nil
}

func (c *SystemServiceClient) IssueInitialTransferGrant(ctx context.Context, id string) (*authorization.InitialProductionResult, error) {
	httpClient, err := c.productionGrantHTTP()
	if err != nil {
		return nil, err
	}
	path, err := productionGrantPath(id)
	if err != nil {
		return nil, err
	}
	var result authorization.InitialProductionResult
	if err := httpClient.doStrictJSON(ctx, http.MethodPost, path, &result, 64<<10); err != nil {
		return nil, err
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	if result.ExecutionID.String() != id || result.TenantID != int64(*c.tenantID) {
		return nil, errors.New("initial production issuance identity mismatch")
	}
	return &result, nil
}
