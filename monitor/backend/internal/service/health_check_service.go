package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
)

var ErrTaskProviderNotFound = errors.New("task provider not found")

type taskProviderLister interface {
	ListTaskProviders() ([]*models.TaskProvider, error)
}

// HealthCheckService 健康检查服务
type HealthCheckService struct {
	systemClient       taskProviderLister
	httpClient         *http.Client
	serviceTokenSource commonClient.ServiceTokenProvider
}

// NewHealthCheckService 创建健康检查服务
func NewHealthCheckService(systemClient taskProviderLister, tokenSource commonClient.ServiceTokenProvider) *HealthCheckService {
	return &HealthCheckService{
		systemClient: systemClient, httpClient: &http.Client{Timeout: 5 * time.Second},
		serviceTokenSource: tokenSource,
	}
}

// ModuleInfo 模块信息
type ModuleInfo struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Status  string `json:"status"`
}

// HealthStatus 健康状态
type HealthStatus struct {
	Module  string `json:"module"`
	Status  string `json:"status"`  // "up", "down", "unknown"
	Latency int64  `json:"latency"` // 毫秒
	Message string `json:"message,omitempty"`
}

// ProviderHealthStatus 是 Monitor 对 TaskProvider 运行态契约的即时观测结果。
type ProviderHealthStatus struct {
	Module        string                        `json:"module"`
	DisplayName   string                        `json:"display_name"`
	BaseURL       string                        `json:"base_url"`
	Status        string                        `json:"status"`
	Message       string                        `json:"message,omitempty"`
	ModuleHealth  *HealthStatus                 `json:"module_health"`
	Capabilities  *ProviderCapabilitiesStatus   `json:"capabilities"`
	TaskDiscovery []*ProviderTaskDiscoveryCheck `json:"task_discovery"`
	CheckedAt     time.Time                     `json:"checked_at"`
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

type ProviderTaskDiscoveryCheck struct {
	TaskType   string `json:"task_type"`
	Status     string `json:"status"`
	Endpoint   string `json:"endpoint"`
	StatusCode int    `json:"status_code,omitempty"`
	Latency    int64  `json:"latency"`
	Message    string `json:"message,omitempty"`
}

// GetModules 获取所有模块（从 System 的 task_providers 表）
func (s *HealthCheckService) GetModules(ctx context.Context) ([]*ModuleInfo, error) {
	// 从 System 获取任务提供者列表
	providers, err := s.systemClient.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get task providers: %w", err)
	}

	// 转换为模块信息
	modules := make([]*ModuleInfo, 0, len(providers))
	for _, provider := range providers {
		modules = append(modules, &ModuleInfo{
			Name:    provider.ModuleName,
			BaseURL: provider.BaseURL,
			Status:  "unknown",
		})
	}

	return modules, nil
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
func (s *HealthCheckService) CheckProviderHealth(ctx context.Context, moduleName string, tenantID uint) (*ProviderHealthStatus, error) {
	providers, err := s.systemClient.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get task providers: %w", err)
	}
	for _, provider := range providers {
		if provider.ModuleName == moduleName {
			return s.checkProviderHealth(ctx, provider, tenantID), nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrTaskProviderNotFound, moduleName)
}

// CheckAllProviderHealth 检查所有启用 TaskProvider 的运行态健康状态。
func (s *HealthCheckService) CheckAllProviderHealth(ctx context.Context, tenantID uint) ([]*ProviderHealthStatus, error) {
	providers, err := s.systemClient.ListTaskProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get task providers: %w", err)
	}

	statuses := make([]*ProviderHealthStatus, 0, len(providers))
	for _, provider := range providers {
		statuses = append(statuses, s.checkProviderHealth(ctx, provider, tenantID))
	}
	return statuses, nil
}

