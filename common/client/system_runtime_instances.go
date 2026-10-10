package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/models"
)

// RuntimeInstanceReference is a current User-authorized operational reference.
// Private endpoints never enter this response or a browser-facing DTO.
type RuntimeInstanceReference struct {
	ID                     uint       `json:"id"`
	ModuleName             string     `json:"module_name"`
	InstanceID             string     `json:"instance_id"`
	Role                   string     `json:"role"`
	NodeID                 string     `json:"node_id"`
	Status                 string     `json:"status"`
	LeaseExpiresAt         time.Time  `json:"lease_expires_at"`
	ProcessStartedAt       *time.Time `json:"process_started_at"`
	ProcessMetricsDeclared *bool      `json:"process_metrics_declared"`
}

// GetRuntimeInstancesForUser performs one bounded exact-record read using only
// this request's human credential. It neither caches nor retries as a machine.
func (c *SystemServiceClient) GetRuntimeInstancesForUser(ctx context.Context, ids []uint, userToken string) ([]RuntimeInstanceReference, error) {
	invalid := errors.New("runtime instance reference requires current User and canonical IDs")
	if c == nil || c.baseURL == "" || c.httpClient == nil || len(ids) == 0 || len(ids) > 100 || !strings.HasPrefix(userToken, "addp_at_") || len(userToken) <= len("addp_at_") || strings.ContainsAny(userToken, " \t\r\n") {
		return nil, invalid
	}
	parts := make([]string, len(ids))
	wanted := map[uint]bool{}
	for i, id := range ids {
		if id == 0 || wanted[id] {
			return nil, invalid
		}
		wanted[id] = true
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	if _, err := models.ParseRuntimeInstanceIDs(strings.Join(parts, ",")); err != nil {
		return nil, invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result struct {
		Data     *[]RuntimeInstanceReference `json:"data"`
		Total    *int                        `json:"total"`
		Page     int                         `json:"page"`
		PageSize int                         `json:"page_size"`
	}
	params := url.Values{"ids": {strings.Join(parts, ",")}, "page": {"1"}, "page_size": {"100"}}
	status, err := c.doJSON(ctx, http.MethodGet, "/api/v1/system/platform/module-instances?"+params.Encode(), userToken, nil, &result, 4<<20)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || result.Data == nil || result.Total == nil || *result.Total != len(ids) || result.Page != 1 || result.PageSize != 100 || len(*result.Data) != len(ids) {
		return nil, errors.New("System returned incomplete runtime instance references")
	}
	validName := func(s string, max int) bool {
		return s != "" && strings.TrimSpace(s) == s && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && strings.IndexFunc(s, unicode.IsControl) < 0
	}
	seen := map[uint]bool{}
	for _, instance := range *result.Data {
		if !wanted[instance.ID] || seen[instance.ID] || !validName(instance.ModuleName, 50) || !validName(instance.InstanceID, 100) || instance.ProcessMetricsDeclared == nil || instance.LeaseExpiresAt.IsZero() {
			return nil, errors.New("System returned invalid runtime instance references")
		}
		switch instance.Role {
		case "backend", "worker", "scheduler", "ingress":
		default:
			return nil, errors.New("System returned invalid runtime role")
		}
		if instance.Status != "up" && instance.Status != "down" {
			return nil, errors.New("System returned invalid runtime status")
		}
		seen[instance.ID] = true
	}
	return *result.Data, nil
}
