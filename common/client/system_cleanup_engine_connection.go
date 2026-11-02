package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
)

type CleanupEngineConnectionRequest struct {
	ExecutionID string                              `json:"execution_id"`
	Attempt     int                                 `json:"attempt"`
	LeaseToken  string                              `json:"lease_token"`
	Scope       execution.CleanupAuthorizationScope `json:"scope"`
	EngineID    string                              `json:"engine_id"`
}

// CleanupConnectionEngine is deliberately smaller than an Engine detail DTO.
// It is ephemeral provider input, never public execution metadata or a receipt.
type CleanupConnectionEngine struct {
	ID             uint                  `json:"id"`
	TenantID       uint                  `json:"tenant_id"`
	EngineType     string                `json:"engine_type"`
	LifecycleState string                `json:"lifecycle_state"`
	Capabilities   *models.JSONString    `json:"capabilities"`
	ConnectionInfo models.ConnectionInfo `json:"connection_info"`
}

type CleanupEngineConnection struct {
	AuthorizationID string                              `json:"authorization_id"`
	ExecutionID     string                              `json:"execution_id"`
	TenantID        string                              `json:"tenant_id"`
	Attempt         int                                 `json:"attempt"`
	Scope           execution.CleanupAuthorizationScope `json:"scope"`
	Purpose         string                              `json:"purpose"`
	EngineID        string                              `json:"engine_id"`
	ExpiresAt       time.Time                           `json:"expires_at"`
	Engine          *CleanupConnectionEngine            `json:"engine"`
}

func (a *CleanupEngineConnection) UnmarshalJSON(data []byte) error {
	type closed CleanupEngineConnection
	var value closed
	if err := events.CleanupStrictDecode(data, &value); err != nil {
		return fmt.Errorf("invalid cleanup connection response")
	}
	*a = CleanupEngineConnection(value)
	return nil
}

// GetCleanupEngineConnection never retries, follows redirects, or archives an
// upstream body. Its separate purpose cannot be used for generic source reads.
func (c *SystemServiceClient) GetCleanupEngineConnection(ctx context.Context, authorizationID string, request CleanupEngineConnectionRequest) (*CleanupEngineConnection, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || c.httpClient == nil || c.tenantTokens == nil {
		return nil, fmt.Errorf("tenant service client required")
	}
	engineID, err := parseCanonicalPositiveID(request.EngineID)
	if err != nil || request.Scope.Validate() != nil || request.Scope.Module != events.ModuleTransfer || request.Attempt <= 0 {
		return nil, fmt.Errorf("invalid cleanup connection boundary")
	}
	if _, err := parseCanonicalPositiveID(authorizationID); err != nil {
		return nil, fmt.Errorf("invalid cleanup connection authorization")
	}
	for _, value := range []string{request.ExecutionID, request.LeaseToken} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return nil, fmt.Errorf("invalid cleanup connection identity")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	bounded := *c
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bounded.httpClient = &transport
	var result CleanupEngineConnection
	path := fmt.Sprintf("/api/v1/system/execution-authorizations/%s/cleanup-engine-connection", authorizationID)
	token, err := bounded.tenantTokens.Token(ctx, *bounded.tenantID)
	if err != nil {
		return nil, fmt.Errorf("cleanup connection identity unavailable")
	}
	if _, err := bounded.doJSON(ctx, http.MethodPost, path, token, request, &result, events.CleanupProtocolMaxBytes); err != nil {
		// Retain only the status: the shared error may contain the response body.
		// Never wrap a credential-shaped decoder or transport diagnostic.
		if status, ok := SystemAPIStatusCode(err); ok {
			return nil, &SystemAPIError{Method: http.MethodPost, Path: path, StatusCode: status}
		}
		return nil, fmt.Errorf("cleanup connection unavailable")
	}
	if result.AuthorizationID != authorizationID || result.ExecutionID != request.ExecutionID || result.TenantID != strconv.FormatUint(uint64(*c.tenantID), 10) || result.Attempt != request.Attempt || result.Scope != request.Scope || result.Purpose != execution.CleanupEngineConnectionPurpose || result.EngineID != request.EngineID || !result.ExpiresAt.After(time.Now()) || result.Engine == nil || uint64(result.Engine.ID) != uint64(engineID) || result.Engine.TenantID != *c.tenantID || result.Engine.LifecycleState != "deleting" || result.Engine.EngineType == "" || len(result.Engine.ConnectionInfo) == 0 {
		return nil, fmt.Errorf("invalid cleanup connection binding")
	}
	return &result, nil
}
