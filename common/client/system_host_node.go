package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// HostNodeReference contains only the facts required by a referencing owner.
// Authorization remains with System; this is never a service-token request.
type HostNodeReference struct {
	NodeID  string `json:"node_id"`
	Version int64  `json:"version"`
	Enabled bool   `json:"enabled"`
}

func (c *SystemServiceClient) GetHostNodeForUser(ctx context.Context, nodeID, userToken string) (*HostNodeReference, error) {
	id, err := uuid.Parse(nodeID)
	if c == nil || c.baseURL == "" || c.httpClient == nil || err != nil || id == uuid.Nil || id.String() != nodeID ||
		!strings.HasPrefix(userToken, "addp_at_") || len(userToken) <= len("addp_at_") || strings.ContainsAny(userToken, " \t\r\n") {
		return nil, errors.New("node reference requires current User Access Token")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result struct {
		NodeID  string `json:"node_id"`
		Version int64  `json:"version"`
		Enabled *bool  `json:"enabled"`
	}
	status, err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/platform/host_nodes/"+nodeID, userToken, nil, &result, 64<<10)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || result.NodeID != nodeID || result.Version < 1 || result.Enabled == nil {
		return nil, errors.New("node reference response incomplete or mismatched")
	}
	return &HostNodeReference{NodeID: result.NodeID, Version: result.Version, Enabled: *result.Enabled}, nil
}

// AuthorizeHostNodesForUser verifies the current owner's list permission, including
// the empty-target case. It does not enumerate or cache a node inventory.
func (c *SystemServiceClient) AuthorizeHostNodesForUser(ctx context.Context, userToken string) error {
	if c == nil || c.baseURL == "" || c.httpClient == nil || !strings.HasPrefix(userToken, "addp_at_") || len(userToken) <= len("addp_at_") || strings.ContainsAny(userToken, " \t\r\n") {
		return errors.New("node list requires current User Access Token")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result struct {
		Data     *[]HostNodeReference `json:"data"`
		Total    *int                 `json:"total"`
		Page     int                  `json:"page"`
		PageSize int                  `json:"page_size"`
	}
	status, err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/platform/host_nodes?page=1&page_size=1", userToken, nil, &result, 64<<10)
	if err != nil {
		return err
	}
	if status != http.StatusOK || result.Data == nil || result.Total == nil || *result.Total < 0 || result.Page != 1 || result.PageSize != 1 || len(*result.Data) > 1 {
		return errors.New("node list authorization response incomplete")
	}
	return nil
}
