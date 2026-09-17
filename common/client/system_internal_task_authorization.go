package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/execution"
	"github.com/google/uuid"
)

type InternalTaskAccessRequest struct {
	ExecutionID  string                      `json:"execution_id"`
	Attempt      int                         `json:"attempt"`
	LeaseToken   string                      `json:"lease_token"`
	InternalTask execution.InternalTaskScope `json:"internal_task"`
}

type InternalTaskAccess struct {
	AuthorizationID string                      `json:"authorization_id"`
	ExecutionID     string                      `json:"execution_id"`
	TenantID        string                      `json:"tenant_id"`
	Audience        string                      `json:"audience"`
	Attempt         int                         `json:"attempt"`
	InternalTask    execution.InternalTaskScope `json:"internal_task"`
	ExpiresAt       time.Time                   `json:"expires_at"`
}

func (c *SystemServiceClient) GetInternalTaskAccess(ctx context.Context, authorizationID string, request InternalTaskAccessRequest) (*InternalTaskAccess, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, fmt.Errorf("tenant service client required")
	}
	if _, err := parseCanonicalPositiveID(authorizationID); err != nil {
		return nil, err
	}
	lease, leaseErr := uuid.Parse(request.LeaseToken)
	id, idErr := uuid.Parse(request.ExecutionID)
	if request.Attempt <= 0 || leaseErr != nil || lease == uuid.Nil || lease.String() != request.LeaseToken ||
		idErr != nil || id == uuid.Nil || id.String() != request.ExecutionID || request.InternalTask.Validate(execution.AudienceOntology) != nil {
		return nil, fmt.Errorf("invalid internal task access request")
	}
	var response InternalTaskAccess
	path := fmt.Sprintf("/api/v1/system/execution-authorizations/%s/internal-task-accesses", authorizationID)
	if err := c.doTenantJSON(ctx, http.MethodPost, path, request, &response); err != nil {
		return nil, err
	}
	if response.AuthorizationID != authorizationID || response.ExecutionID != request.ExecutionID ||
		response.TenantID != strconv.FormatUint(uint64(*c.tenantID), 10) || response.Audience != execution.AudienceOntology ||
		response.Attempt != request.Attempt || response.InternalTask != request.InternalTask || !response.ExpiresAt.After(time.Now()) {
		return nil, fmt.Errorf("System internal task access returned an invalid response")
	}
	return &response, nil
}
