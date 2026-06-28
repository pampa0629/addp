package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// TaskProviderHandler 标准 TaskProvider API 处理器
// 实现: GET /api/v1/manager/tasks, POST /api/v1/manager/tasks/:task_type/:id/execute
//
//	GET /api/v1/manager/tasks/:task_type/:id, GET /api/v1/manager/executions/:execution_id
type TaskProviderHandler struct {
	embeddingTaskSvc             *service.EmbeddingTaskService
	tileCacheTaskSvc             *service.TileCacheTaskService
	quickViewOptimizationTaskSvc *service.QuickViewOptimizationTaskService
	rasterCOGTaskSvc             *service.RasterCOGTaskService
	rasterMosaicTaskSvc          *service.RasterMosaicTaskService
	model3DTilesTaskSvc          *service.Model3DTilesTaskService
	taskExecRepo                 *commonExecution.TaskExecutionRepository
}

// NewTaskProviderHandler 创建处理器
func NewTaskProviderHandler(
	embeddingTaskSvc *service.EmbeddingTaskService,
	tileCacheTaskSvc *service.TileCacheTaskService,
	quickViewOptimizationTaskSvc *service.QuickViewOptimizationTaskService,
	rasterCOGTaskSvc *service.RasterCOGTaskService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	rasterMosaicTaskSvc ...*service.RasterMosaicTaskService,
) *TaskProviderHandler {
	handler := &TaskProviderHandler{
		embeddingTaskSvc:             embeddingTaskSvc,
		tileCacheTaskSvc:             tileCacheTaskSvc,
		quickViewOptimizationTaskSvc: quickViewOptimizationTaskSvc,
		rasterCOGTaskSvc:             rasterCOGTaskSvc,
		taskExecRepo:                 taskExecRepo,
	}
	if len(rasterMosaicTaskSvc) > 0 {
		handler.rasterMosaicTaskSvc = rasterMosaicTaskSvc[0]
	}
	return handler
}

func (h *TaskProviderHandler) SetModel3DTilesTaskService(model3DTilesTaskSvc *service.Model3DTilesTaskService) {
	h.model3DTilesTaskSvc = model3DTilesTaskSvc
}

