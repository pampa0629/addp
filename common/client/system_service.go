package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
	"github.com/google/uuid"
)

// SystemServiceClient is the Bearer-only client used by ADDP service
// principals. Tenant and platform requests are explicit and immutable.
type SystemServiceClient struct {
	baseURL        string
	httpClient     *http.Client
	tenantTokens   ServiceTokenProvider
	platformTokens PlatformServiceTokenProvider
	tenantID       *uint
}

type SystemAPIError struct {
	Method     string
	Path       string
	StatusCode int
	ErrorCode  string
}

func (e *SystemAPIError) Error() string {
	return fmt.Sprintf("System API %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
}

func SystemAPIStatusCode(err error) (int, bool) {
	var apiError *SystemAPIError
	if errors.As(err, &apiError) {
		return apiError.StatusCode, true
	}
	return 0, false
}

func SystemAPIErrorCode(err error) (string, bool) {
	var apiError *SystemAPIError
	if errors.As(err, &apiError) && apiError.ErrorCode != "" {
		return apiError.ErrorCode, true
	}
	return "", false
}

type ServiceTokenSource interface {
	ServiceTokenProvider
	PlatformServiceTokenProvider
}

func NewSystemServiceClient(baseURL string, tokenSource ServiceTokenSource, httpClient *http.Client) *SystemServiceClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &SystemServiceClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), httpClient: httpClient,
		tenantTokens: tokenSource, platformTokens: tokenSource,
	}
}

func (c *SystemServiceClient) WithTenantID(tenantID uint) *SystemServiceClient {
	return &SystemServiceClient{
		baseURL: c.baseURL, httpClient: c.httpClient,
		tenantTokens: c.tenantTokens, platformTokens: c.platformTokens, tenantID: &tenantID,
	}
}

func (c *SystemServiceClient) TenantServiceAccessToken(ctx context.Context, tenantID uint) (string, error) {
	if c == nil || c.tenantTokens == nil || tenantID == 0 {
		return "", errors.New("tenant service access token requires a tenant context")
	}
	return c.tenantTokens.Token(ctx, tenantID)
}

func (c *SystemServiceClient) GetEngine(ctx context.Context, engineID uint) (*models.Engine, error) {
	var engine models.Engine
	err := c.doTenantJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d", engineID), nil, &engine)
	return &engine, err
}

// ListCatalogChildren lists live catalog entries using the tenant service
// access token. The System catalog endpoint is also used by service modules
// when they need to validate a user-selected resource before persisting it.
func (c *SystemServiceClient) ListCatalogChildren(ctx context.Context, engineID uint, req EngineCatalogListChildrenRequest) ([]EngineCatalogEntry, error) {
	var response EngineCatalogListChildrenResponse
	if err := c.doTenantJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/system/engines/%d/catalog/children", engineID), req, &response); err != nil {
		return nil, err
	}
	return response.Nodes, nil
}

// DescribeCatalogFacts reads the live structural facts for one catalog leaf.
func (c *SystemServiceClient) DescribeCatalogFacts(ctx context.Context, engineID uint, req EngineCatalogDescribeFactsRequest) (*plugin.CatalogFacts, error) {
	var facts plugin.CatalogFacts
	if err := c.doTenantJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/system/engines/%d/catalog/facts", engineID), req, &facts); err != nil {
		return nil, err
	}
	return &facts, nil
}

func (c *SystemServiceClient) ListEngines(ctx context.Context) ([]models.Engine, error) {
	var engines []models.Engine
	err := c.doTenantJSON(ctx, http.MethodGet, "/api/v1/system/engines", nil, &engines)
	return engines, err
}

func (c *SystemServiceClient) GetEngineRuntimeDescriptor(
	ctx context.Context,
	engineID uint,
) (*models.EngineRuntimeDescriptor, error) {
	var descriptor models.EngineRuntimeDescriptor
	err := c.doTenantJSON(
		ctx,
		http.MethodGet,
		fmt.Sprintf("/api/v1/system/runtime/engine-descriptors/%d", engineID),
		nil,
		&descriptor,
	)
	return &descriptor, err
}

func (c *SystemServiceClient) ListEngineRuntimeDescriptors(
	ctx context.Context,
) ([]models.EngineRuntimeDescriptor, error) {
	const pageSize = 100
	descriptors := make([]models.EngineRuntimeDescriptor, 0, pageSize)
	for page := 1; ; page++ {
		var response struct {
			Data     []models.EngineRuntimeDescriptor `json:"data"`
			Total    int64                            `json:"total"`
			Page     int                              `json:"page"`
			PageSize int                              `json:"page_size"`
		}
		path := fmt.Sprintf("/api/v1/system/runtime/engine-descriptors?page=%d&page_size=%d", page, pageSize)
		if err := c.doTenantJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		if response.Total < 0 || response.Page != page || response.PageSize != pageSize || len(response.Data) > pageSize {
			return nil, errors.New("System engine descriptor list returned an invalid pagination response")
		}
		descriptors = append(descriptors, response.Data...)
		if int64(len(descriptors)) >= response.Total {
			if int64(len(descriptors)) != response.Total {
				return nil, errors.New("System engine descriptor pagination exceeded the declared total")
			}
			return descriptors, nil
		}
		if len(response.Data) == 0 {
			return nil, errors.New("System engine descriptor pagination ended before the declared total")
		}
	}
}

