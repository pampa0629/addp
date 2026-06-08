package service

import (
	"context"
	"fmt"
	"net/http"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/models"
)

// HealthCheckService 健康检查服务
type HealthCheckService struct {
	systemClient *commonClient.SystemClient
	httpClient   *http.Client
}

// NewHealthCheckService 创建健康检查服务
func NewHealthCheckService(systemClient *commonClient.SystemClient) *HealthCheckService {
	return &HealthCheckService{
		systemClient: systemClient,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
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