// TaskListResponse 任务列表响应（统一包装 Manager provider 声明的任务类型）
type TaskListItem struct {
	ID                  uint    `json:"id"`
	TenantID            uint    `json:"tenant_id"`
	TaskType            string  `json:"task_type"`
	Name                string  `json:"name"`
	Description         string  `json:"description,omitempty"`
	Enabled             bool    `json:"enabled"`
	LastExecutionID     *string `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string `json:"last_execution_status,omitempty"`
}

type TaskListResponse struct {
	Items    []TaskListItem `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

// EmbeddingTaskRequest 是私有向量化任务 CRUD 的显式契约。
type EmbeddingTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type EmbeddingTaskTargetResponse struct {
	Scope     string `json:"scope"`
	EngineID  uint   `json:"engine_id,omitempty"`
	ItemID    uint   `json:"item_id,omitempty"`
	NodeID    uint   `json:"node_id,omitempty"`
	Locator   string `json:"locator,omitempty"`
	Recursive bool   `json:"recursive"`
}

type EmbeddingTaskResponse struct {
	ID                  uint                         `json:"id"`
	TenantID            uint                         `json:"tenant_id"`
	TaskType            string                       `json:"task_type"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description,omitempty"`
	Enabled             bool                         `json:"enabled"`
	Schedule            string                       `json:"schedule,omitempty"`
	NextRunAt           *time.Time                   `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                   `json:"last_run_at,omitempty"`
	LastExecutionID     *string                      `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                      `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                        `json:"created_by,omitempty"`
	Config              commonModels.JSONMap         `json:"config"`
	Target              *EmbeddingTaskTargetResponse `json:"target,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

type TileCacheTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type TileCacheTaskTargetResponse struct {
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	Locator         string `json:"locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	Schema          string `json:"schema,omitempty"`
	Table           string `json:"table,omitempty"`
}

type TileCacheTaskTileResponse struct {
	Format         string `json:"format"`
	MinZoom        int    `json:"min_zoom"`
	MaxZoom        int    `json:"max_zoom"`
	TargetSRID     int    `json:"target_srid,omitempty"`
	GeometryColumn string `json:"geometry_column,omitempty"`
}

type TileCacheTaskResponse struct {
	ID                  uint                         `json:"id"`
	TenantID            uint                         `json:"tenant_id"`
	TaskType            string                       `json:"task_type"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description,omitempty"`
	Enabled             bool                         `json:"enabled"`
	Schedule            string                       `json:"schedule,omitempty"`
	NextRunAt           *time.Time                   `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                   `json:"last_run_at,omitempty"`
	LastExecutionID     *string                      `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                      `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                        `json:"created_by,omitempty"`
	Config              commonModels.JSONMap         `json:"config"`
	Target              *TileCacheTaskTargetResponse `json:"target,omitempty"`
	Tile                *TileCacheTaskTileResponse   `json:"tile,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

type QuickViewOptimizationTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type QuickViewOptimizationTaskTargetResponse struct {
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	Locator         string `json:"locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	Schema          string `json:"schema,omitempty"`
	Table           string `json:"table,omitempty"`
}

type QuickViewOptimizationTaskGeometryResponse struct {
	GeometryColumn string `json:"geometry_column,omitempty"`
	SourceSRID     int    `json:"source_srid,omitempty"`
	TargetSRID     int    `json:"target_srid,omitempty"`
}

type QuickViewOptimizationTaskResponse struct {
	ID                  uint                                       `json:"id"`
	TenantID            uint                                       `json:"tenant_id"`
	TaskType            string                                     `json:"task_type"`
	Name                string                                     `json:"name"`
	Description         string                                     `json:"description,omitempty"`
	Enabled             bool                                       `json:"enabled"`
	Schedule            string                                     `json:"schedule,omitempty"`
	NextRunAt           *time.Time                                 `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                                 `json:"last_run_at,omitempty"`
	LastExecutionID     *string                                    `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                                    `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                                      `json:"created_by,omitempty"`
	Config              commonModels.JSONMap                       `json:"config"`
	Target              *QuickViewOptimizationTaskTargetResponse   `json:"target,omitempty"`
	Geometry            *QuickViewOptimizationTaskGeometryResponse `json:"geometry,omitempty"`
	CreatedAt           time.Time                                  `json:"created_at"`
	UpdatedAt           time.Time                                  `json:"updated_at"`
}

type RasterCOGTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type RasterCOGTaskTargetResponse struct {
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	Locator         string `json:"locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
}

type RasterCOGTaskRasterResponse struct {
	SourceProfile   string    `json:"source_profile,omitempty"`
	SourceSizeBytes int64     `json:"source_size_bytes,omitempty"`
	Width           int64     `json:"width,omitempty"`
	Height          int64     `json:"height,omitempty"`
	BandCount       int64     `json:"band_count,omitempty"`
	SourceSRID      int       `json:"source_srid,omitempty"`
	Extent          []float64 `json:"extent,omitempty"`
	ExtentSRID      int       `json:"extent_srid,omitempty"`
}

type RasterCOGTaskCOGResponse struct {
	Compression        string `json:"compression,omitempty"`
	BlockSize          int    `json:"blocksize,omitempty"`
	OverviewResampling string `json:"overview_resampling,omitempty"`
}

type RasterCOGTaskResponse struct {
	ID                  uint                         `json:"id"`
	TenantID            uint                         `json:"tenant_id"`
	TaskType            string                       `json:"task_type"`
	Name                string                       `json:"name"`
	Description         string                       `json:"description,omitempty"`
	Enabled             bool                         `json:"enabled"`
	Schedule            string                       `json:"schedule,omitempty"`
	NextRunAt           *time.Time                   `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                   `json:"last_run_at,omitempty"`
	LastExecutionID     *string                      `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                      `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                        `json:"created_by,omitempty"`
	Config              commonModels.JSONMap         `json:"config"`
	Target              *RasterCOGTaskTargetResponse `json:"target,omitempty"`
	Raster              *RasterCOGTaskRasterResponse `json:"raster,omitempty"`
	COG                 *RasterCOGTaskCOGResponse    `json:"cog,omitempty"`
	CreatedAt           time.Time                    `json:"created_at"`
	UpdatedAt           time.Time                    `json:"updated_at"`
}

type RasterMosaicTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type RasterMosaicTaskSourceResponse struct {
	NodeLocator     string   `json:"node_locator,omitempty"`
	SourceEngineID  uint     `json:"source_engine_id,omitempty"`
	Recursive       bool     `json:"recursive"`
	IncludePatterns []string `json:"include_patterns,omitempty"`
	ExcludePatterns []string `json:"exclude_patterns,omitempty"`
}

type RasterMosaicTaskTargetResponse struct {
	StorageLocator string `json:"storage_locator,omitempty"`
	TargetEngineID uint   `json:"target_engine_id,omitempty"`
	DatasetName    string `json:"dataset_name,omitempty"`
}

type RasterMosaicTaskPlacementResponse struct {
	Mode string `json:"mode"`
}

type RasterMosaicTaskResponse struct {
	ID                  uint                               `json:"id"`
	TenantID            uint                               `json:"tenant_id"`
	TaskType            string                             `json:"task_type"`
	Name                string                             `json:"name"`
	Description         string                             `json:"description,omitempty"`
	Enabled             bool                               `json:"enabled"`
	Schedule            string                             `json:"schedule,omitempty"`
	NextRunAt           *time.Time                         `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                         `json:"last_run_at,omitempty"`
	LastExecutionID     *string                            `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                            `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                              `json:"created_by,omitempty"`
	Config              commonModels.JSONMap               `json:"config"`
	Source              *RasterMosaicTaskSourceResponse    `json:"source,omitempty"`
	Target              *RasterMosaicTaskTargetResponse    `json:"target,omitempty"`
	Placement           *RasterMosaicTaskPlacementResponse `json:"placement,omitempty"`
	CreatedAt           time.Time                          `json:"created_at"`
	UpdatedAt           time.Time                          `json:"updated_at"`
}

type Model3DTilesTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type Model3DTilesTaskSourceResponse struct {
	ItemLocator    string `json:"item_locator,omitempty"`
	SourceEngineID uint   `json:"source_engine_id,omitempty"`
	Format         string `json:"format,omitempty"`
}

type Model3DTilesTaskTargetResponse struct {
	StorageLocator string `json:"storage_locator,omitempty"`
	TargetEngineID uint   `json:"target_engine_id,omitempty"`
	DatasetName    string `json:"dataset_name,omitempty"`
}

type Model3DTilesTaskTilesResponse struct {
	Format string `json:"format"`
}

type Model3DTilesTaskResponse struct {
	ID                  uint                            `json:"id"`
	TenantID            uint                            `json:"tenant_id"`
	TaskType            string                          `json:"task_type"`
	Name                string                          `json:"name"`
	Description         string                          `json:"description,omitempty"`
	Enabled             bool                            `json:"enabled"`
	Schedule            string                          `json:"schedule,omitempty"`
	NextRunAt           *time.Time                      `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                      `json:"last_run_at,omitempty"`
	LastExecutionID     *string                         `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                         `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                           `json:"created_by,omitempty"`
	Config              commonModels.JSONMap            `json:"config"`
	Source              *Model3DTilesTaskSourceResponse `json:"source,omitempty"`
	Target              *Model3DTilesTaskTargetResponse `json:"target,omitempty"`
	Tiles               *Model3DTilesTaskTilesResponse  `json:"tiles,omitempty"`
	CreatedAt           time.Time                       `json:"created_at"`
	UpdatedAt           time.Time                       `json:"updated_at"`
}

// ListTasks GET /api/v1/manager/tasks
// 查询参数：?task_type=vector_tile_cache_generation|vector_quick_view_target_generation|raster_cog_generation|raster_mosaic_generation|model_3d_tiles_generation|embedding
// @Summary 列出任务 | List tasks
// @Description 列出 Manager 模块的任务（矢量瓦片缓存生成、矢量快显性能优化、栅格快显 COG 生成、栅格 mosaic 生成、三维模型 3D Tiles 生成和向量化任务）| List Manager module tasks
// @Tags Manager
// @Produce json
// @Param task_type query string false "任务类型过滤：vector_tile_cache_generation|vector_quick_view_target_generation|raster_cog_generation|raster_mosaic_generation|model_3d_tiles_generation|embedding | Task type filter"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} TaskListResponse "任务列表 | Task list"
// @Failure 400 {object} map[string]interface{} "不支持的任务类型 | Unsupported task type"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	h.listTasks(c, strings.TrimSpace(c.Query("task_type")))
}

func (h *TaskProviderHandler) listTasks(c *gin.Context, taskType string) {
	tenantID := c.GetUint("tenant_id")

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	ctx := c.Request.Context()
	var items []TaskListItem
	var total int64

	switch taskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		if h.tileCacheTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "tile cache task service is unavailable"})
			return
		}
		tasks, t, err := h.tileCacheTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorTileCacheGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeVectorQuickViewTargetGeneration:
		if h.quickViewOptimizationTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "quick view optimization task service is unavailable"})
			return
		}
		tasks, t, err := h.quickViewOptimizationTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorQuickViewTargetGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeRasterCOGGeneration:
		if h.rasterCOGTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "raster COG generation task service is unavailable"})
			return
		}
		tasks, t, err := h.rasterCOGTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeRasterCOGGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeRasterMosaicGeneration:
		if h.rasterMosaicTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "raster mosaic generation task service is unavailable"})
			return
		}
		tasks, t, err := h.rasterMosaicTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeRasterMosaicGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeModel3DTilesGeneration:
		if h.model3DTilesTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "model 3d tiles generation task service is unavailable"})
			return
		}
		tasks, t, err := h.model3DTilesTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeModel3DTilesGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeEmbedding:
		if h.embeddingTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "embedding task service is unavailable"})
			return
		}
		tasks, t, err := h.embeddingTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeEmbedding,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case "":
		// 返回所有类型
		if h.tileCacheTaskSvc != nil {
			tasks, t, err := h.tileCacheTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorTileCacheGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.quickViewOptimizationTaskSvc != nil {
			tasks, t, err := h.quickViewOptimizationTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorQuickViewTargetGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.rasterCOGTaskSvc != nil {
			tasks, t, err := h.rasterCOGTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeRasterCOGGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.rasterMosaicTaskSvc != nil {
			tasks, t, err := h.rasterMosaicTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeRasterMosaicGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.model3DTilesTaskSvc != nil {
			tasks, t, err := h.model3DTilesTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeModel3DTilesGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.embeddingTaskSvc != nil {
			tasks, t, err := h.embeddingTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeEmbedding, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的任务类型: " + taskType})
		return
	}

	if items == nil {
		items = []TaskListItem{}
	}
	c.JSON(http.StatusOK, TaskListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// ListTileCacheTasks GET /api/v1/manager/vector_tile_cache_tasks
// @Summary 列出瓦片缓存生成任务配置 | List tile cache generation task configurations
// @Description 列出 Manager 模块的瓦片缓存生成任务配置。该私有入口固定返回 task_type=vector_tile_cache_generation；编排模块应使用标准 /tasks 入口。| List Manager tile cache generation task configurations. This private endpoint always returns task_type=vector_tile_cache_generation; orchestrator should use the standard /tasks endpoint.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /vector_tile_cache_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTileCacheTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := h.tileCacheTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]TileCacheTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, tileCacheTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// ListEmbeddingTasks GET /api/v1/manager/embedding_tasks
// @Summary 列出向量化任务配置 | List embedding task configurations
// @Description 列出 Manager 模块的向量化任务配置。该私有入口固定返回 task_type=embedding；编排模块应使用标准 /tasks 入口。| List Manager embedding task configurations. This private endpoint always returns task_type=embedding; orchestrator should use the standard /tasks endpoint.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /embedding_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListEmbeddingTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := h.embeddingTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]EmbeddingTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, embeddingTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// TaskDetail GET /api/v1/manager/tasks/:task_type/:id
// @Summary 获取任务详情 | Get task detail
// @Description 获取指定类型和ID的任务详细信息 | Get detailed information of a task by type and ID
// @Tags Manager
// @Produce json
// @Param task_type path string true "任务类型：vector_tile_cache_generation|vector_quick_view_target_generation|raster_cog_generation|raster_mosaic_generation|model_3d_tiles_generation|embedding | Task type"
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} object "任务详情，按 task_type 返回矢量瓦片缓存、矢量快显性能优化、栅格 COG 生成或向量化任务详情 | Task detail by task_type"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tasks/{task_type}/{id} [get]
// @Router /embedding_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskDetail(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	taskType := c.Param("task_type")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	ctx := c.Request.Context()
	switch taskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		task, err := h.tileCacheTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, tileCacheTaskResponse(task))
	case commonExecution.TaskTypeVectorQuickViewTargetGeneration:
		task, err := h.quickViewOptimizationTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, quickViewOptimizationTaskResponse(task))
	case commonExecution.TaskTypeRasterCOGGeneration:
		task, err := h.rasterCOGTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, rasterCOGTaskResponse(task))
	case commonExecution.TaskTypeRasterMosaicGeneration:
		task, err := h.rasterMosaicTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, rasterMosaicTaskResponse(task))
	case commonExecution.TaskTypeModel3DTilesGeneration:
		task, err := h.model3DTilesTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, model3DTilesTaskResponse(task))
	case commonExecution.TaskTypeEmbedding:
		task, err := h.embeddingTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusOK, embeddingTaskResponse(task))
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的任务类型: " + taskType})
	}
}

