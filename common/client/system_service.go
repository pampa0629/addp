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

	"github.com/addp/common/models"
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

func (c *SystemServiceClient) GetEngine(ctx context.Context, engineID uint) (*models.Engine, error) {
	var engine models.Engine
	err := c.doTenantJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/system/engines/%d", engineID), nil, &engine)
	return &engine, err
}

func (c *SystemServiceClient) ListEngines(ctx context.Context) ([]models.Engine, error) {
	const pageSize = 100
	engines := make([]models.Engine, 0, pageSize)
	for page := 1; ; page++ {
		var response struct {
			Data     []models.Engine `json:"data"`
			Total    int64           `json:"total"`
			Page     int             `json:"page"`
			PageSize int             `json:"page_size"`
		}
		path := fmt.Sprintf("/api/v1/system/engines?page=%d&page_size=%d", page, pageSize)
		if err := c.doTenantJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		if response.Total < 0 || response.Page != page || response.PageSize != pageSize || len(response.Data) > pageSize {
			return nil, errors.New("System engine list returned an invalid pagination response")
		}
		engines = append(engines, response.Data...)
		if int64(len(engines)) >= response.Total {
			if int64(len(engines)) != response.Total {
				return nil, errors.New("System engine list pagination exceeded the declared total")
			}
			return engines, nil
		}
		if len(response.Data) == 0 {
			return nil, errors.New("System engine list pagination ended before the declared total")
		}
	}
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

func (c *SystemServiceClient) SendModuleHeartbeat(ctx context.Context, moduleName string) error {
	return c.doPlatformJSON(ctx, http.MethodPost, "/api/v1/system/runtime/modules/heartbeat", map[string]string{"module_name": moduleName}, nil)
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

func (c *SystemServiceClient) RegisterAndHeartbeatWithMetadata(
	ctx context.Context,
	moduleName, moduleURL, routePrefix string,
	metadata map[string]interface{},
) {
	go func() {
		registrationMetadata := map[string]interface{}{"module": moduleName}
		for key, value := range metadata {
			registrationMetadata[key] = value
		}
		request := &ModuleRegistrationRequest{
			ModuleName: moduleName, ModuleURL: moduleURL, RoutePrefix: routePrefix,
			HealthCheckURL: moduleURL + "/health", Metadata: registrationMetadata,
		}
		register := func() bool {
			if err := c.RegisterModule(ctx, request); err != nil {
				log.Printf("%s module registration failed: %v", moduleName, err)
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
				if err := c.SendModuleHeartbeat(ctx, moduleName); err != nil {
					consecutiveFailures++
					log.Printf("%s module heartbeat failed: %v", moduleName, err)
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
		return response.StatusCode, &SystemAPIError{
			Method: method, Path: pathWithoutQuery(path), StatusCode: response.StatusCode,
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
