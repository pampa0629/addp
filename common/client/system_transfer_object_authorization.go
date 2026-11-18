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

type IssuedTransferObjectAuthorization struct {
	ID              string                            `json:"id"`
	ExecutionID     string                            `json:"execution_id"`
	TenantID        string                            `json:"tenant_id"`
	Audience        string                            `json:"audience"`
	SourceReadScope execution.TransferObjectReadScope `json:"source_read_scope"`
	ExpiresAt       time.Time                         `json:"expires_at"`
}

func (a *IssuedTransferObjectAuthorization) Matches(tenant uint, id string, scope *execution.TransferObjectReadScope) bool {
	if a == nil || tenant == 0 || !canonicalExecutionUUID(id) || scope.Validate() != nil {
		return false
	}
	_, err := parseCanonicalPositiveID(a.ID)
	return err == nil && a.ExecutionID == id && a.TenantID == strconv.FormatUint(uint64(tenant), 10) &&
		a.Audience == execution.AudienceTransfer && a.ExpiresAt.After(time.Now()) &&
		reflect.DeepEqual(a.SourceReadScope, *scope)
}

type TransferObjectAccessRequest struct {
	ExecutionID     string                            `json:"execution_id"`
	Attempt         int                               `json:"attempt"`
	LeaseToken      string                            `json:"lease_token"`
	SourceReadScope execution.TransferObjectReadScope `json:"source_read_scope"`
}

func (c *SystemExecutionAuthorizationClient) IssueTransferObject(ctx context.Context, credential string, request IssueManagerFileAuthorizationRequest) (*IssuedTransferObjectAuthorization, error) {
	if c == nil || c.system == nil || !validExecutionBearer(credential) || !canonicalExecutionUUID(request.ExecutionID) ||
		request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
		return nil, errors.New("invalid Transfer object import authorization input")
	}
	var response IssuedTransferObjectAuthorization
	if err := c.system.executionAuthorizationJSON(ctx, "/api/v1/system/auth/execution-authorizations/transfer-objects",
		credential, request, &response, http.StatusCreated); err != nil {
		return nil, err
	}
	_, idErr := parseCanonicalPositiveID(response.ID)
	_, tenantErr := parseCanonicalPositiveID(response.TenantID)
	if idErr != nil || tenantErr != nil || response.ExecutionID != request.ExecutionID || response.Audience != execution.AudienceTransfer ||
		response.SourceReadScope.Validate() != nil || !response.ExpiresAt.After(time.Now()) ||
		response.ExpiresAt.After(time.Now().Add(time.Hour+5*time.Second)) {
		return nil, errors.New("invalid Transfer object import authorization response")
	}
	return &response, nil
}

func (c *SystemServiceClient) CheckTransferObjectAccess(ctx context.Context, id string, request TransferObjectAccessRequest) (*ManagerFileAccessObservation, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, errors.New("Transfer object import access requires Tenant Service context")
	}
	if _, err := parseCanonicalPositiveID(id); err != nil {
		return nil, err
	}
	if !canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) ||
		request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
		return nil, errors.New("invalid Transfer object import access input")
	}
	credential, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil || !validExecutionBearer(credential) {
		return nil, errors.New("Transfer object import Service credential unavailable")
	}
	request.SourceReadScope = *request.SourceReadScope.Clone()
	var response ManagerFileAccessObservation
	if err := c.executionAuthorizationJSON(ctx, "/api/v1/system/execution-authorizations/"+id+"/transfer-object-accesses",
		credential, request, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if !response.MatchesTransferObjectSource(*c.tenantID, request.SourceReadScope.ObjectSource) {
		return nil, errors.New("invalid Transfer object import engine observation")
	}
	return &response, nil
}

func (r *ManagerFileAccessObservation) MatchesTransferObjectSource(tenant uint, source execution.TransferObjectSource) bool {
	if r == nil || r.ObservedAt.IsZero() || r.Engine == nil || tenant == 0 || source.Validate() != nil {
		return false
	}
	engine := r.Engine
	version, _ := strconv.ParseInt(source.EngineVersion, 10, 64)
	return engine.ID == source.Binding.NativeTarget.EngineID && engine.Version == version && engine.EngineType == "rustfs" &&
		engine.TenantID != nil && *engine.TenantID == tenant && engine.LifecycleState == "active" &&
		engine.DeletedAt == nil && len(engine.ConnectionInfo) > 0
}