// GetTileCacheTask GET /api/v1/manager/vector_tile_cache_tasks/:id
// @Summary 获取瓦片缓存生成任务配置 | Get tile cache generation task configuration
// @Description 获取指定瓦片缓存生成任务配置 | Get a specific tile cache generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} TileCacheTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /vector_tile_cache_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	task, err := h.tileCacheTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, tileCacheTaskResponse(task))
}

// TaskExecuteRequest 触发执行请求
type TaskExecuteRequest struct {
	TriggerType       string                 `json:"trigger_type"`        // manual|scheduled，默认 manual
	Source            string                 `json:"source"`              // 触发来源模块
	ParentExecutionID string                 `json:"parent_execution_id"` // 父执行ID（Orchestrator 调用时传入）
	Parameters        map[string]interface{} `json:"parameters"`          // 执行参数覆盖；当前 Manager provider 不支持
}

type TaskExecuteResponse struct {
	Status      string `json:"status"`
	ExecutionID string `json:"execution_id"`
}

// TaskExecute POST /api/v1/manager/tasks/:task_type/:id/execute
// @Summary 执行任务 | Execute task
// @Description 触发指定任务立即执行 | Trigger immediate execution of a specific task
// @Tags Manager
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型：vector_tile_cache_generation|vector_quick_view_target_generation|raster_cog_generation|raster_mosaic_generation|model_3d_tiles_generation|embedding | Task type"
// @Param id path int true "任务ID | Task ID"
// @Param body body TaskExecuteRequest false "执行配置 | Execution configuration"
// @Success 202 {object} TaskExecuteResponse "执行ID | Execution ID"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskExecute(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	taskType := c.Param("task_type")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	var req TaskExecuteRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	triggerType, err := commonExecution.NormalizeTriggerType(req.TriggerType)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = commonExecution.ModuleManager
	}
	if len(req.Parameters) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Manager task provider does not support execution parameter overrides"})
		return
	}
	var parentExecID *string
	if req.ParentExecutionID != "" {
		parentExecID = &req.ParentExecutionID
	}

	ctx := c.Request.Context()
	var executionID string

	switch taskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		executionID, err = h.tileCacheTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	case commonExecution.TaskTypeVectorQuickViewTargetGeneration:
		executionID, err = h.quickViewOptimizationTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	case commonExecution.TaskTypeRasterCOGGeneration:
		executionID, err = h.rasterCOGTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	case commonExecution.TaskTypeRasterMosaicGeneration:
		executionID, err = h.rasterMosaicTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	case commonExecution.TaskTypeModel3DTilesGeneration:
		executionID, err = h.model3DTilesTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	case commonExecution.TaskTypeEmbedding:
		executionID, err = h.embeddingTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的任务类型: " + taskType})
		return
	}

	if err != nil {
		if errors.Is(err, service.ErrTaskNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, TaskExecuteResponse{
		Status:      commonExecution.ExecutionStatusRunning,
		ExecutionID: executionID,
	})
}

