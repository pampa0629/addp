package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
)

var ErrTaskProviderNotFound = errors.New("task provider not found")

type taskProviderLister interface {
	ListTaskProviders() ([]*models.TaskProvider, error)
}

// HealthCheckService 健康检查服务
type HealthCheckService struct {
	systemClient taskProviderLister
	httpClient   *http.Client
}

// NewHealthCheckService 创建健康检查服务
func NewHealthCheckService(systemClient taskProviderLister) *HealthCheckService {
	return &HealthCheckService{
		systemClient: systemClient, httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// HealthStatus 健康状态
type HealthStatus struct {
	Module  string `json:"module"`
	Status  string `json:"status"`  // "up", "down", "unknown"
	Latency int64  `json:"latency"` // 毫秒
	Message string `json:"message,omitempty"`
}

// ProviderHealthStatus 是 Monitor 对 TaskProvider 就绪状态的即时观测结果。
type ProviderHealthStatus struct {
	Module       string                         `json:"module"`
	DisplayName  string                         `json:"display_name"`
	Available    bool                           `json:"available"`
	Status       string                         `json:"status"`
	Message      string                         `json:"message,omitempty"`
	Capabilities *ProviderCapabilitiesStatus    `json:"capabilities"`
	Backends     []*ProviderBackendHealthStatus `json:"backends"`
	CheckedAt    time.Time                      `json:"checked_at"`
}

// ProviderBackendHealthStatus 是一个 TaskProvider Backend 实例的即时探活结果。
type ProviderBackendHealthStatus struct {
	InstanceID     string        `json:"instance_id"`
	BaseURL        string        `json:"base_url"`
	LeaseExpiresAt time.Time     `json:"lease_expires_at"`
	Status         string        `json:"status"`
	Message        string        `json:"message,omitempty"`
	ModuleHealth   *HealthStatus `json:"module_health"`
}

type ProviderCapabilitiesStatus struct {
	Status           string                          `json:"status"`
	SchemaVersion    string                          `json:"schema_version,omitempty"`
	Message          string                          `json:"message,omitempty"`
	TaskCapabilities []*ProviderTaskCapabilityStatus `json:"task_capabilities"`
}

type ProviderTaskCapabilityStatus struct {
	Type       string `json:"type"`
	Deprecated bool   `json:"deprecated"`
}

// ListTaskProviders 获取 System 中已启用的 TaskProvider。
func (s *HealthCheckService) ListTaskProviders(ctx context.Context) ([]*models.TaskProvider, error) {
	providers, err := s.systemClient.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get task providers: %w", err)
	}
	return providers, nil
}

// CheckProviderHealth 检查单个 TaskProvider 的运行态健康状态。
func (s *HealthCheckService) CheckProviderHealth(ctx context.Context, moduleName string) (*ProviderHealthStatus, error) {
	providers, err := s.systemClient.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get task providers: %w", err)
	}
	for _, provider := range providers {
		if provider.ModuleName == moduleName {
			return s.checkProviderHealth(ctx, provider), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrTaskProviderNotFound, moduleName)
}

// CheckAllProviderHealth 检查所有启用 TaskProvider 的运行态健康状态。
func (s *HealthCheckService) CheckAllProviderHealth(ctx context.Context) ([]*ProviderHealthStatus, error) {
	providers, err := s.systemClient.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get task providers: %w", err)
	}

	statuses := make([]*ProviderHealthStatus, 0, len(providers))
	for _, provider := range providers {
		statuses = append(statuses, s.checkProviderHealth(ctx, provider))
	}
	return statuses, nil
}

func (s *HealthCheckService) checkProviderHealth(ctx context.Context, provider *models.TaskProvider) *ProviderHealthStatus {
	result := &ProviderHealthStatus{
		Module:      provider.ModuleName,
		DisplayName: provider.DisplayName,
		Available:   provider.Available,
		Status:      "unknown",
		CheckedAt:   time.Now(),
	}

	result.Capabilities = parseProviderCapabilities(provider.Capabilities)
	if !provider.Available || len(provider.Backends) == 0 {
		result.Status = "down"
		result.Message = providerUnavailableMessage(provider.UnavailableReason)
		return result
	}

	result.Backends = make([]*ProviderBackendHealthStatus, 0, len(provider.Backends))
	for _, backend := range provider.Backends {
		result.Backends = append(result.Backends, s.checkProviderBackendHealth(ctx, provider.ModuleName, backend, result.Capabilities))
	}
	result.Status, result.Message = summarizeProviderHealth(result)
	return result
}