func (c *SystemServiceClient) AppendTenantAuditEvent(ctx context.Context, request *models.AuditLogCreateRequest) error {
	return c.doTenantJSON(ctx, http.MethodPost, "/api/v1/system/tenant/audit/events", request, nil)
}

func (c *SystemServiceClient) RegisterModule(ctx context.Context, request *ModuleRegistrationRequest) error {
	return c.doPlatformJSON(ctx, http.MethodPost, "/api/v1/system/runtime/modules", request, nil)
}

// RegisterRuntimeEngine registers or updates one platform runtime Engine Instance.
// Runtime processes call this only after their own HTTP server is ready to accept probes.
func (c *SystemServiceClient) RegisterRuntimeEngine(ctx context.Context, request *models.CapabilityRegistrationRequest) error {
	if request == nil {
		return errors.New("runtime engine registration request is required")
	}
	return c.doPlatformJSON(ctx, http.MethodPost, "/api/v1/system/runtime/engines", request, nil)
}

// RegisterRuntimeEngineWithRetry starts a non-blocking registration loop and stops after
// the first successful idempotent registration or when ctx is cancelled.
func (c *SystemServiceClient) RegisterRuntimeEngineWithRetry(
	ctx context.Context,
	request *models.CapabilityRegistrationRequest,
	initialRetryInterval time.Duration,
	maxRetryInterval time.Duration,
) {
	go func() {
		if request == nil {
			return
		}
		if initialRetryInterval <= 0 {
			initialRetryInterval = time.Second
		}
		if maxRetryInterval < initialRetryInterval {
			maxRetryInterval = initialRetryInterval
		}
		retryInterval := initialRetryInterval
		for {
			if err := c.RegisterRuntimeEngine(ctx, request); err == nil {
				log.Printf("%s runtime engine registration succeeded", request.EngineType)
				return
			} else {
				log.Printf("%s runtime engine registration failed: %v", request.EngineType, err)
			}

			timer := time.NewTimer(retryInterval)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			case <-timer.C:
			}
			if retryInterval < maxRetryInterval {
				retryInterval *= 2
				if retryInterval > maxRetryInterval {
					retryInterval = maxRetryInterval
				}
			}
		}
	}()
}

func (c *SystemServiceClient) SendModuleHeartbeat(ctx context.Context, moduleName, instanceID string) error {
	return c.doPlatformJSON(ctx, http.MethodPost, "/api/v1/system/runtime/modules/heartbeat", map[string]string{
		"module_name": moduleName, "instance_id": instanceID,
	}, nil)
}

func (c *SystemServiceClient) RegisterTaskProvider(ctx context.Context, provider *models.TaskProvider) error {
	return c.doPlatformJSON(ctx, http.MethodPost, "/api/v1/system/runtime/task-providers", provider, nil)
}

func (c *SystemServiceClient) ListTaskProviders(ctx context.Context) ([]*models.TaskProvider, error) {
	var providers []*models.TaskProvider
	if err := c.doPlatformJSON(ctx, http.MethodGet, "/api/v1/system/runtime/task-providers", nil, &providers); err != nil {
		return nil, err
	}
	return providers, nil
}

func (c *SystemServiceClient) GetTaskProvider(ctx context.Context, moduleName string) (*models.TaskProvider, error) {
	moduleName = strings.TrimSpace(moduleName)
	if moduleName == "" || url.PathEscape(moduleName) != moduleName {
		return nil, errors.New("task provider module name is invalid")
	}
	var provider models.TaskProvider
	path := "/api/v1/system/runtime/task-providers/" + moduleName
	if err := c.doPlatformJSON(ctx, http.MethodGet, path, nil, &provider); err != nil {
		return nil, err
	}
	return &provider, nil
}

func (c *SystemServiceClient) ListModules(ctx context.Context) ([]*ModuleInfo, error) {
	return c.listModules(ctx, "/api/v1/system/runtime/modules")
}

func (c *SystemServiceClient) ListActiveModules(ctx context.Context) ([]*ModuleInfo, error) {
	return c.listModules(ctx, "/api/v1/system/runtime/modules?status=up")
}

func (c *SystemServiceClient) listModules(ctx context.Context, path string) ([]*ModuleInfo, error) {
	var response struct {
		Modules []*ModuleInfo `json:"modules"`
	}
	if err := c.doPlatformJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Modules, nil
}

type APIKeyValidationResponse struct {
	Valid              bool       `json:"valid"`
	AppID              uint       `json:"app_id"`
	AppName            string     `json:"app_name"`
	AllowedServices    []string   `json:"allowed_services"`
	RateLimitPerMinute int        `json:"rate_limit_per_minute"`
	ExpiresAt          *time.Time `json:"expires_at"`
}

