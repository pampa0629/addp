package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// SystemClient is the Bearer-only tenant client for System-owned engine facts.
// It intentionally has no Internal API Key or caller-supplied tenant header path.
type SystemClient struct {
	baseURL     string
	httpClient  *http.Client
	tokenSource ServiceTokenProvider
	tenantID    *uint
}

func NewSystemClient(baseURL string, tokenSource ServiceTokenProvider) *SystemClient {
	return &SystemClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second}, tokenSource: tokenSource,
	}
}

func (c *SystemClient) WithTenantID(tenantID uint) *SystemClient {
	if c == nil {
		return nil
	}
	return &SystemClient{baseURL: c.baseURL, httpClient: c.httpClient, tokenSource: c.tokenSource, tenantID: &tenantID}
}

func (c *SystemClient) GetEngine(engineID uint) (*models.Engine, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, errors.New("System engine request requires a tenant context")
	}
	var engine models.Engine
	if err := c.doTenantJSON(context.Background(), http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d", engineID), &engine); err != nil {
		return nil, err
	}
	return &engine, nil
}

func (c *SystemClient) GetEngineForTenant(ctx context.Context, tenantID, engineID uint) (*models.Engine, error) {
	if c == nil {
		return nil, errors.New("System client is required")
	}
	bound := c.WithTenantID(tenantID)
	var engine models.Engine
	if err := bound.doTenantJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d", engineID), &engine); err != nil {
		return nil, err
	}
	return &engine, nil
}

func (c *SystemClient) ListEngines(engineType string, tenantID uint) ([]models.Engine, error) {
	bound := c.WithTenantID(tenantID)
	values := url.Values{}
	if engineType != "" {
		values.Set("engine_type", engineType)
	}
	path := "/api/v1/system/engines"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var engines []models.Engine
	if err := bound.doTenantJSON(context.Background(), http.MethodGet, path, &engines); err != nil {
		return nil, err
	}
	return engines, nil
}

type EngineCatalogListChildrenRequest struct {
	Path    EngineCatalogPath        `json:"path"`
	Options EngineCatalogListOptions `json:"options,omitempty"`
}

type EngineCatalogListChildrenResponse struct {
	Nodes []EngineCatalogEntry `json:"nodes"`
}

type EngineCatalogDescribeFactsRequest struct {
	Path EngineCatalogPath `json:"path"`
}

type EngineCatalogPath struct {
	Version  string                 `json:"version,omitempty"`
	EngineID uint                   `json:"engine_id,omitempty"`
	Segments []EngineCatalogSegment `json:"segments"`
}

