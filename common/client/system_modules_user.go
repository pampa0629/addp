package client

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type ModuleReference struct {
	ModuleName string `json:"module_name"`
	Version    int64  `json:"version"`
	Enabled    *bool  `json:"enabled"`
}

var userModuleName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,49}$`)

// GetModulesForUser reads the complete bounded platform module projection using
// only the current human credential; it never retries with machine authority.
func (c *SystemServiceClient) GetModulesForUser(ctx context.Context, token string) ([]ModuleReference, error) {
	invalid := errors.New("invalid current User module projection")
	if c == nil || c.baseURL == "" || c.httpClient == nil || !strings.HasPrefix(token, "addp_at_") || len(token) <= 8 || strings.ContainsAny(token, " \t\r\n") {
		return nil, invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result struct {
		Modules *[]ModuleReference `json:"modules"`
		Count   *int               `json:"count"`
	}
	status, err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/platform/modules", token, nil, &result, 4<<20)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || result.Modules == nil || result.Count == nil || *result.Count != len(*result.Modules) || len(*result.Modules) > 1000 {
		return nil, invalid
	}
	seen := map[string]bool{}
	for _, r := range *result.Modules {
		if !userModuleName.MatchString(r.ModuleName) || r.Enabled == nil || r.Version < 1 || seen[r.ModuleName] {
			return nil, invalid
		}
		seen[r.ModuleName] = true
	}
	return *result.Modules, nil
}
