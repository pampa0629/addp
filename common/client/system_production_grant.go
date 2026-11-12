package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/addp/common/production"
	"github.com/addp/common/authorization"
)

func (c *SystemServiceClient) productionGrantHTTP() (tenantHTTPClient, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 {
		return tenantHTTPClient{}, errors.New("initial production requires immutable Tenant context")
	}
	return newTenantHTTPClient(c.baseURL, c.tenantTokens, c.httpClient).withTenantID(*c.tenantID), nil
}

func productionGrantPath(ref production.Reference) (string, error) {
	if ref.Validate() != nil {
		return "", errors.New("canonical production execution UUID required")
	}
	return "/api/v1/system/runtime/initial_production_grants/" + ref.Owner + "/" + ref.ExecutionID + "/" + ref.ResultID, nil
}

func (c *SystemServiceClient) ResolveInitialProductionGrant(ctx context.Context, ref production.Reference) (*authorization.InitialProductionLookup, error) {
	httpClient, err := c.productionGrantHTTP()
	if err != nil {
		return nil, err
	}
	path, err := productionGrantPath(ref)
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
	if result.Found && (result.Result.ProducerOwner != ref.Owner || result.Result.ResultID.String() != ref.ResultID || result.Result.ExecutionID.String() != ref.ExecutionID || result.Result.TenantID != int64(*c.tenantID)) {
		return nil, errors.New("initial production history identity mismatch")
	}
	return result, nil
}

func (c *SystemServiceClient) IssueInitialProductionGrant(ctx context.Context, ref production.Reference) (*authorization.InitialProductionResult, error) {
	httpClient, err := c.productionGrantHTTP()
	if err != nil {
		return nil, err
	}
	path, err := productionGrantPath(ref)
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
	if result.ProducerOwner != ref.Owner || result.ResultID.String() != ref.ResultID || result.ExecutionID.String() != ref.ExecutionID || result.TenantID != int64(*c.tenantID) {
		return nil, errors.New("initial production issuance identity mismatch")
	}
	return &result, nil
}