type EngineCatalogSegment struct {
	Term string `json:"term"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type EngineCatalogEntry struct {
	Name      string                      `json:"name"`
	Path      EngineCatalogPath           `json:"path"`
	Term      string                      `json:"term"`
	Kind      string                      `json:"kind"`
	Role      string                      `json:"role"`
	Table     *datatype.TableInfo         `json:"table,omitempty"`
	Storage   *plugin.CatalogStorageFacts `json:"storage,omitempty"`
	LeafCount *int                        `json:"leaf_count,omitempty"`
	UpdatedAt *time.Time                  `json:"updated_at,omitempty"`
}

type EngineCatalogListOptions struct {
	Recursive bool `json:"recursive,omitempty"`
	Limit     int  `json:"limit,omitempty"`
	Offset    int  `json:"offset,omitempty"`
}

func (c *SystemClient) ListCatalogChildren(engineID uint, req EngineCatalogListChildrenRequest) ([]EngineCatalogEntry, error) {
	return c.ListCatalogChildrenWithToken(engineID, req, "")
}

func (c *SystemClient) ListCatalogChildrenWithToken(engineID uint, req EngineCatalogListChildrenRequest, token string) ([]EngineCatalogEntry, error) {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 {
		return nil, errors.New("System catalog request requires a tenant context")
	}
	if token != "" {
		var response EngineCatalogListChildrenResponse
		status, err := c.doJSON(context.Background(), http.MethodPost, fmt.Sprintf("/api/v1/system/engines/%d/catalog/children", engineID), token, &response)
		if err != nil {
			return nil, err
		}
		_ = status
		return response.Nodes, nil
	}
	// Catalog browsing requires a request body; use the shared tenant transport directly.
	var response EngineCatalogListChildrenResponse
	if err := c.doTenantJSONWithPayload(context.Background(), http.MethodPost, fmt.Sprintf("/api/v1/system/engines/%d/catalog/children", engineID), req, &response); err != nil {
		return nil, err
	}
	return response.Nodes, nil
}

func (c *SystemClient) doTenantJSON(ctx context.Context, method, path string, result any) error {
	if c == nil || c.httpClient == nil || c.tokenSource == nil || c.tenantID == nil || *c.tenantID == 0 {
		return errors.New("System service request requires a tenant context")
	}
	token, err := c.tokenSource.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	status, err := c.doJSON(ctx, method, path, token, result)
	if status != http.StatusUnauthorized {
		return err
	}
	invalidator, ok := c.tokenSource.(ServiceTokenInvalidator)
	if !ok {
		return err
	}
	invalidator.InvalidateToken(*c.tenantID, token)
	token, err = c.tokenSource.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	_, err = c.doJSON(ctx, method, path, token, result)
	return err
}

func (c *SystemClient) doJSON(ctx context.Context, method, path, token string, result any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return response.StatusCode, fmt.Errorf("System API returned status %d: %s", response.StatusCode, strings.TrimSpace(string(payload)))
	}
	if result == nil {
		return response.StatusCode, nil
	}
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		return response.StatusCode, err
	}
	return response.StatusCode, nil
}

func (c *SystemClient) doTenantJSONWithPayload(ctx context.Context, method, path string, payload, result any) error {
	if c == nil || c.tenantID == nil || *c.tenantID == 0 || c.tokenSource == nil {
		return errors.New("System service request requires a tenant context")
	}
	token, err := c.tokenSource.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	requestBody, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, strings.NewReader(string(requestBody)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return fmt.Errorf("System API returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(result)
}

// ModuleRegistrationRequest is the shared request model for the
// Bearer-only SystemServiceClient platform registration API.
type ModuleRegistrationRequest struct {
	ModuleName              string                                     `json:"module_name"`
	InstanceID              string                                     `json:"instance_id"`
	Role                    string                                     `json:"role"`
	ModuleURL               string                                     `json:"module_url"`
	RoutePrefix             string                                     `json:"route_prefix"`
	HealthCheckURL          string                                     `json:"health_check_url,omitempty"`
	Metadata                map[string]interface{}                     `json:"metadata,omitempty"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
	TaskProvider            *models.TaskProviderDeclaration            `json:"task_provider,omitempty"`
}

const (
	ModuleRuntimeRoleBackend   = "backend"
	ModuleRuntimeRoleWorker    = "worker"
	ModuleRuntimeRoleScheduler = "scheduler"
)

type ModuleInfo struct {
	ID                      uint                                       `json:"id"`
	ModuleName              string                                     `json:"module_name"`
	RoutePrefix             string                                     `json:"route_prefix"`
	Enabled                 bool                                       `json:"enabled"`
	Version                 int64                                      `json:"version"`
	Instances               []ModuleRuntimeInstanceInfo                `json:"instances"`
	ConfigurationManagement *commonconfiguration.ManagementDeclaration `json:"configuration_management,omitempty"`
	TaskProvider            *models.TaskProviderDeclaration            `json:"task_provider,omitempty"`
	CreatedAt               time.Time                                  `json:"created_at"`
	UpdatedAt               time.Time                                  `json:"updated_at"`
}

type ModuleRuntimeInstanceInfo struct {
	ID             uint                   `json:"id"`
	InstanceID     string                 `json:"instance_id"`
	Role           string                 `json:"role"`
	ModuleURL      string                 `json:"module_url"`
	HealthCheckURL string                 `json:"health_check_url"`
	Status         string                 `json:"status"`
	LastHeartbeat  time.Time              `json:"last_heartbeat"`
	LeaseExpiresAt time.Time              `json:"lease_expires_at"`
	Metadata       map[string]interface{} `json:"metadata"`
	RegisteredAt   time.Time              `json:"registered_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

func (c *SystemClient) String() string {
	return "SystemClient(" + strconv.Itoa(int(len(c.baseURL))) + ")"
}