// ExecutionStatus GET /api/v1/manager/executions/:execution_id
// @Summary 获取执行状态 | Get execution status
// @Description 获取任务执行记录的状态信息 | Get status information of a task execution record
// @Tags Manager
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} execution.TaskExecution "执行状态 | Execution status"
// @Failure 404 {object} map[string]interface{} "执行记录不存在 | Execution not found"
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ExecutionStatus(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	executionID := c.Param("execution_id")

	exec, err := h.taskExecRepo.GetByExecutionID(c.Request.Context(), executionID, int(tenantID))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "执行记录不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, exec)
}

// ===== EmbeddingTask CRUD =====

// CreateEmbeddingTask POST /api/v1/manager/embedding_tasks
// @Summary 创建向量化任务配置 | Create embedding task configuration
// @Description 创建新的向量化任务配置（定时或手动触发）| Create a new embedding task configuration (scheduled or manual)
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body EmbeddingTaskRequest true "向量化任务配置 | Embedding task configuration"
// @Success 201 {object} EmbeddingTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /embedding_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	req, err := decodeEmbeddingTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.EmbeddingTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}

	if err := h.embeddingTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, embeddingTaskResponse(&task))
}

// UpdateEmbeddingTask PUT /api/v1/manager/embedding_tasks/:id
// @Summary 更新向量化任务配置 | Update embedding task configuration
// @Description 更新指定的向量化任务配置 | Update a specific embedding task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body EmbeddingTaskRequest true "向量化任务配置 | Embedding task configuration"
// @Success 200 {object} EmbeddingTaskResponse "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /embedding_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	existing, err := h.embeddingTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	req, err := decodeEmbeddingTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.ID = uint(id)
	existing.TenantID = tenantID
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config

	if err := h.embeddingTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, embeddingTaskResponse(existing))
}

// DeleteEmbeddingTask DELETE /api/v1/manager/embedding_tasks/:id
// @Summary 删除向量化任务配置 | Delete embedding task configuration
// @Description 删除指定的向量化任务配置 | Delete a specific embedding task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /embedding_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteEmbeddingTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	if err := h.embeddingTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

func decodeEmbeddingTaskRequest(c *gin.Context) (EmbeddingTaskRequest, error) {
	var req EmbeddingTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func embeddingTaskResponse(task *models.EmbeddingTask) EmbeddingTaskResponse {
	resp := EmbeddingTaskResponse{}
	if task == nil {
		return resp
	}
	resp = EmbeddingTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeEmbedding,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &EmbeddingTaskTargetResponse{
			Scope:     stringFromConfig(target["scope"]),
			EngineID:  uintFromConfig(target["engine_id"]),
			ItemID:    uintFromConfig(target["item_id"]),
			NodeID:    uintFromConfig(target["node_id"]),
			Locator:   stringFromConfig(target["locator"]),
			Recursive: boolFromConfig(target["recursive"], true),
		}
	}
	return resp
}

func asJSONMap(value interface{}) (commonModels.JSONMap, bool) {
	switch v := value.(type) {
	case commonModels.JSONMap:
		return v, true
	case map[string]interface{}:
		return commonModels.JSONMap(v), true
	default:
		return nil, false
	}
}

func uintFromConfig(value interface{}) uint {
	switch v := value.(type) {
	case uint:
		return v
	case int:
		if v > 0 {
			return uint(v)
		}
	case int64:
		if v > 0 {
			return uint(v)
		}
	case float64:
		if v > 0 {
			return uint(v)
		}
	}
	return 0
}

func stringFromConfig(value interface{}) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func boolFromConfig(value interface{}, defaultValue bool) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	return defaultValue
}

// ===== TileCacheTask CRUD =====

