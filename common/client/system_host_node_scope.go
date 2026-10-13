package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

var ErrHostNodeScopeBudget = errors.New("complete user node scope exceeds bounded inventory")

// UserHostNode is the safe inventory projection consumed by resource aggregation.
// It never carries addresses, binding allowlists or collection endpoints.
type UserHostNode struct {
	NodeID      string `json:"node_id"`
	DisplayName string `json:"display_name"`
	NodeKind    string `json:"node_kind"`
	Version     int64  `json:"version"`
	Enabled     bool   `json:"enabled"`
}

// HostNodesForUser reads the complete bounded inventory from the owner's existing
// repeatable-read list. A partial first page is an error, never a smaller scope.
func (c *SystemServiceClient) HostNodesForUser(ctx context.Context, token string) ([]UserHostNode, error) {
	if c == nil || c.baseURL == "" || c.httpClient == nil || !strings.HasPrefix(token, "addp_at_") || len(token) <= len("addp_at_") || strings.ContainsAny(token, " \t\r\n") {
		return nil, errors.New("node scope requires current User Access Token")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var page struct {
		Data *[]struct {
			NodeID      string `json:"node_id"`
			DisplayName string `json:"display_name"`
			NodeKind    string `json:"node_kind"`
			Version     int64  `json:"version"`
			Enabled     *bool  `json:"enabled"`
		} `json:"data"`
		Total      *int `json:"total"`
		Page       int  `json:"page"`
		PageSize   int  `json:"page_size"`
		TotalPages *int `json:"total_pages"`
	}
	status, err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/platform/host_nodes?page=1&page_size=100", token, nil, &page, 4<<20)
	if err != nil {
		return nil, err
	}
	invalid := func() ([]UserHostNode, error) {
		return nil, errors.New("complete node scope response incomplete or mismatched")
	}
	if status != 200 || page.Data == nil || page.Total == nil || *page.Total < 0 || page.TotalPages == nil || page.Page != 1 || page.PageSize != 100 || *page.TotalPages != (*page.Total+99)/100 || len(*page.Data) != min(*page.Total, 100) {
		return invalid()
	}
	seen := map[string]bool{}
	nodes := make([]UserHostNode, 0, len(*page.Data))
	for _, row := range *page.Data {
		id, e := uuid.Parse(row.NodeID)
		if e != nil || id == uuid.Nil || id.String() != row.NodeID || seen[row.NodeID] || row.Version < 1 || row.Enabled == nil || strings.TrimSpace(row.DisplayName) == "" || utf8.RuneCountInString(row.DisplayName) > 255 || strings.IndexFunc(row.DisplayName, unicode.IsControl) >= 0 || (row.NodeKind != "physical" && row.NodeKind != "virtual") {
			return invalid()
		}
		seen[row.NodeID] = true
		nodes = append(nodes, UserHostNode{NodeID: row.NodeID, DisplayName: row.DisplayName, NodeKind: row.NodeKind, Version: row.Version, Enabled: *row.Enabled})
	}
	if *page.Total > 100 {
		return nil, ErrHostNodeScopeBudget
	}
	return nodes, nil
}