func (c *SystemServiceClient) ValidateAPIKey(ctx context.Context, keyHash string) (*APIKeyValidationResponse, error) {
	if strings.TrimSpace(keyHash) == "" {
		return nil, errors.New("API key hash is required")
	}
	var response APIKeyValidationResponse
	path := "/api/v1/system/runtime/api-keys/validate?key_hash=" + url.QueryEscape(keyHash)
	if err := c.doPlatformJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *SystemServiceClient) RegisterAndHeartbeatWithMetadata(
	ctx context.Context,
	moduleName, moduleURL, routePrefix string,
	metadata map[string]interface{},
) {
	c.RegisterAndHeartbeat(ctx, &ModuleRegistrationRequest{
		ModuleName: moduleName, ModuleURL: moduleURL, RoutePrefix: routePrefix,
		HealthCheckURL: moduleURL + "/health", Metadata: metadata,
	})
}

func (c *SystemServiceClient) RegisterAndHeartbeat(ctx context.Context, request *ModuleRegistrationRequest) {
	go func() {
		if request == nil {
			return
		}
		registration := *request
		if strings.TrimSpace(registration.InstanceID) == "" {
			registration.InstanceID = uuid.NewString()
		}
		if strings.TrimSpace(registration.Role) == "" {
			registration.Role = ModuleRuntimeRoleBackend
		}
		registration.Metadata = map[string]interface{}{"module": registration.ModuleName}
		for key, value := range request.Metadata {
			registration.Metadata[key] = value
		}
		register := func() bool {
			if err := c.RegisterModule(ctx, &registration); err != nil {
				log.Printf("%s module registration failed: %v", registration.ModuleName, err)
				return false
			}
			return true
		}

		registered := false
		for attempt := 1; attempt <= 3 && !registered; attempt++ {
			registered = register()
			if !registered {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(attempt*5) * time.Second):
				}
			}
		}

		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		consecutiveFailures := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.SendModuleHeartbeat(ctx, registration.ModuleName, registration.InstanceID); err != nil {
					consecutiveFailures++
					log.Printf("%s module heartbeat failed: %v", registration.ModuleName, err)
				} else {
					registered = true
					consecutiveFailures = 0
				}
				if consecutiveFailures >= 3 {
					registered = register()
					consecutiveFailures = 0
				}
			}
		}
	}()
}

func (c *SystemServiceClient) doTenantJSON(ctx context.Context, method, path string, payload, result any) error {
	if c == nil || c.tenantTokens == nil || c.tenantID == nil || *c.tenantID == 0 {
		return errors.New("System service request requires a tenant context")
	}
	token, err := c.tenantTokens.Token(ctx, *c.tenantID)
	if err != nil {
		return err
	}
	status, err := c.doJSON(ctx, method, path, token, payload, result)
	if status != http.StatusUnauthorized {
		return err
	}
	invalidator, ok := c.tenantTokens.(ServiceTokenInvalidator)
	if !ok {
		return err
	}
	invalidator.InvalidateToken(*c.tenantID, token)
	token, tokenErr := c.tenantTokens.Token(ctx, *c.tenantID)
	if tokenErr != nil {
		return tokenErr
	}
	_, err = c.doJSON(ctx, method, path, token, payload, result)
	return err
}

func (c *SystemServiceClient) doPlatformJSON(ctx context.Context, method, path string, payload, result any) error {
	if c == nil || c.platformTokens == nil || c.tenantID != nil {
		return errors.New("System service request requires a platform context")
	}
	token, err := c.platformTokens.PlatformToken(ctx)
	if err != nil {
		return err
	}
	status, err := c.doJSON(ctx, method, path, token, payload, result)
	if status != http.StatusUnauthorized {
		return err
	}
	invalidator, ok := c.platformTokens.(PlatformServiceTokenInvalidator)
	if !ok {
		return err
	}
	invalidator.InvalidatePlatformToken(token)
	token, tokenErr := c.platformTokens.PlatformToken(ctx)
	if tokenErr != nil {
		return tokenErr
	}
	_, err = c.doJSON(ctx, method, path, token, payload, result)
	return err
}

func (c *SystemServiceClient) doJSON(ctx context.Context, method, path, token string, payload, result any) (int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("encode System request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, fmt.Errorf("create System request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("send System request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var errorResponse struct {
			ErrorCode string `json:"error_code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&errorResponse)
		return response.StatusCode, &SystemAPIError{
			Method: method, Path: pathWithoutQuery(path), StatusCode: response.StatusCode,
			ErrorCode: errorResponse.ErrorCode,
		}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(result); err != nil {
		return response.StatusCode, fmt.Errorf("decode System response: %w", err)
	}
	return response.StatusCode, nil
}

func pathWithoutQuery(path string) string {
	if parsed, err := url.Parse(path); err == nil {
		return parsed.Path
	}
	return path
}
