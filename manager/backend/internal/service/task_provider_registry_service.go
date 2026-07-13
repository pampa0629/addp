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
	TaskCancelEndpoint  string  `json:"task_cancel_endpoint,omitempty"`
	Capabilities        *string `json:"capabilities,omitempty"` // JSON 字符串
	IsEnabled           bool    `json:"is_enabled"`
}

// Register 注册 Manager 模块为任务提供者
func (s *TaskProviderRegistryService) Register() error {
	// 构造能力描述
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v1",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "vector_tile_cache_generation",
				"display_name":              "瓦片缓存生成",
				"description":               "为空间数据项生成可复用的瓦片缓存结果",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         true,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/vector-tile-cache?tab=tasks",
				"edit_url":                  "/manager/spatial-quick-view/vector-tile-cache?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "embedding",
				"display_name":              "向量化",
				"description":               "对数据项执行向量化并生成可检索向量结果",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         true,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/vectorization-tasks",
				"edit_url":                  "/manager/vectorization-tasks?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "vector_materialized_view_generation",
				"display_name":              "矢量物化视图",
				"description":               "为 PostGIS 空间数据项创建可复用的 3857 矢量物化视图目标",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/vector-materialized-view?tab=tasks",
				"edit_url":                  "/manager/spatial-quick-view/vector-materialized-view?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "raster_cog_generation",
				"display_name":              "栅格快显 COG 生成",
				"description":               "为 TIFF/GeoTIFF 数据项生成或登记 Manager 受管的 COG",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/raster-cog?tab=tasks",
				"edit_url":                  "/manager/spatial-quick-view/raster-cog?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "raster_mosaic_generation",
				"display_name":              "栅格 mosaic 生成",
				"description":               "从资源树 node 选择一批 TIFF/GeoTIFF，生成写入业务存储的 raster_mosaic 数据集",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/raster-mosaic?tab=tasks",
				"edit_url":                  "/manager/spatial-quick-view/raster-mosaic?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "model_3d_tiles_generation",
				"display_name":              "三维模型 3D Tiles 生成",
				"description":               "将 OSGB Scene 倾斜摄影三维模型转换为写入业务存储的 3D Tiles 数据集",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/model-3d-tiles?tab=tasks",
				"edit_url":                  "/manager/model-3d-tiles?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "model_3d_glb_generation",
				"display_name":              "三维模型 GLB 快显生成",
				"description":               "将 OSGB、glTF、FBX、OBJ、STL 或 IFC 三维模型转换为 Manager 受管的 GLB 快显 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/model-3d-glb?tab=tasks",
				"edit_url":                  "/manager/model-3d-glb?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "gaussian_splat_ksplat_generation",
				"display_name":              "3DGS - KSplat 快显生成",
				"description":               "将 3DGS 高斯泼溅数据转换或发布为 Manager 受管的 KSplat 快显 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/gaussian-splat-ksplat?tab=tasks",
				"edit_url":                  "/manager/gaussian-splat-ksplat?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "point_cloud_copc_generation",
				"display_name":              "点云 COPC 快显生成",
				"description":               "将 LAS、LAZ、E57、PCD 或 XYZ 点云转换为 Manager 受管的 COPC 快显 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/point-cloud-copc?tab=tasks",
				"edit_url":                  "/manager/point-cloud-copc?tab=tasks&task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "cad_preview_generation",
				"display_name":              "CAD 栅格预览生成",
				"description":               "使用 SuperMap 直接渲染 DWG Dataset，生成 Manager 受管的 WebP 瓦片预览 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/data-explorer",
				"edit_url":                  "/manager/data-explorer",
				"deprecated":                false,
			},
		},
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
		Description: "矢量物化视图、矢量瓦片缓存、栅格快显 COG、栅格 mosaic、CAD 栅格预览、三维模型 3D Tiles、三维模型 GLB 快显、3DGS - KSplat 快显和对象存储向量化任务",

		// API 端点配置（相对于 base_url，支持 {task_type}/{id} 占位符）
		BaseURL:             s.managerURL,
		TaskListEndpoint:    "/api/v1/manager/tasks",
		TaskDetailEndpoint:  "/api/v1/manager/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/manager/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/manager/executions/{execution_id}",

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
	httpReq, err := http.NewRequest("POST", s.systemURL+"/api/v1/internal/task-providers/register", bytes.NewReader(bodyJSON))
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
