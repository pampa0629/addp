package client

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
)

type IssueManagerModel3DAuthorizationRequest struct {
	ExecutionID string `json:"execution_id"`
	ExpiresIn   int64  `json:"expires_in,omitempty"`
}

type IssuedManagerModel3DAuthorization struct {
	ID              string                            `json:"id"`
	ExecutionID     string                            `json:"execution_id"`
	TenantID        string                            `json:"tenant_id"`
	Audience        string                            `json:"audience"`
	SourceReadScope execution.ManagerModel3DReadScope `json:"source_read_scope"`
	ExpiresAt       time.Time                         `json:"expires_at"`
}

type ManagerModel3DAccessRequest struct {
	ExecutionID     string                            `json:"execution_id"`
	Attempt         int                               `json:"attempt"`
	LeaseToken      string                            `json:"lease_token"`
	SourceReadScope execution.ManagerModel3DReadScope `json:"source_read_scope"`
}

// ManagerModel3DAccessObservation is an uncached current response. The Engine
// connection may be used only for this guarded source I/O, never persisted.
type ManagerModel3DAccessObservation struct {
	ObservedAt time.Time      `json:"observed_at"`
	Engine     *models.Engine `json:"engine"`
}

func (c *SystemExecutionAuthorizationClient) IssueManagerModel3D(ctx context.Context, credential string, request IssueManagerModel3DAuthorizationRequest) (*IssuedManagerModel3DAuthorization, error) {
	if c == nil || c.system == nil || !validExecutionBearer(credential) || !canonicalExecutionUUID(request.ExecutionID) || request.ExpiresIn < 0 || request.ExpiresIn > 3600 {
		return nil, errors.New("invalid Manager model3d issuance input")
	}
	var response IssuedManagerModel3DAuthorization
	if err := c.system.executionAuthorizationJSON(ctx, "/api/v1/system/auth/execution-authorizations/manager-model3d-glb", credential, request, &response, http.StatusCreated); err != nil {
		return nil, err
	}
	_, idErr := parseCanonicalPositiveID(response.ID)
	_, tenantErr := parseCanonicalPositiveID(response.TenantID)
	if idErr != nil || tenantErr != nil || response.ExecutionID != request.ExecutionID || response.Audience != "manager" || response.SourceReadScope.Validate() != nil ||
		!response.ExpiresAt.After(time.Now()) || response.ExpiresAt.After(time.Now().Add(time.Hour+5*time.Second)) {
		return nil, errors.New("invalid Manager model3d issuance response")
	}
	response.SourceReadScope = *response.SourceReadScope.Clone()
	return &response, nil
}

func (r *IssuedManagerModel3DAuthorization) Matches(tenant uint, scope execution.ManagerModel3DReadScope) bool {
	if r == nil || tenant == 0 || r.TenantID != strconv.FormatUint(uint64(tenant), 10) || r.Audience != "manager" || !canonicalExecutionUUID(r.ExecutionID) || !r.ExpiresAt.After(time.Now()) || scope.Validate() != nil {
		return false
	}
	_, err := parseCanonicalPositiveID(r.ID)
	return err == nil && reflect.DeepEqual(r.SourceReadScope, scope)
}

func (c *SystemServiceClient) CheckManagerModel3DAccess(ctx context.Context, id string, request ManagerModel3DAccessRequest) (*ManagerModel3DAccessObservation, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, errors.New("Manager model3d consumption requires Tenant Service context")
	}
	if _, err := parseCanonicalPositiveID(id); err != nil {
		return nil, errors.New("invalid Manager model3d authorization ID")
	}
	if !canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) || request.Attempt <= 0 || request.SourceReadScope.Validate() != nil {
		return nil, errors.New("invalid Manager model3d consumption input")
	}
	credential, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil || !validExecutionBearer(credential) {
		return nil, errors.New("Manager model3d Service credential unavailable")
	}
	request.SourceReadScope = *request.SourceReadScope.Clone()
	var response ManagerModel3DAccessObservation
	path := "/api/v1/system/execution-authorizations/" + id + "/manager-model3d-glb-accesses"
	if err := c.executionAuthorizationJSON(ctx, path, credential, request, &response, http.StatusOK); err != nil {
		return nil, err
	}
	primary, _ := request.SourceReadScope.FileComposition.PrimaryTarget()
	version, _ := strconv.ParseInt(request.SourceReadScope.EngineVersion, 10, 64)
	engine := response.Engine
	if response.ObservedAt.IsZero() || engine == nil || engine.ID != primary.EngineID || engine.TenantID == nil || *engine.TenantID != *c.tenantID ||
		engine.Version != version || !execution.ManagerModel3DSourceEngineType(engine.EngineType) || engine.LifecycleState != "active" || engine.DeletedAt != nil || len(engine.ConnectionInfo) == 0 {
		return nil, errors.New("invalid current file engine access observation")
	}
	return &response, nil
}
