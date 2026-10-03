package client

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/addp/common/authorization"
	"github.com/google/uuid"
)

// SystemFulfillmentClient transports preparation identity, acceptance and
// recovery. None of these operations writes a resource Grant.
type SystemFulfillmentClient struct{ tenantHTTPClient }

func NewSystemFulfillmentClient(baseURL string, tokens ServiceTokenProvider, httpClient *http.Client) *SystemFulfillmentClient {
	return &SystemFulfillmentClient{newTenantHTTPClient(baseURL, tokens, httpClient)}
}

func (c *SystemFulfillmentClient) WithTenantID(id uint) *SystemFulfillmentClient {
	return &SystemFulfillmentClient{c.tenantHTTPClient.withTenantID(id)}
}

// CurrentPrincipal derives the caller from System's authoritative AuthContext,
// not deployment configuration, user input or local token parsing.
func (c *SystemFulfillmentClient) CurrentPrincipal(ctx context.Context) (int64, error) {
	var ac authorization.AuthContext
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/auth/context", nil, &ac); err != nil {
		return 0, err
	}
	if ac.Principal.Type != "service_principal" || ac.Context.Type != "tenant" || ac.Context.TenantID == nil ||
		ac.Client.ClientID == nil || *ac.Client.ClientID != "addp-catalog" || c.tenantID == nil ||
		*ac.Context.TenantID != strconv.FormatUint(uint64(*c.tenantID), 10) {
		return 0, errors.New("unexpected fulfillment caller")
	}
	id, err := strconv.ParseInt(ac.Principal.ID, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid fulfillment caller")
	}
	return id, nil
}

func (c *SystemFulfillmentClient) Accept(ctx context.Context, id uuid.UUID, binding authorization.SharingFulfillmentBinding) (*authorization.SharingFulfillmentResolution, error) {
	if id == uuid.Nil || binding.Validate() != nil {
		return nil, errors.New("invalid fulfillment request")
	}
	var result authorization.SharingFulfillmentResolution
	err := c.doJSON(ctx, http.MethodPost, "/api/v1/system/runtime/engine-access-fulfillments/"+id.String()+"/accept", binding, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
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
