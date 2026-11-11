package client

import (
	"context"
	"errors"
	"net/http"
)

type WorkflowGeoJSONAccessRequest struct {
	ExecutionID string `json:"execution_id"`
	Attempt     int    `json:"attempt"`
	LeaseToken  string `json:"lease_token"`
	NodeID      string `json:"node_id"`
}

func (c *SystemServiceClient) CheckWorkflowGeoJSONAccess(ctx context.Context, request WorkflowGeoJSONAccessRequest) (*ManagerFileAccessObservation, error) {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 || !canonicalExecutionUUID(request.ExecutionID) || !canonicalExecutionUUID(request.LeaseToken) || request.Attempt != 1 || request.NodeID == "" {
		return nil, errors.New("invalid workflow GeoJSON access request")
	}
	token, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil {
		return nil, err
	}
	var result ManagerFileAccessObservation
	err = c.executionAuthorizationJSON(ctx, "/api/v1/system/execution-authorizations/develop-geojson/access", token, request, &result, http.StatusOK)
	if err != nil {
		return nil, err
	}
	if result.Engine == nil || result.ObservedAt.IsZero() {
		return nil, errors.New("incomplete GeoJSON observation")
	}
	return &result, nil
}
