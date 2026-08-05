package service

import (
	"context"
	"encoding/json"
	"fmt"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// TaskProviderRegistryService 任务提供者注册服务
// 将 Manager 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemClient *commonClient.SystemServiceClient
	managerURL   string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemClient *commonClient.SystemServiceClient, managerURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemClient: systemClient,
		managerURL:   managerURL,
	}
}

// TaskProviderRegistration 任务提供者注册请求
type TaskProviderRegistration = commonModels.TaskProvider

// Register 注册 Manager 模块为任务提供者
func (s *TaskProviderRegistryService) Register(ctx context.Context) error {
	// 构造能力描述
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "vector_tile_set_generation",
				"display_name":              "矢量瓦片生成",
				"description":               "将空间数据项生成为 Business 存储中的 PMTiles v3 数据项",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-tasks/vector-tiles?create=1",
				"edit_url":                  "/manager/spatial-tasks/vector-tiles?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "vector_tile_cache_generation",
				"display_name":              "瓦片缓存生成",
				"description":               "为空间数据项生成可复用的瓦片缓存结果",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/vector-tile-cache?create=1",
				"edit_url":                  "/manager/spatial-quick-view/vector-tile-cache?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "embedding",
				"display_name":              "向量化",
				"description":               "对数据项执行向量化并生成可检索向量结果",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         true,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/vectorization-tasks?create=1",
				"edit_url":                  "/manager/vectorization-tasks?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "vector_materialized_view_generation",
				"display_name":              "矢量物化视图",
				"description":               "为 PostGIS 空间数据项创建可复用的 3857 矢量物化视图目标",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/vector-materialized-view?create=1",
				"edit_url":                  "/manager/spatial-quick-view/vector-materialized-view?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "raster_cog_generation",
				"display_name":              "栅格快显 COG 生成",
				"description":               "为 TIFF/GeoTIFF 数据项生成或登记 Manager 受管的 COG",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/data-explorer",
				"edit_url":                  "/manager/spatial-quick-view/raster-cog?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "raster_mosaic_generation",
				"display_name":              "栅格 mosaic 生成",
				"description":               "从资源树 node 选择一批 TIFF/GeoTIFF，生成写入业务存储的 raster_mosaic 数据集",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/raster-mosaic?create=1",
				"edit_url":                  "/manager/spatial-quick-view/raster-mosaic?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "model3d_tiles_generation",
				"display_name":              "分块三维模型瓦片生成",
				"description":               "将 OSGB Scene 生成 3D Tiles 或 S3M 快显结果并写入 Manager infra MinIO",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/data-explorer",
				"edit_url":                  "/manager/model-3d-tiles?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "model_3d_glb_generation",
				"display_name":              "三维模型 GLB 快显生成",
				"description":               "将 OSGB、glTF、FBX、OBJ、STL 或 IFC 三维模型转换为 Manager 受管的 GLB 快显 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/model-3d-glb?create=1",
				"edit_url":                  "/manager/model-3d-glb?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "gaussian_splat_ksplat_generation",
				"display_name":              "3DGS - KSplat 快显生成",
				"description":               "将 3DGS 高斯泼溅数据转换或发布为 Manager 受管的 KSplat 快显 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/gaussian-splat-ksplat?create=1",
				"edit_url":                  "/manager/gaussian-splat-ksplat?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "point_cloud_copc_generation",
				"display_name":              "点云 COPC 快显生成",
				"description":               "将 LAS、LAZ、E57、PCD 或 XYZ 点云转换为 Manager 受管的 COPC 快显 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/point-cloud-copc?create=1",
				"edit_url":                  "/manager/point-cloud-copc?task_id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "cad_preview_generation",
				"display_name":              "CAD 栅格预览生成",
				"description":               "使用 SuperMap 直接渲染 DWG 或 DXF Dataset，生成 Manager 受管的 WebP 瓦片预览 artifact",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/manager/spatial-quick-view/cad-preview?create=1",
				"edit_url":                  "/manager/spatial-quick-view/cad-preview?task_id=:id",
				"deprecated":                false,
			},
		},
	}

	// 序列化为 JSON 字符串
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

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

	if s.systemClient == nil {
		return fmt.Errorf("System service client is required")
	}
	return s.systemClient.RegisterTaskProvider(ctx, &registration)
}
