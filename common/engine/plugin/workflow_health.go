package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WorkflowHealthResponse describes whole-runtime readiness, not registration or
// the connectivity/permissions of a particular workflow's input resources.
type WorkflowHealthResponse struct {
	Status          string                 `json:"status"`
	Service         string                 `json:"service"`
	Version         string                 `json:"version"`
	Uptime          *int64                 `json:"uptime,omitempty"`
	OperatorsCount  *int64                 `json:"operators_count,omitempty"`
	Dependencies    json.RawMessage        `json:"dependencies,omitempty"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	RasterResources map[string]interface{} `json:"raster_resources,omitempty"`
}

type workflowHealthDependency struct {
	Available *bool           `json:"available"`
	Required  *bool           `json:"required"`
	Version   json.RawMessage `json:"version,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
}

// HTTPWorkflowHealth is the sole addp.workflow/v1 health consumer. A successful
// HTTP exchange alone is insufficient: invalid or contradictory bodies fail.
func HTTPWorkflowHealth(ctx context.Context, connInfo ConnectionInfo) (*WorkflowHealthResponse, error) {
	baseURL, err := RuntimeBaseURL(connInfo)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("workflow runtime health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, fmt.Errorf("workflow runtime health check failed with status %d", resp.StatusCode)
	}
	const maxHealthBytes = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHealthBytes+1))
	if err != nil {
		return nil, fmt.Errorf("workflow runtime health response could not be read")
	}
	if len(body) > maxHealthBytes {
		return nil, fmt.Errorf("workflow runtime health response exceeds size limit")
	}
	var health WorkflowHealthResponse
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("workflow runtime health response is invalid JSON")
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	for _, name := range []string{"uptime", "operators_count", "error_code", "raster_resources"} {
		if bytes.Equal(fields[name], []byte("null")) {
			return nil, fmt.Errorf("workflow runtime health response contains a null field")
		}
	}
	if strings.TrimSpace(health.Service) == "" || strings.TrimSpace(health.Version) == "" {
		return nil, fmt.Errorf("workflow runtime health response requires service and version")
	}
	if health.Uptime != nil && *health.Uptime < 0 || health.OperatorsCount != nil && *health.OperatorsCount < 0 {
		return nil, fmt.Errorf("workflow runtime health response contains a negative count")
	}
	if health.Status != "healthy" && health.Status != "unhealthy" {
		return nil, fmt.Errorf("workflow runtime health response contains an invalid status")
	}
	if len(fields["error_code"]) > 0 && health.ErrorCode != "health_check_failed" {
		return nil, fmt.Errorf("workflow runtime health response contains an invalid error_code")
	}
	if health.Status == "healthy" && (resp.StatusCode != http.StatusOK || health.ErrorCode != "") ||
		health.Status == "unhealthy" && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, fmt.Errorf("workflow runtime health response contradicts HTTP status or error_code")
	}
	if len(health.Dependencies) > 0 {
		var dependencies map[string]workflowHealthDependency
		decoder := json.NewDecoder(bytes.NewReader(health.Dependencies))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&dependencies); err != nil || dependencies == nil {
			return nil, fmt.Errorf("workflow runtime health dependencies must contain structured facts")
		}
		for name, dependency := range dependencies {
			if strings.TrimSpace(name) == "" || dependency.Available == nil || dependency.Required == nil {
				return nil, fmt.Errorf("workflow runtime health dependency requires name, available and required")
			}
			for _, value := range []json.RawMessage{dependency.Version, dependency.Details} {
				if len(value) == 0 {
					continue
				}
				var text string
				if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &text) != nil {
					return nil, fmt.Errorf("workflow runtime health dependency version and details must be strings")
				}
			}
			if health.Status == "healthy" && *dependency.Required && !*dependency.Available {
				return nil, fmt.Errorf("workflow runtime required dependency is unavailable")
			}
		}
	}
	if health.Status == "unhealthy" {
		return nil, fmt.Errorf("workflow runtime is unhealthy")
	}
	return &health, nil
}
