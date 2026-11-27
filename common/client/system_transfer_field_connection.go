package client

import (
	"context"
	"net/http"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// TransferFieldConnectionRequest carries the original user credential only to the
// fixed System route. Never log this request or cache the returned connection.
type TransferFieldConnectionRequest struct {
	UserCredential string                   `json:"user_credential"`
	Target         plugin.EngineCatalogPath `json:"target"`
	EngineVersion  int64                    `json:"engine_version"`
}

type TransferFieldConnection struct {
	ConnectionInfo models.ConnectionInfo `json:"connection_info"`
}

func (c *SystemServiceClient) OpenTransferFieldConnection(ctx context.Context, tenantID uint, request TransferFieldConnectionRequest) (*TransferFieldConnection, error) {
	token, err := c.TenantServiceAccessToken(ctx, tenantID)
	if err != nil {
		return nil, ErrManagerPreviewReadUnavailable
	}
	bound := *c
	transport := *c.httpClient
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	bound.httpClient = &transport
	var result TransferFieldConnection
	status, err := bound.doJSON(ctx, http.MethodPost, "/api/v1/system/engine-access/connections/transfer-field-recommendation", token, request, &result)
	switch status {
	case http.StatusUnauthorized:
		return nil, ErrManagerPreviewCredentialRejected
	case http.StatusForbidden:
		return nil, ErrManagerPreviewReadDenied
	}
	if err != nil || status != http.StatusOK || len(result.ConnectionInfo) == 0 {
		return nil, ErrManagerPreviewReadUnavailable
	}
	return &result, nil
}
