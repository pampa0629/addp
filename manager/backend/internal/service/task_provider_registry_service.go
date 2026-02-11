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
// 将 Manager 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemURL      string
	internalAPIKey string
	managerURL     string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemURL, internalAPIKey, managerURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		managerURL:     managerURL,
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
	Capabilities        *string `json:"capabilities,omitempty"` // JSON 字符串
	CreateTaskURL       string  `json:"create_task_url,omitempty"`
	EditTaskURL         string  `json:"edit_task_url,omitempty"`
	IsEnabled           bool    `json:"is_enabled"`
}

// Register 注册 Manager 模块为任务提供者
func (s *TaskProviderRegistryService) Register() error {
	// 构造能力描述
	capabilities := map[string]interface{}{
		"task_types": []map[string]string{
			{
				"type":         "tile_cache",
				"display_name": "瓦片缓存",
				"description":  "生成 MVT 矢量瓦片缓存",
			},
			{
				"type":         "data_preview",
				"display_name": "数据预览",
				"description":  "表数据、GeoJSON、Shapefile 预览",
			},
		},
		"supported_sources": []string{"postgresql", "geojson", "shapefile"},
		"features":          []string{"async", "zoom_range", "bbox_filter", "mvt"},
	}

	// 序列化为 JSON 字符串
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "manager",
		DisplayName: "数据管理",
		Description: "数据浏览、预览、MVT 瓦片缓存任务",

		// API 端点配置
		BaseURL:             s.managerURL,
		TaskListEndpoint:    "/api/manager/quick-view/tasks",   // 快显任务列表
		TaskDetailEndpoint:  "/api/manager/quick-view/:id",    // 快显任务详情
		TaskExecuteEndpoint: "/api/manager/quick-view",        // 创建快显任务
		TaskStatusEndpoint:  "/api/manager/quick-view/status", // 快显任务状态

		// 能力描述（JSON 字符串）
		Capabilities: &capabilitiesStr,

		// 前端集成 URL（可选）
		CreateTaskURL: "http://localhost:5174/#/quick-view/new",
		EditTaskURL:   "http://localhost:5174/#/quick-view/:id",

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

	log.Printf("✅ Manager 模块已成功注册到 task_providers (module_name: manager)")
	return nil
}
