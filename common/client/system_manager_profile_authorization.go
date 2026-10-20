package client

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/addp/common/execution"
)

type IssueManagerProfileAuthorizationRequest struct {
	ExecutionID string `json:"execution_id"`
	ExpiresIn   int64  `json:"expires_in,omitempty"`
}

type IssuedManagerProfileAuthorization struct {
	ID              string                            `json:"id"`
	ExecutionID     string                            `json:"execution_id"`
	TenantID        string                            `json:"tenant_id"`
	Audience        string                            `json:"audience"`
	SourceReadScope execution.ManagerProfileReadScope `json:"source_read_scope"`
	ExpiresAt       time.Time                         `json:"expires_at"`
}

type ManagerProfileAccessRequest struct {
	ExecutionID     string                            `json:"execution_id"`
	Attempt         int                               `json:"attempt"`
	LeaseToken      string                            `json:"lease_token"`
	SourceReadScope execution.ManagerProfileReadScope `json:"source_read_scope"`
}

type ManagerProfileAccessObservation struct {
	ObservedAt time.Time `json:"observed_at"`
}

// IssueManagerProfile retains the User credential only in this synchronous
// call. Tenant and complete scope are resolved by System, not supplied here.
func (c *SystemExecutionAuthorizationClient) IssueManagerProfile(ctx context.Context, credential string, request IssueManagerProfileAuthorizationRequest) (*IssuedManagerProfileAuthorization, error) {
	if c == nil || c.system == nil || !validExecutionBearer(credential) ||
		!canonicalExecutionUUID(request.ExecutionID) || request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
		return nil, errors.New("invalid Manager profile issuance input")
	}
	var response IssuedManagerProfileAuthorization
	if err := c.system.executionAuthorizationJSON(ctx, "/api/v1/system/auth/execution-authorizations/manager-profiles", credential, request, &response, http.StatusCreated); err != nil {
		return nil, err
	}
	_, idErr := parseCanonicalPositiveID(response.ID)
	_, tenantErr := parseCanonicalPositiveID(response.TenantID)
	if idErr != nil || tenantErr != nil || response.ExecutionID != request.ExecutionID || response.Audience != "manager" || response.SourceReadScope.Validate() != nil ||
		!response.ExpiresAt.After(time.Now()) || response.ExpiresAt.After(time.Now().Add(time.Hour+5*time.Second)) {
		return nil, errors.New("invalid Manager profile issuance response")
	}
	response.SourceReadScope = *response.SourceReadScope.Clone()
	return &response, nil
}

// CheckManagerProfileAccess makes one fixed consumption request using the
// current Tenant Service credential, never a saved User token or cached Allow.
func (c *SystemServiceClient) CheckManagerProfileAccess(ctx context.Context, id string, request ManagerProfileAccessRequest) (*ManagerProfileAccessObservation, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, errors.New("Manager profile consumption requires a Tenant Service client")
	}
	if _, err := parseCanonicalPositiveID(id); err != nil {
		return nil, errors.New("invalid Manager profile authorization ID")
	}
	if !canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) || request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
		return nil, errors.New("invalid Manager profile consumption input")
	}
	credential, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil || !validExecutionBearer(credential) {
		return nil, errors.New("Manager profile Service credential unavailable")
	}
	var response ManagerProfileAccessObservation
	path := "/api/v1/system/execution-authorizations/" + id + "/manager-profile-accesses"
	if err := c.executionAuthorizationJSON(ctx, path, credential, request, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if response.ObservedAt.IsZero() {
		return nil, errors.New("incomplete Manager profile observation")
	}
	return &response, nil
}

// Matches validates the owner binding before any execution reference is saved.
func (r *IssuedManagerProfileAuthorization) Matches(tenant uint, scope execution.ManagerProfileReadScope) bool {
	if r == nil || tenant == 0 || r.TenantID != strconv.FormatUint(uint64(tenant), 10) || r.Audience != "manager" || !canonicalExecutionUUID(r.ExecutionID) || !r.ExpiresAt.After(time.Now()) || scope.Validate() != nil {
		return false
	}
	_, err := parseCanonicalPositiveID(r.ID)
	return err == nil && reflect.DeepEqual(r.SourceReadScope, scope)
}