func (s *HealthCheckService) checkProviderBackendHealth(
	ctx context.Context,
	moduleName string,
	backend models.TaskProviderBackend,
	capabilities *ProviderCapabilitiesStatus,
) *ProviderBackendHealthStatus {
	result := &ProviderBackendHealthStatus{
		InstanceID:     backend.InstanceID,
		BaseURL:        backend.BaseURL,
		LeaseExpiresAt: backend.LeaseExpiresAt,
		Status:         "unknown",
	}

	if strings.TrimSpace(backend.BaseURL) == "" {
		result.Status = "down"
		result.Message = "Backend base_url is empty"
		return result
	}
	if !backend.LeaseExpiresAt.After(time.Now()) {
		result.Status = "down"
		result.Message = "Backend lease has expired"
		return result
	}

	result.ModuleHealth, _ = s.checkModuleHealth(ctx, moduleName, backend.BaseURL)
	result.Status, result.Message = summarizeProviderBackendHealth(result, capabilities)
	return result
}

func parseProviderCapabilities(capabilities *models.JSONString) *ProviderCapabilitiesStatus {
	status := &ProviderCapabilitiesStatus{Status: "down"}
	if capabilities == nil || strings.TrimSpace(string(*capabilities)) == "" {
		status.Message = "capabilities is empty"
		return status
	}

	payload, err := taskprovider.ParseCapabilities(string(*capabilities))
	if err != nil {
		status.Message = err.Error()
		return status
	}
	status.SchemaVersion = payload.SchemaVersion
	for _, item := range payload.TaskCapabilities {
		status.TaskCapabilities = append(status.TaskCapabilities, &ProviderTaskCapabilityStatus{
			Type:       item.Type,
			Deprecated: item.Deprecated,
		})
	}
	status.Status = "up"
	return status
}

func summarizeProviderHealth(status *ProviderHealthStatus) (string, string) {
	if len(status.Backends) == 0 {
		return "down", "TaskProvider has no Backend instance to check"
	}

	upCount, downCount, degradedCount := 0, 0, 0
	for _, backend := range status.Backends {
		switch backend.Status {
		case "up":
			upCount++
		case "down":
			downCount++
		case "degraded":
			degradedCount++
		}
	}
	if upCount == len(status.Backends) {
		return "up", ""
	}
	if downCount == len(status.Backends) {
		return "down", "all Backend instances are unready"
	}
	if upCount > 0 || downCount > 0 || degradedCount > 0 {
		return "degraded", "some Provider readiness checks did not pass"
	}
	return "unknown", "Backend health could not be determined"
}

func summarizeProviderBackendHealth(status *ProviderBackendHealthStatus, capabilities *ProviderCapabilitiesStatus) (string, string) {
	if status.ModuleHealth == nil {
		return "unknown", "module health was not checked"
	}
	if status.ModuleHealth.Status == "down" {
		return "down", status.ModuleHealth.Message
	}
	if capabilities == nil || capabilities.Status == "down" {
		if capabilities != nil {
			return "degraded", capabilities.Message
		}
		return "degraded", "capabilities was not checked"
	}
	for _, capability := range capabilities.TaskCapabilities {
		if !capability.Deprecated {
			return "up", ""
		}
	}
	return "unknown", "no active task capability"
}

func providerUnavailableMessage(reason string) string {
	switch reason {
	case "module_disabled":
		return "TaskProvider module is disabled"
	case "no_valid_backend":
		return "TaskProvider has no valid Backend lease"
	default:
		return "TaskProvider is currently unavailable"
	}
}

// checkModuleHealth 检查一个 Backend 实例的模块健康状态（带重试机制）。
func (s *HealthCheckService) checkModuleHealth(ctx context.Context, moduleName, baseURL string) (*HealthStatus, error) {
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		status, err := s.checkHealth(ctx, moduleName, baseURL)
		if err == nil {
			return status, nil
		}
		lastErr = err

		// 等待后重试（指数退避）
		if i < maxRetries-1 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	// 所有重试都失败
	return &HealthStatus{
		Module:  moduleName,
		Status:  "down",
		Latency: 0,
		Message: fmt.Sprintf("failed after %d retries: %v", maxRetries, lastErr),
	}, nil
}

// checkHealth 单次健康检查
func (s *HealthCheckService) checkHealth(ctx context.Context, moduleName, baseURL string) (*HealthStatus, error) {
	healthURL := baseURL + "/health/ready"

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	status := "down"
	if resp.StatusCode == http.StatusOK {
		status = "up"
	}

	return &HealthStatus{
		Module:  moduleName,
		Status:  status,
		Latency: latency,
	}, nil
}
