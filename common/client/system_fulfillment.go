package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/addp/common/authorization"
	"github.com/google/uuid"
)

// SystemFulfillmentClient transports preparation, acceptance, recovery and
// exact original Grant issuance/history. It never owns or caches authority
// facts, and does not orchestrate these distinct operations automatically.
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

// IssueGrant sends only the exact original request. An uncertain response is
// an error, not permission to renew the window or create a replacement request.
func (c *SystemFulfillmentClient) IssueGrant(ctx context.Context, id uuid.UUID, binding authorization.SharingFulfillmentBinding) (*authorization.SharingFulfillmentGrant, error) {
	if id == uuid.Nil || binding.Validate() != nil {
		return nil, errors.New("invalid fulfillment grant request")
	}
	var result authorization.SharingFulfillmentGrant
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/system/runtime/engine-access-fulfillments/"+id.String()+"/grant", binding, &result); err != nil {
		return nil, err
	}
	if err := validateFulfillmentGrant(id, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ResolveGrant is pure history retrieval. Only an explicit valid miss can
// return Found=false; it never issues, closes or restores a revoked Grant.
func (c *SystemFulfillmentClient) ResolveGrant(ctx context.Context, id uuid.UUID, binding authorization.SharingFulfillmentBinding) (*authorization.SharingFulfillmentGrantLookup, error) {
	if id == uuid.Nil || binding.Validate() != nil {
		return nil, errors.New("invalid fulfillment grant request")
	}
	var result struct {
		Found *bool           `json:"found"`
		Grant json.RawMessage `json:"grant"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/system/runtime/engine-access-fulfillments/"+id.String()+"/grant/resolve", binding, &result); err != nil {
		return nil, err
	}
	if result.Found == nil {
		return nil, errors.New("invalid fulfillment grant lookup")
	}
	if !*result.Found {
		if len(result.Grant) != 0 {
			return nil, errors.New("invalid fulfillment grant lookup")
		}
		return &authorization.SharingFulfillmentGrantLookup{Found: false}, nil
	}
	var grant authorization.SharingFulfillmentGrant
	if err := json.Unmarshal(result.Grant, &grant); err != nil {
		return nil, errors.New("invalid fulfillment grant lookup")
	}
	if err := validateFulfillmentGrant(id, &grant); err != nil {
		return nil, err
	}
	return &authorization.SharingFulfillmentGrantLookup{Found: true, Grant: &grant}, nil
}

func validateFulfillmentGrant(id uuid.UUID, grant *authorization.SharingFulfillmentGrant) error {
	if grant == nil || grant.RequestID != id || grant.GrantedAt.IsZero() {
		return errors.New("invalid fulfillment grant history")
	}
	// System is the time authority. Historical issuance remains observable
	// after expiry/revocation, regardless of a client's clock.
	return nil
}