// CreateTileCacheTask POST /api/v1/manager/vector_tile_cache_tasks
// @Summary 创建瓦片缓存生成任务配置 | Create tile cache generation task configuration
// @Description 创建新的瓦片缓存生成任务配置 | Create a new tile cache generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body TileCacheTaskRequest true "瓦片缓存任务配置 | Tile cache task configuration"
// @Success 201 {object} map[string]interface{} "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /vector_tile_cache_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	req, err := decodeTileCacheTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.TileCacheTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}

	if err := h.tileCacheTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, tileCacheTaskResponse(&task))
}

// UpdateTileCacheTask PUT /api/v1/manager/vector_tile_cache_tasks/:id
// @Summary 更新瓦片缓存生成任务配置 | Update tile cache generation task configuration
// @Description 更新指定的瓦片缓存生成任务配置 | Update a specific tile cache generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body TileCacheTaskRequest true "瓦片缓存任务配置 | Tile cache task configuration"
// @Success 200 {object} map[string]interface{} "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /vector_tile_cache_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	existing, err := h.tileCacheTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}

	req, err := decodeTileCacheTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.ID = uint(id)
	existing.TenantID = tenantID
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config

	if err := h.tileCacheTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tileCacheTaskResponse(existing))
}

// DeleteTileCacheTask DELETE /api/v1/manager/vector_tile_cache_tasks/:id
// @Summary 删除瓦片缓存生成任务配置 | Delete tile cache generation task configuration
// @Description 删除指定的瓦片缓存生成任务配置 | Delete a specific tile cache generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /vector_tile_cache_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteTileCacheTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}

	if err := h.tileCacheTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListQuickViewOptimizationTasks GET /api/v1/manager/vector_quick_view_target_tasks
// @Summary 列出快显性能优化任务配置 | List quick view optimization task configurations
// @Description 列出 Manager 模块的快显性能优化任务配置。该私有入口固定返回 task_type=vector_quick_view_target_generation；编排模块应使用标准 /tasks 入口。| List Manager quick view optimization task configurations. This private endpoint always returns task_type=vector_quick_view_target_generation; orchestrator should use the standard /tasks endpoint.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Router /vector_quick_view_target_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListQuickViewOptimizationTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.quickViewOptimizationTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]QuickViewOptimizationTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, quickViewOptimizationTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateQuickViewOptimizationTask POST /api/v1/manager/vector_quick_view_target_tasks
// @Summary 创建快显性能优化任务配置 | Create quick view optimization task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body QuickViewOptimizationTaskRequest true "快显性能优化任务配置 | Quick view optimization task configuration"
// @Success 201 {object} QuickViewOptimizationTaskResponse "创建的任务配置 | Created task configuration"
// @Router /vector_quick_view_target_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateQuickViewOptimizationTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")
	req, err := decodeQuickViewOptimizationTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.QuickViewOptimizationTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.quickViewOptimizationTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, quickViewOptimizationTaskResponse(&task))
}

// GetQuickViewOptimizationTask GET /api/v1/manager/vector_quick_view_target_tasks/:id
// @Summary 获取快显性能优化任务配置 | Get quick view optimization task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} QuickViewOptimizationTaskResponse "任务配置 | Task configuration"
// @Router /vector_quick_view_target_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetQuickViewOptimizationTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeVectorQuickViewTargetGeneration})
	h.TaskDetail(c)
}

// UpdateQuickViewOptimizationTask PUT /api/v1/manager/vector_quick_view_target_tasks/:id
// @Summary 更新快显性能优化任务配置 | Update quick view optimization task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body QuickViewOptimizationTaskRequest true "快显性能优化任务配置 | Quick view optimization task configuration"
// @Success 200 {object} QuickViewOptimizationTaskResponse "更新后的任务配置 | Updated task configuration"
// @Router /vector_quick_view_target_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateQuickViewOptimizationTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.quickViewOptimizationTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeQuickViewOptimizationTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config
	if err := h.quickViewOptimizationTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, quickViewOptimizationTaskResponse(existing))
}

// DeleteQuickViewOptimizationTask DELETE /api/v1/manager/vector_quick_view_target_tasks/:id
// @Summary 删除快显性能优化任务配置 | Delete quick view optimization task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Router /vector_quick_view_target_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteQuickViewOptimizationTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.quickViewOptimizationTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListRasterCOGTasks GET /api/v1/manager/raster_cog_tasks
// @Summary 列出栅格快显 COG 任务配置 | List raster COG generation task configurations
// @Description 列出 Manager 模块的栅格快显 COG 任务配置。该私有入口固定返回 task_type=raster_cog_generation；编排模块应使用标准 /tasks 入口。| List Manager raster COG generation task configurations.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Router /raster_cog_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListRasterCOGTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.rasterCOGTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]RasterCOGTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, rasterCOGTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateRasterCOGTask POST /api/v1/manager/raster_cog_tasks
// @Summary 创建栅格快显 COG 任务配置 | Create raster COG generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body RasterCOGTaskRequest true "raster COG generation task configuration"
// @Success 201 {object} RasterCOGTaskResponse "创建的任务配置 | Created task configuration"
// @Router /raster_cog_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateRasterCOGTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")
	req, err := decodeRasterCOGTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.RasterCOGTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.rasterCOGTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rasterCOGTaskResponse(&task))
}

// GetRasterCOGTask GET /api/v1/manager/raster_cog_tasks/:id
// @Summary 获取栅格快显 COG 任务配置 | Get raster COG generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} RasterCOGTaskResponse "任务配置 | Task configuration"
// @Router /raster_cog_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetRasterCOGTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeRasterCOGGeneration})
	h.TaskDetail(c)
}

// UpdateRasterCOGTask PUT /api/v1/manager/raster_cog_tasks/:id
// @Summary 更新栅格快显 COG 任务配置 | Update raster COG generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body RasterCOGTaskRequest true "raster COG generation task configuration"
// @Success 200 {object} RasterCOGTaskResponse "更新后的任务配置 | Updated task configuration"
// @Router /raster_cog_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateRasterCOGTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.rasterCOGTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeRasterCOGTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config
	if err := h.rasterCOGTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rasterCOGTaskResponse(existing))
}