func (s *HealthCheckService) checkProviderHealth(ctx context.Context, provider *models.TaskProvider, tenantID uint) *ProviderHealthStatus {
	result := &ProviderHealthStatus{
		Module:      provider.ModuleName,
		DisplayName: provider.DisplayName,
		BaseURL:     provider.BaseURL,
		Status:      "unknown",
		CheckedAt:   time.Now(),
	}

	result.Capabilities = parseProviderCapabilities(provider.Capabilities)
	result.ModuleHealth, _ = s.CheckModuleHealth(ctx, provider.ModuleName, provider.BaseURL)
	if result.Capabilities.Status == "up" {
		for _, taskType := range result.Capabilities.TaskCapabilities {
			if taskType.Deprecated {
				continue
			}
			result.TaskDiscovery = append(result.TaskDiscovery, s.checkTaskDiscovery(ctx, provider, taskType.Type, tenantID))
		}
	}
	result.Status, result.Message = summarizeProviderHealth(result)
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

func (s *HealthCheckService) checkTaskDiscovery(ctx context.Context, provider *models.TaskProvider, taskType string, tenantID uint) *ProviderTaskDiscoveryCheck {
	endpoint := provider.BaseURL + provider.TaskListEndpoint
	discoveryURL := appendTaskTypeQuery(endpoint, taskType)
	check := &ProviderTaskDiscoveryCheck{
		TaskType: taskType,
		Status:   "down",
		Endpoint: discoveryURL,
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		check.Message = err.Error()
		return check
	}
	if s.serviceTokenSource == nil {
		check.Message = "service token source is not configured"
		return check
	}
	token, err := s.serviceTokenSource.Token(ctx, tenantID)
	if err != nil {
		check.Message = err.Error()
		return check
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.httpClient.Do(req)
	check.Latency = time.Since(start).Milliseconds()
	if err != nil {
		check.Message = err.Error()
		return check
	}
	defer resp.Body.Close()

	check.StatusCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := validateTaskDiscoveryResponse(resp.Body); err != nil {
			check.Message = err.Error()
			return check
		}
		check.Status = "up"
		return check
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	check.Message = strings.TrimSpace(string(body))
	return check
}

func validateTaskDiscoveryResponse(body io.Reader) error {
	_, err := taskprovider.ParseTaskListResponse(body)
	return err
}

func summarizeProviderHealth(status *ProviderHealthStatus) (string, string) {
	if status.ModuleHealth == nil {
		return "unknown", "module health was not checked"
	}
	if status.ModuleHealth.Status == "down" {
		return "down", status.ModuleHealth.Message
	}
	if status.Capabilities == nil || status.Capabilities.Status == "down" {
		if status.Capabilities != nil {
			return "degraded", status.Capabilities.Message
		}
		return "degraded", "capabilities was not checked"
	}
	if len(status.TaskDiscovery) == 0 {
		return "unknown", "no active task_type to discover"
	}

	upCount := 0
	for _, check := range status.TaskDiscovery {
		if check.Status == "up" {
			upCount++
		}
	}
	if upCount == len(status.TaskDiscovery) {
		return "up", ""
	}
	if upCount == 0 {
		return "down", "all task discovery checks failed"
	}
	return "degraded", "some task discovery checks failed"
}

func appendTaskTypeQuery(rawURL string, taskType string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		separator := "?"
		if strings.Contains(rawURL, "?") {
			separator = "&"
		}
		return rawURL + separator + "task_type=" + url.QueryEscape(taskType)
	}
	query := parsed.Query()
	query.Set("task_type", taskType)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// CheckModuleHealth 检查模块健康状态（带重试机制）
func (s *HealthCheckService) CheckModuleHealth(ctx context.Context, moduleName, baseURL string) (*HealthStatus, error) {
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
	healthURL := baseURL + "/health"

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

// CheckAllModules 检查所有模块健康状态
func (s *HealthCheckService) CheckAllModules(ctx context.Context) ([]*HealthStatus, error) {
	modules, err := s.GetModules(ctx)
	if err != nil {
		return nil, err
	}

	statuses := make([]*HealthStatus, len(modules))
	for i, module := range modules {
		status, _ := s.CheckModuleHealth(ctx, module.Name, module.BaseURL)
		statuses[i] = status
	}

	return statuses, nil
}
