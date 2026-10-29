package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

type CleanupAccessRequest struct {
	ExecutionID string                              `json:"execution_id"`
	Stage       string                              `json:"stage"`
	Attempt     int                                 `json:"attempt"`
	LeaseToken  string                              `json:"lease_token,omitempty"`
	Scope       execution.CleanupAuthorizationScope `json:"scope"`
}

type CleanupAccess struct {
	AuthorizationID string                              `json:"authorization_id"`
	ExecutionID     string                              `json:"execution_id"`
	TenantID        string                              `json:"tenant_id"`
	Stage           string                              `json:"stage"`
	Attempt         int                                 `json:"attempt"`
	Scope           execution.CleanupAuthorizationScope `json:"scope"`
	ExpiresAt       time.Time                           `json:"expires_at"`
}

func (a *CleanupAccess) UnmarshalJSON(data []byte) error {
	type closed CleanupAccess
	var value closed
	if err := events.CleanupStrictDecode(data, &value); err != nil {
		return err
	}
	*a = CleanupAccess(value)
	return nil
}

// GetCleanupAccess obtains a fresh decision, never caches authority and never
// returns engine credentials. Owners still fence each domain commit with C.
func (c *SystemServiceClient) GetCleanupAccess(ctx context.Context, authorizationID string, request CleanupAccessRequest) (*CleanupAccess, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || c.httpClient == nil {
		return nil, fmt.Errorf("tenant service client required")
	}
	if _, err := parseCanonicalPositiveID(authorizationID); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(request.ExecutionID)
	if err != nil || id == uuid.Nil || id.String() != request.ExecutionID || request.Scope.Validate() != nil {
		return nil, fmt.Errorf("invalid cleanup access boundary")
	}
	switch request.Stage {
	case "claim":
		if request.Attempt != 0 || request.LeaseToken != "" {
			return nil, fmt.Errorf("invalid cleanup claim boundary")
		}
	case "effect":
		token, err := uuid.Parse(request.LeaseToken)
		if err != nil || token == uuid.Nil || token.String() != request.LeaseToken || request.Attempt <= 0 {
			return nil, fmt.Errorf("invalid cleanup lease boundary")
		}
	default:
		return nil, fmt.Errorf("invalid cleanup authorization stage")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	bounded := *c
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bounded.httpClient = &transport
	var result CleanupAccess
	path := fmt.Sprintf("/api/v1/system/execution-authorizations/%s/cleanup-accesses", authorizationID)
	if err := bounded.doTenantJSON(ctx, http.MethodPost, path, request, &result, events.CleanupProtocolMaxBytes); err != nil {
		return nil, err
	}
	if result.AuthorizationID != authorizationID || result.ExecutionID != request.ExecutionID || result.TenantID != strconv.FormatUint(uint64(*c.tenantID), 10) || result.Stage != request.Stage || result.Attempt != request.Attempt || result.Scope != request.Scope || !result.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("System cleanup access returned an invalid response")
	}
	return &result, nil
}
