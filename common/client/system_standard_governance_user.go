package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type StandardGovernanceUser struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Code          string `json:"code,omitempty"`
	Found         bool   `json:"found"`
	Referenceable bool   `json:"referenceable"`
}

type StandardGovernanceUserList struct {
	Data       []StandardGovernanceUser `json:"data"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int                      `json:"total_pages"`
}

type standardGovernanceUserWire struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Code          string `json:"code,omitempty"`
	Found         bool   `json:"found,omitempty"`
	Referenceable bool   `json:"referenceable,omitempty"`
}

func (c *SystemServiceClient) ResolveStandardGovernanceUsers(ctx context.Context, ids []int64) ([]StandardGovernanceUser, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, errors.New("System resolve Standard governance users requires 1 to 200 IDs")
	}
	references := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, errors.New("System resolve Standard governance users contains an invalid ID")
		}
		references = append(references, map[string]string{"subject_type": "user", "id": strconv.FormatInt(id, 10)})
	}
	var response struct {
		Results []standardGovernanceUserWire `json:"results"`
	}
	if err := c.doTenantJSON(ctx, http.MethodPost, "/api/v1/system/runtime/standard-governance-users/resolve", map[string]any{"references": references}, &response); err != nil {
		return nil, fmt.Errorf("System resolve Standard governance users: %w", err)
	}
	if len(response.Results) != len(ids) {
		return nil, errors.New("System resolve Standard governance users returned a result count mismatch")
	}
	result := make([]StandardGovernanceUser, 0, len(ids))
	for index, item := range response.Results {
		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil || id != ids[index] {
			return nil, errors.New("System resolve Standard governance users returned an invalid result")
		}
		result = append(result, StandardGovernanceUser{ID: id, Name: item.Name, Code: item.Code, Found: item.Found, Referenceable: item.Referenceable})
	}
	return result, nil
}

func (c *SystemServiceClient) ListStandardGovernanceUsers(ctx context.Context, search string, page, pageSize int) (*StandardGovernanceUserList, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || page < 1 || pageSize < 1 || pageSize > 50 || len([]rune(strings.TrimSpace(search))) > 100 {
		return nil, errors.New("System list Standard governance users contains invalid parameters")
	}
	query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(pageSize)}}
	if search = strings.TrimSpace(search); search != "" {
		query.Set("search", search)
	}
	var wire struct {
		Data       []struct{ ID, Name, Code, Status string } `json:"data"`
		Total      int64                                     `json:"total"`
		Page       int                                       `json:"page"`
		PageSize   int                                       `json:"page_size"`
		TotalPages int                                       `json:"total_pages"`
	}
	if err := c.doTenantJSON(ctx, http.MethodGet, "/api/v1/system/runtime/standard-governance-users/candidates?"+query.Encode(), nil, &wire); err != nil {
		return nil, fmt.Errorf("System list Standard governance users: %w", err)
	}
	result := &StandardGovernanceUserList{Total: wire.Total, Page: wire.Page, PageSize: wire.PageSize, TotalPages: wire.TotalPages, Data: make([]StandardGovernanceUser, 0, len(wire.Data))}
	for _, item := range wire.Data {
		id, err := strconv.ParseInt(item.ID, 10, 64)
		if err != nil || id <= 0 || strings.TrimSpace(item.Name) == "" || item.Status != "active" {
			return nil, errors.New("System list Standard governance users returned an invalid candidate")
		}
		result.Data = append(result.Data, StandardGovernanceUser{ID: id, Name: item.Name, Code: item.Code, Found: true, Referenceable: true})
	}
	return result, nil
}