// DeleteRasterCOGTask DELETE /api/v1/manager/raster_cog_tasks/:id
// @Summary 删除栅格快显 COG 任务配置 | Delete raster COG generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Router /raster_cog_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteRasterCOGTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.rasterCOGTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListRasterMosaicTasks GET /api/v1/manager/raster_mosaic_tasks
// @Summary 列出栅格 mosaic 任务配置 | List raster mosaic generation task configurations
// @Description 列出 Manager 模块的栅格 mosaic 任务配置。该私有入口固定返回 task_type=raster_mosaic_generation；编排模块应使用标准 /tasks 入口。| List Manager raster mosaic generation task configurations.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Router /raster_mosaic_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListRasterMosaicTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.rasterMosaicTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]RasterMosaicTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, rasterMosaicTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateRasterMosaicTask POST /api/v1/manager/raster_mosaic_tasks
// @Summary 创建栅格 mosaic 任务配置 | Create raster mosaic generation task configuration
// @Description 创建新的栅格 mosaic 任务配置。任务从资源树 node 选择源数据，并将 mosaic 数据集写入用户选择的业务存储。| Create a raster mosaic task from a resource-tree node into the selected business storage.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body RasterMosaicTaskRequest true "raster mosaic generation task configuration"
// @Success 201 {object} RasterMosaicTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /raster_mosaic_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateRasterMosaicTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")
	req, err := decodeRasterMosaicTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.RasterMosaicTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.rasterMosaicTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, rasterMosaicTaskResponse(&task))
}

// GetRasterMosaicTask GET /api/v1/manager/raster_mosaic_tasks/:id
// @Summary 获取栅格 mosaic 任务配置 | Get raster mosaic generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} RasterMosaicTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /raster_mosaic_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetRasterMosaicTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeRasterMosaicGeneration})
	h.TaskDetail(c)
}

// UpdateRasterMosaicTask PUT /api/v1/manager/raster_mosaic_tasks/:id
// @Summary 更新栅格 mosaic 任务配置 | Update raster mosaic generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body RasterMosaicTaskRequest true "raster mosaic generation task configuration"
// @Success 200 {object} RasterMosaicTaskResponse "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /raster_mosaic_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateRasterMosaicTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.rasterMosaicTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeRasterMosaicTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config
	if err := h.rasterMosaicTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rasterMosaicTaskResponse(existing))
}

// DeleteRasterMosaicTask DELETE /api/v1/manager/raster_mosaic_tasks/:id
// @Summary 删除栅格 mosaic 任务配置 | Delete raster mosaic generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /raster_mosaic_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteRasterMosaicTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.rasterMosaicTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListModel3DTilesTasks GET /api/v1/manager/model_3d_tiles_tasks
// @Summary 列出三维模型 3D Tiles 任务配置 | List model 3D Tiles generation task configurations
// @Description 列出 Manager 模块的三维模型 3D Tiles 任务配置。该私有入口固定返回 task_type=model_3d_tiles_generation；编排模块应使用标准 /tasks 入口。| List Manager model 3D Tiles generation task configurations.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @Router /model_3d_tiles_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListModel3DTilesTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.model3DTilesTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]Model3DTilesTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, model3DTilesTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateModel3DTilesTask POST /api/v1/manager/model_3d_tiles_tasks
// @Summary 创建三维模型 3D Tiles 任务配置 | Create model 3D Tiles generation task configuration
// @Description 创建新的三维模型 3D Tiles 任务配置。任务从 OSGB whole item 读取源数据，并将 3D Tiles 数据集写入用户选择的业务存储。| Create a model 3D Tiles task from an OSGB whole item into selected business storage.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body Model3DTilesTaskRequest true "model 3D Tiles generation task configuration"
// @Success 201 {object} Model3DTilesTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /model_3d_tiles_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateModel3DTilesTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")
	req, err := decodeModel3DTilesTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.Model3DTilesTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.model3DTilesTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, model3DTilesTaskResponse(&task))
}

// GetModel3DTilesTask GET /api/v1/manager/model_3d_tiles_tasks/:id
// @Summary 获取三维模型 3D Tiles 任务配置 | Get model 3D Tiles generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} Model3DTilesTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /model_3d_tiles_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetModel3DTilesTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeModel3DTilesGeneration})
	h.TaskDetail(c)
}

// UpdateModel3DTilesTask PUT /api/v1/manager/model_3d_tiles_tasks/:id
// @Summary 更新三维模型 3D Tiles 任务配置 | Update model 3D Tiles generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body Model3DTilesTaskRequest true "model 3D Tiles generation task configuration"
// @Success 200 {object} Model3DTilesTaskResponse "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Router /model_3d_tiles_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateModel3DTilesTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.model3DTilesTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeModel3DTilesTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.Description = strings.TrimSpace(req.Description)
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.Schedule = strings.TrimSpace(req.Schedule)
	existing.NextRunAt = req.NextRunAt
	existing.Config = req.Config
	if err := h.model3DTilesTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, model3DTilesTaskResponse(existing))
}

// DeleteModel3DTilesTask DELETE /api/v1/manager/model_3d_tiles_tasks/:id
// @Summary 删除三维模型 3D Tiles 任务配置 | Delete model 3D Tiles generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Router /model_3d_tiles_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteModel3DTilesTask(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.model3DTilesTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListRasterCOGs GET /api/v1/manager/raster_cog
// @Summary 列出栅格快显 COG | List raster COG results
// @Tags Manager
// @Produce json
// @Param item_id query int false "数据项ID | Item ID"
// @Param item_fingerprint query string false "数据项指纹 | Item fingerprint"
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "状态 | Status"
// @Param q query string false "关键词 | Keyword"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "结果列表 | Result list"
// @Router /raster_cog [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListRasterCOGs(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.rasterCOGTaskSvc.ListResults(c.Request.Context(), repository.RasterCOGFilter{
		TenantID:        tenantID,
		ItemID:          uint(itemID64),
		ItemFingerprint: c.Query("item_fingerprint"),
		TaskID:          uint(taskID64),
		Status:          c.Query("status"),
		Q:               c.Query("q"),
		Page:            page,
		PageSize:        pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results, "total": total, "page": page, "page_size": pageSize})
}

