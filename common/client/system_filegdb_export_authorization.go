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

type IssuedFileGDBExportAuthorization struct {
	ID              string                           `json:"id"`
	ExecutionID     string                           `json:"execution_id"`
	TenantID        string                           `json:"tenant_id"`
	Audience        string                           `json:"audience"`
	SourceReadScope execution.FileGDBExportReadScope `json:"source_read_scope"`
	ExpiresAt       time.Time                        `json:"expires_at"`
}

// FileGDBExportActor is frozen by the Manager's authenticated export request.
// System independently verifies the same user against the current credential.
type FileGDBExportActor struct {
	PrincipalID          int64 `json:"principal_id"`
	TenantMembershipID   int64 `json:"tenant_membership_id"`
	AuthorizationVersion int64 `json:"authorization_version"`
}

func (a *IssuedFileGDBExportAuthorization) Matches(tenant uint, id string, scope *execution.FileGDBExportReadScope) bool {
	if a == nil || tenant == 0 || !canonicalExecutionUUID(id) || scope.Validate() != nil {
		return false
	}
	_, err := parseCanonicalPositiveID(a.ID)
	return err == nil && a.ExecutionID == id && a.TenantID == strconv.FormatUint(uint64(tenant), 10) &&
		a.Audience == execution.AudienceTransfer && a.ExpiresAt.After(time.Now()) &&
		reflect.DeepEqual(a.SourceReadScope, *scope)
}

func (c *TransferClient) BindFileGDBExportAuthorization(ctx context.Context, tenant uint, issued *IssuedFileGDBExportAuthorization) error {
	if c == nil || issued == nil || !issued.Matches(tenant, issued.ExecutionID, &issued.SourceReadScope) {
		return errors.New("invalid FileGDB export authorization")
	}
	transport := newTenantHTTPClient(c.baseURL, c.tokenSource, c.httpClient).withTenantID(tenant)
	return transport.doJSON(ctx, http.MethodPut, "/api/v1/transfer/executions/"+issued.ExecutionID+"/filegdb-source-authorization", issued, nil)
}

type FileGDBExportAccessRequest struct {
	ExecutionID     string                           `json:"execution_id"`
	Attempt         int                              `json:"attempt"`
	LeaseToken      string                           `json:"lease_token"`
	SourceReadScope execution.FileGDBExportReadScope `json:"source_read_scope"`
}

func (c *SystemExecutionAuthorizationClient) IssueFileGDBExport(ctx context.Context, credential string, request IssueManagerFileAuthorizationRequest) (*IssuedFileGDBExportAuthorization, error) {
	if c == nil || c.system == nil || !validExecutionBearer(credential) || !canonicalExecutionUUID(request.ExecutionID) ||
		request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
		return nil, errors.New("invalid FileGDB export authorization input")
	}
	var response IssuedFileGDBExportAuthorization
	if err := c.system.executionAuthorizationJSON(ctx, "/api/v1/system/auth/execution-authorizations/filegdb-export",
		credential, request, &response, http.StatusCreated); err != nil {
		return nil, err
	}
	_, idErr := parseCanonicalPositiveID(response.ID)
	_, tenantErr := parseCanonicalPositiveID(response.TenantID)
	if idErr != nil || tenantErr != nil || response.ExecutionID != request.ExecutionID || response.Audience != execution.AudienceTransfer ||
		response.SourceReadScope.Validate() != nil || !response.ExpiresAt.After(time.Now()) ||
		response.ExpiresAt.After(time.Now().Add(time.Hour+5*time.Second)) {
		return nil, errors.New("invalid FileGDB export authorization response")
	}
	return &response, nil
}

func (c *SystemServiceClient) CheckFileGDBExportAccess(ctx context.Context, id string, request FileGDBExportAccessRequest) (*ManagerFileAccessObservation, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, errors.New("FileGDB export access requires Tenant Service context")
	}
	if _, err := parseCanonicalPositiveID(id); err != nil {
		return nil, err
	}
	if !canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) ||
		request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
		return nil, errors.New("invalid FileGDB export access input")
	}
	credential, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil || !validExecutionBearer(credential) {
		return nil, errors.New("FileGDB export Service credential unavailable")
	}
	request.SourceReadScope = *request.SourceReadScope.Clone()
	var response ManagerFileAccessObservation
	if err := c.executionAuthorizationJSON(ctx, "/api/v1/system/execution-authorizations/"+id+"/filegdb-export-accesses",
		credential, request, &response, http.StatusOK); err != nil {
		return nil, err
	}
	if !response.MatchesFileGDBExportSource(*c.tenantID, request.SourceReadScope.FileGDBSource) {
		return nil, errors.New("invalid FileGDB export engine observation")
	}
	return &response, nil
}

func (r *ManagerFileAccessObservation) MatchesFileGDBExportSource(tenant uint, source execution.FileGDBExportSource) bool {
	if r == nil || r.ObservedAt.IsZero() || r.Engine == nil || tenant == 0 || source.Validate() != nil {
		return false
	}
	engine := r.Engine
	version, _ := strconv.ParseInt(source.EngineVersion, 10, 64)
	return engine.ID == source.Binding.NativeTarget.EngineID && engine.Version == version && engine.EngineType == source.EngineType() &&
		engine.TenantID != nil && *engine.TenantID == tenant && engine.LifecycleState == "active" &&
		engine.DeletedAt == nil && len(engine.ConnectionInfo) > 0
}
