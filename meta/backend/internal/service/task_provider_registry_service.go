package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// TaskProviderRegistryService 任务提供者注册服务
// 将 Meta 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemURL      string
	internalAPIKey string
	metaURL        string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemURL, internalAPIKey, metaURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		metaURL:        metaURL,
	}
}

// TaskProviderRegistration 任务提供者注册请求
type TaskProviderRegistration struct {
	ModuleName          string  `json:"module_name"`
	DisplayName         string  `json:"display_name"`
	Description         string  `json:"description"`
	BaseURL             string  `json:"base_url"`
	TaskListEndpoint    string  `json:"task_list_endpoint"`
	TaskDetailEndpoint  string  `json:"task_detail_endpoint"`
	TaskExecuteEndpoint string  `json:"task_execute_endpoint"`
	TaskStatusEndpoint  string  `json:"task_status_endpoint"`
	TaskCancelEndpoint  string  `json:"task_cancel_endpoint,omitempty"`
	Capabilities        *string `json:"capabilities,omitempty"`
	IsEnabled           bool    `json:"is_enabled"`
}

// Register 注册 Meta 模块为任务提供者
func (s *TaskProviderRegistryService) Register() error {
	// 构造能力描述（含前端集成 URL）
	capabilities := map[string]interface{}{
		"task_types": []map[string]string{
			{
				"type":         "metadata_scan",
				"display_name": "元数据扫描",
				"description":  "扫描数据库表结构、对象存储目录树",
			},
			{
				"type":         "auto_scan",
				"display_name": "自动扫描",
				"description":  "定时扫描任务（基于 Cron 表达式）",
			},
			{
				"type":         "metadata_extract",
				"display_name": "元数据提取",
				"description":  "提取文件元数据（图片、视频、文档）",
			},
		},
		"supported_source_models": []string{"tabular_catalog", "object_catalog", "file_catalog", "nosql_catalog", "graph_catalog"},
		"features":                []string{"async", "cron", "spatial_metadata", "vector_index"},
		"create_task_url":         "http://localhost:5175/#/scan/new",
		"edit_task_url":           "http://localhost:5175/#/scan/:id",
	}

	// 序列化为 JSON 字符串
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "meta",
		DisplayName: "元数据管理",
		Description: "元数据扫描、索引、向量化任务",

		// API 端点配置
		BaseURL:             s.metaURL,
		TaskListEndpoint:    "/api/v1/meta/scan/tasks",                  // 扫描任务列表
		TaskDetailEndpoint:  "/api/v1/meta/scan/tasks/:task_id",         // 扫描任务详情
		TaskExecuteEndpoint: "/api/v1/meta/scan/tasks/:task_id/trigger", // 执行扫描任务
		TaskStatusEndpoint:  "/api/v1/meta/scan/runs/:run_id",           // 查询执行状态（UUID）
		TaskCancelEndpoint:  "/api/v1/meta/scan/runs/:run_id/cancel",    // 取消执行

		// 能力描述（JSON 字符串）
		Capabilities: &capabilitiesStr,

		IsEnabled: true,
	}

	return s.sendRegistration(&registration)
}

// sendRegistration 发送注册请求到 System task_providers API
func (s *TaskProviderRegistryService) sendRegistration(req *TaskProviderRegistration) error {
	bodyJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	// 注册到 task_providers Internal API（使用 Internal API Key 认证）
	httpReq, err := http.NewRequest("POST", s.systemURL+"/internal/task-providers/register", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-API-Key", s.internalAPIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// 读取响应 body 以获取详细错误信息
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("registration failed with status %d: %v", resp.StatusCode, errBody)
	}

	log.Printf("✅ Meta 模块已成功注册到 task_providers (module_name: meta)")
	return nil
}