// GetRasterCOG GET /api/v1/manager/raster_cog/:id
// @Summary 获取栅格快显 COG 详情 | Get raster COG detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.RasterCOG "结果详情 | Result detail"
// @Router /raster_cog/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetRasterCOG(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.rasterCOGTaskSvc.GetResult(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "结果不存在"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteRasterCOG DELETE /api/v1/manager/raster_cog/:id
// @Summary 删除栅格快显 COG | Delete raster COG
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Router /raster_cog/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteRasterCOG(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.rasterCOGTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListQuickViewOptimizations GET /api/v1/manager/vector_quick_view_targets
// @Summary 列出快显性能优化结果 | List quick view optimization results
// @Tags Manager
// @Produce json
// @Param item_id query int false "数据项ID | Item ID"
// @Param item_fingerprint query string false "数据项指纹 | Item fingerprint"
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "状态 | Status"
// @Param q query string false "关键词 | Keyword"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "结果列表 | Result list"
// @Router /vector_quick_view_targets [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListQuickViewOptimizations(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.quickViewOptimizationTaskSvc.ListResults(c.Request.Context(), repository.QuickViewOptimizationFilter{
		TenantID:        tenantID,
		ItemID:          uint(itemID64),
		ItemFingerprint: c.Query("item_fingerprint"),
		TaskID:          uint(taskID64),
		Status:          c.Query("status"),
		Q:               c.Query("q"),
		Page:            page,
		PageSize:        pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results, "total": total, "page": page, "page_size": pageSize})
}

