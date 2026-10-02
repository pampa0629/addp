package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/addp/common/authorization"
	"github.com/google/uuid"
)

// SystemFulfillmentClient exposes recovery only, never new acceptance or Grant.
type SystemFulfillmentClient struct{ tenantHTTPClient }

func NewSystemFulfillmentClient(baseURL string, tokens ServiceTokenProvider, httpClient *http.Client) *SystemFulfillmentClient {
	return &SystemFulfillmentClient{newTenantHTTPClient(baseURL, tokens, httpClient)}
}

func (c *SystemFulfillmentClient) WithTenantID(id uint) *SystemFulfillmentClient {
	return &SystemFulfillmentClient{c.tenantHTTPClient.withTenantID(id)}
}

func (c *SystemFulfillmentClient) Resolve(ctx context.Context, id uuid.UUID, binding authorization.SharingFulfillmentBinding) (*authorization.SharingFulfillmentLookup, error) {
	if id == uuid.Nil {
		return nil, errors.New("fulfillment request ID required")
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	var result struct {
		Found      *bool                                       `json:"found"`
		Resolution *authorization.SharingFulfillmentResolution `json:"resolution"`
	}
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/system/runtime/engine-access-fulfillments/"+id.String()+"/resolve", binding, &result)
	if err != nil {
		return nil, err
	}
	if result.Found == nil || *result.Found != (result.Resolution != nil) {
		return nil, errors.New("invalid fulfillment lookup")
	}
	return &authorization.SharingFulfillmentLookup{Found: *result.Found, Resolution: result.Resolution}, nil
}

func (c *SystemFulfillmentClient) Close(ctx context.Context, id uuid.UUID, binding authorization.SharingFulfillmentBinding) (*authorization.SharingFulfillmentResolution, error) {
	if id == uuid.Nil {
		return nil, errors.New("fulfillment request ID required")
	}
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	var result authorization.SharingFulfillmentResolution
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/system/runtime/engine-access-fulfillments/"+id.String()+"/close", binding, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