// GetQuickViewOptimization GET /api/v1/manager/vector_quick_view_targets/:id
// @Summary 获取快显性能优化结果详情 | Get quick view optimization result detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.QuickViewOptimization "结果详情 | Result detail"
// @Router /vector_quick_view_targets/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetQuickViewOptimization(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.quickViewOptimizationTaskSvc.GetResult(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "结果不存在"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteQuickViewOptimization DELETE /api/v1/manager/vector_quick_view_targets/:id
// @Summary 删除快显性能优化结果 | Delete quick view optimization result
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Router /vector_quick_view_targets/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteQuickViewOptimization(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.quickViewOptimizationTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "结果不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListTileCaches GET /api/v1/manager/vector_tile_cache
// @Summary 列出瓦片缓存结果 | List tile cache results
// @Description 查询瓦片缓存结果状态 | Query tile cache result states
// @Tags Manager
// @Produce json
// @Param item_id query int false "数据项ID | Item ID"
// @Param item_fingerprint query string false "数据项指纹 | Item fingerprint"
// @Param task_id query int false "任务ID | Task ID"
// @Param status query string false "状态 | Status"
// @Param q query string false "关键词 | Keyword"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "结果列表 | Result list"
// @Router /vector_tile_cache [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTileCaches(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.tileCacheTaskSvc.ListTileCache(c.Request.Context(), repository.TileCacheFilter{
		TenantID:        tenantID,
		ItemID:          uint(itemID64),
		ItemFingerprint: c.Query("item_fingerprint"),
		TaskID:          uint(taskID64),
		Status:          c.Query("status"),
		Q:               c.Query("q"),
		Page:            page,
		PageSize:        pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"data":      results,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTileCache GET /api/v1/manager/vector_tile_cache/:id
// @Summary 获取瓦片缓存结果详情 | Get tile cache result detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.TileCache "结果详情 | Result detail"
// @Router /vector_tile_cache/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetTileCache(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.tileCacheTaskSvc.GetTileCache(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "结果不存在"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteTileCache DELETE /api/v1/manager/vector_tile_cache/:id
// @Summary 删除瓦片缓存结果 | Delete tile cache result
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Router /vector_tile_cache/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteTileCache(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.tileCacheTaskSvc.DeleteTileCache(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

func decodeTileCacheTaskRequest(c *gin.Context) (TileCacheTaskRequest, error) {
	var req TileCacheTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func decodeQuickViewOptimizationTaskRequest(c *gin.Context) (QuickViewOptimizationTaskRequest, error) {
	var req QuickViewOptimizationTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func decodeRasterCOGTaskRequest(c *gin.Context) (RasterCOGTaskRequest, error) {
	var req RasterCOGTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func decodeRasterMosaicTaskRequest(c *gin.Context) (RasterMosaicTaskRequest, error) {
	var req RasterMosaicTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func decodeModel3DTilesTaskRequest(c *gin.Context) (Model3DTilesTaskRequest, error) {
	var req Model3DTilesTaskRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return req, errors.New("request body must contain a single JSON object")
	}
	if req.Config == nil {
		req.Config = commonModels.JSONMap{}
	}
	return req, nil
}

func tileCacheTaskResponse(task *models.TileCacheTask) TileCacheTaskResponse {
	resp := TileCacheTaskResponse{}
	if task == nil {
		return resp
	}
	resp = TileCacheTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeVectorTileCacheGeneration,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &TileCacheTaskTargetResponse{
			ItemID:          uintFromConfig(target["item_id"]),
			ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
			Locator:         stringFromConfig(target["locator"]),
			SourceEngineID:  uintFromConfig(target["source_engine_id"]),
			Schema:          stringFromConfig(target["schema"]),
			Table:           stringFromConfig(target["table"]),
		}
	}
	if tile, ok := asJSONMap(task.Config["tile"]); ok {
		options, _ := asJSONMap(task.Config["options"])
		resp.Tile = &TileCacheTaskTileResponse{
			Format:         stringFromConfig(tile["format"]),
			MinZoom:        intFromAPIConfig(tile["min_zoom"], 0),
			MaxZoom:        intFromAPIConfig(tile["max_zoom"], 0),
			TargetSRID:     intFromAPIConfig(tile["target_srid"], 0),
			GeometryColumn: stringFromConfig(options["geometry_column"]),
		}
	}
	return resp
}

func rasterCOGTaskResponse(task *models.RasterCOGTask) RasterCOGTaskResponse {
	resp := RasterCOGTaskResponse{}
	if task == nil {
		return resp
	}
	resp = RasterCOGTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeRasterCOGGeneration,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &RasterCOGTaskTargetResponse{
			ItemID:          uintFromConfig(target["item_id"]),
			ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
			Locator:         stringFromConfig(target["locator"]),
			SourceEngineID:  uintFromConfig(target["source_engine_id"]),
		}
	}
	if raster, ok := asJSONMap(task.Config["raster"]); ok {
		extent, _ := floatSliceFromAPIConfig(raster["extent"])
		resp.Raster = &RasterCOGTaskRasterResponse{
			SourceProfile:   stringFromConfig(raster["source_profile"]),
			SourceSizeBytes: int64FromAPIConfig(raster["source_size_bytes"], 0),
			Width:           int64FromAPIConfig(raster["width"], 0),
			Height:          int64FromAPIConfig(raster["height"], 0),
			BandCount:       int64FromAPIConfig(raster["band_count"], 0),
			SourceSRID:      intFromAPIConfig(raster["source_srid"], 0),
			Extent:          extent,
			ExtentSRID:      intFromAPIConfig(raster["extent_srid"], 0),
		}
	}
	if cog, ok := asJSONMap(task.Config["cog"]); ok {
		resp.COG = &RasterCOGTaskCOGResponse{
			Compression:        stringFromConfig(cog["compression"]),
			BlockSize:          intFromAPIConfig(cog["blocksize"], 0),
			OverviewResampling: stringFromConfig(cog["overview_resampling"]),
		}
	}
	return resp
}

func rasterMosaicTaskResponse(task *models.RasterMosaicTask) RasterMosaicTaskResponse {
	resp := RasterMosaicTaskResponse{}
	if task == nil {
		return resp
	}
	resp = RasterMosaicTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeRasterMosaicGeneration,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if source, ok := asJSONMap(task.Config["source"]); ok {
		resp.Source = &RasterMosaicTaskSourceResponse{
			NodeLocator:     stringFromConfig(source["node_locator"]),
			SourceEngineID:  uintFromConfig(source["source_engine_id"]),
			Recursive:       boolFromConfig(source["recursive"], true),
			IncludePatterns: stringSliceFromAPIConfig(source["include_patterns"]),
			ExcludePatterns: stringSliceFromAPIConfig(source["exclude_patterns"]),
		}
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &RasterMosaicTaskTargetResponse{
			StorageLocator: stringFromConfig(target["storage_locator"]),
			TargetEngineID: uintFromConfig(target["target_engine_id"]),
			DatasetName:    stringFromConfig(target["dataset_name"]),
		}
	}
	if placement, ok := asJSONMap(task.Config["placement"]); ok {
		resp.Placement = &RasterMosaicTaskPlacementResponse{
			Mode: stringFromConfig(placement["mode"]),
		}
	}
	return resp
}

func model3DTilesTaskResponse(task *models.Model3DTilesTask) Model3DTilesTaskResponse {
	resp := Model3DTilesTaskResponse{}
	if task == nil {
		return resp
	}
	resp = Model3DTilesTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeModel3DTilesGeneration,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if source, ok := asJSONMap(task.Config["source"]); ok {
		resp.Source = &Model3DTilesTaskSourceResponse{
			ItemLocator:    stringFromConfig(source["item_locator"]),
			SourceEngineID: uintFromConfig(source["source_engine_id"]),
			Format:         stringFromConfig(source["format"]),
		}
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &Model3DTilesTaskTargetResponse{
			StorageLocator: stringFromConfig(target["storage_locator"]),
			TargetEngineID: uintFromConfig(target["target_engine_id"]),
			DatasetName:    stringFromConfig(target["dataset_name"]),
		}
	}
	if tiles, ok := asJSONMap(task.Config["tiles"]); ok {
		resp.Tiles = &Model3DTilesTaskTilesResponse{
			Format: stringFromConfig(tiles["format"]),
		}
	}
	return resp
}

func quickViewOptimizationTaskResponse(task *models.QuickViewOptimizationTask) QuickViewOptimizationTaskResponse {
	resp := QuickViewOptimizationTaskResponse{}
	if task == nil {
		return resp
	}
	resp = QuickViewOptimizationTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeVectorQuickViewTargetGeneration,
		Name:                task.Name,
		Description:         task.Description,
		Enabled:             task.Enabled,
		Schedule:            task.Schedule,
		NextRunAt:           task.NextRunAt,
		LastRunAt:           task.LastRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		Config:              task.Config,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
	if target, ok := asJSONMap(task.Config["target"]); ok {
		resp.Target = &QuickViewOptimizationTaskTargetResponse{
			ItemID:          uintFromConfig(target["item_id"]),
			ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
			Locator:         stringFromConfig(target["locator"]),
			SourceEngineID:  uintFromConfig(target["source_engine_id"]),
			Schema:          stringFromConfig(target["schema"]),
			Table:           stringFromConfig(target["table"]),
		}
	}
	if geometry, ok := asJSONMap(task.Config["geometry"]); ok {
		resp.Geometry = &QuickViewOptimizationTaskGeometryResponse{
			GeometryColumn: stringFromConfig(geometry["geometry_column"]),
			SourceSRID:     intFromAPIConfig(geometry["source_srid"], 0),
			TargetSRID:     intFromAPIConfig(geometry["target_srid"], 0),
		}
	}
	return resp
}

func intFromAPIConfig(value interface{}, defaultValue int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint:
		return int(v)
	case float64:
		return int(v)
	}
	return defaultValue
}

func int64FromAPIConfig(value interface{}, defaultValue int64) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case float64:
		return int64(v)
	}
	return defaultValue
}

func stringSliceFromAPIConfig(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				if trimmed := strings.TrimSpace(text); trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func floatSliceFromAPIConfig(value interface{}) ([]float64, bool) {
	switch v := value.(type) {
	case []float64:
		return v, true
	case []interface{}:
		out := make([]float64, 0, len(v))
		for _, item := range v {
			switch n := item.(type) {
			case int:
				out = append(out, float64(n))
			case int64:
				out = append(out, float64(n))
			case uint:
				out = append(out, float64(n))
			case float64:
				out = append(out, n)
			default:
				return nil, false
			}
		}
		return out, true
	default:
		return nil, false
	}
}
