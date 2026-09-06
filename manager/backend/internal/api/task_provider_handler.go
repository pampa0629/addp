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
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	manageri18n "github.com/addp/manager/i18n"
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
	embeddingTaskSvc              *service.EmbeddingTaskService
	tileCacheTaskSvc              *service.TileCacheTaskService
	vectorTileSetTaskSvc          *service.VectorTileSetTaskService
	vectorMaterializedViewTaskSvc *service.VectorMaterializedViewTaskService
	rasterCOGTaskSvc              *service.RasterCOGTaskService
	rasterMosaicTaskSvc           *service.RasterMosaicTaskService
	model3DGLBTaskSvc             *service.Model3DGLBTaskService
	model3DTilesTaskSvc           *service.Model3DTilesTaskService
	gaussianSplatKSplatTaskSvc    *service.GaussianSplatKSplatTaskService
	pointCloudCOPCTaskSvc         *service.PointCloudCOPCTaskService
	pptxPDFTaskSvc                *service.PPTXPDFTaskService
	taskExecRepo                  *commonExecution.TaskExecutionRepository
}

// NewTaskProviderHandler 创建处理器
func NewTaskProviderHandler(
	embeddingTaskSvc *service.EmbeddingTaskService,
	tileCacheTaskSvc *service.TileCacheTaskService,
	vectorMaterializedViewTaskSvc *service.VectorMaterializedViewTaskService,
	rasterCOGTaskSvc *service.RasterCOGTaskService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	rasterMosaicTaskSvc ...*service.RasterMosaicTaskService,
) *TaskProviderHandler {
	handler := &TaskProviderHandler{
		embeddingTaskSvc:              embeddingTaskSvc,
		tileCacheTaskSvc:              tileCacheTaskSvc,
		vectorMaterializedViewTaskSvc: vectorMaterializedViewTaskSvc,
		rasterCOGTaskSvc:              rasterCOGTaskSvc,
		taskExecRepo:                  taskExecRepo,
	}
	if len(rasterMosaicTaskSvc) > 0 {
		handler.rasterMosaicTaskSvc = rasterMosaicTaskSvc[0]
	}
	return handler
}

func (h *TaskProviderHandler) SetModel3DTilesTaskService(model3DTilesTaskSvc *service.Model3DTilesTaskService) {
	h.model3DTilesTaskSvc = model3DTilesTaskSvc
}

func (h *TaskProviderHandler) SetVectorTileSetTaskService(vectorTileSetTaskSvc *service.VectorTileSetTaskService) {
	h.vectorTileSetTaskSvc = vectorTileSetTaskSvc
}

func (h *TaskProviderHandler) SetModel3DGLBTaskService(model3DGLBTaskSvc *service.Model3DGLBTaskService) {
	h.model3DGLBTaskSvc = model3DGLBTaskSvc
}

func (h *TaskProviderHandler) SetGaussianSplatKSplatTaskService(gaussianSplatKSplatTaskSvc *service.GaussianSplatKSplatTaskService) {
	h.gaussianSplatKSplatTaskSvc = gaussianSplatKSplatTaskSvc
}

func (h *TaskProviderHandler) SetPointCloudCOPCTaskService(pointCloudCOPCTaskSvc *service.PointCloudCOPCTaskService) {
	h.pointCloudCOPCTaskSvc = pointCloudCOPCTaskSvc
}

func (h *TaskProviderHandler) SetPPTXPDFTaskService(pptxPDFTaskSvc *service.PPTXPDFTaskService) {
	h.pptxPDFTaskSvc = pptxPDFTaskSvc
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

// TaskProviderTaskDetailResponse documents the fields shared by every Manager
// TaskProvider detail response. Owner-specific task fields remain at the same
// top level in the actual response.
type TaskProviderTaskDetailResponse struct {
	ID                uint                           `json:"id"`
	TenantID          uint                           `json:"tenant_id"`
	TaskType          string                         `json:"task_type"`
	Name              string                         `json:"name"`
	ExecutionContract taskprovider.ExecutionContract `json:"execution_contract"`
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
	Config      commonModels.JSONMap `json:"config"`
}

type TileCacheTaskTargetResponse struct {
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	Locator         string `json:"locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	FullName        string `json:"full_name,omitempty"`
	Schema          string `json:"schema,omitempty"`
	Table           string `json:"table,omitempty"`
}

type TileCacheTaskTileResponse struct {
	ArchiveFormat  string `json:"archive_format"`
	TileType       string `json:"tile_type"`
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

type VectorMaterializedViewTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type VectorMaterializedViewTaskTargetResponse struct {
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	Locator         string `json:"locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	Schema          string `json:"schema,omitempty"`
	Table           string `json:"table,omitempty"`
}

type VectorMaterializedViewTaskGeometryResponse struct {
	GeometryColumn string `json:"geometry_column,omitempty"`
	SourceSRID     int    `json:"source_srid,omitempty"`
	TargetSRID     int    `json:"target_srid,omitempty"`
}

type VectorMaterializedViewTaskResponse struct {
	ID                  uint                                        `json:"id"`
	TenantID            uint                                        `json:"tenant_id"`
	TaskType            string                                      `json:"task_type"`
	Name                string                                      `json:"name"`
	Description         string                                      `json:"description,omitempty"`
	Enabled             bool                                        `json:"enabled"`
	Schedule            string                                      `json:"schedule,omitempty"`
	NextRunAt           *time.Time                                  `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                                  `json:"last_run_at,omitempty"`
	LastExecutionID     *string                                     `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                                     `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                                       `json:"created_by,omitempty"`
	Config              commonModels.JSONMap                        `json:"config"`
	Target              *VectorMaterializedViewTaskTargetResponse   `json:"target,omitempty"`
	Geometry            *VectorMaterializedViewTaskGeometryResponse `json:"geometry,omitempty"`
	CreatedAt           time.Time                                   `json:"created_at"`
	UpdatedAt           time.Time                                   `json:"updated_at"`
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

type Model3DTilesTaskSourceResponse struct {
	ItemLocator     string `json:"item_locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	ItemID          uint   `json:"item_id,omitempty"`
	Format          string `json:"format,omitempty"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`
}

type Model3DTilesTaskResultResponse struct {
	StorageRef string `json:"storage_ref,omitempty"`
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
	TargetFormat        string                          `json:"target_format,omitempty"`
	Result              *Model3DTilesTaskResultResponse `json:"result,omitempty"`
	CreatedAt           time.Time                       `json:"created_at"`
	UpdatedAt           time.Time                       `json:"updated_at"`
}

type Model3DGLBTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type Model3DGLBTaskSourceResponse struct {
	ItemLocator     string `json:"item_locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	ItemID          uint   `json:"item_id,omitempty"`
	Format          string `json:"format,omitempty"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`
}

type Model3DGLBTaskResultResponse struct {
	StorageRef string `json:"storage_ref,omitempty"`
	FileName   string `json:"file_name,omitempty"`
}

type Model3DGLBTaskResponse struct {
	ID                  uint                          `json:"id"`
	TenantID            uint                          `json:"tenant_id"`
	TaskType            string                        `json:"task_type"`
	Name                string                        `json:"name"`
	Description         string                        `json:"description,omitempty"`
	Enabled             bool                          `json:"enabled"`
	Schedule            string                        `json:"schedule,omitempty"`
	NextRunAt           *time.Time                    `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                    `json:"last_run_at,omitempty"`
	LastExecutionID     *string                       `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                       `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                         `json:"created_by,omitempty"`
	Config              commonModels.JSONMap          `json:"config"`
	Source              *Model3DGLBTaskSourceResponse `json:"source,omitempty"`
	Result              *Model3DGLBTaskResultResponse `json:"result,omitempty"`
	CreatedAt           time.Time                     `json:"created_at"`
	UpdatedAt           time.Time                     `json:"updated_at"`
}

type GaussianSplatKSplatTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type GaussianSplatKSplatTaskSourceResponse struct {
	ItemLocator              string               `json:"item_locator,omitempty"`
	SourceEngineID           uint                 `json:"source_engine_id,omitempty"`
	ItemFingerprint          string               `json:"item_fingerprint,omitempty"`
	ItemID                   uint                 `json:"item_id,omitempty"`
	Format                   string               `json:"format,omitempty"`
	SourceSizeBytes          int64                `json:"source_size_bytes,omitempty"`
	Bounds3D                 commonModels.JSONMap `json:"bounds_3d,omitempty"`
	SampledBounds3D          commonModels.JSONMap `json:"sampled_bounds_3d,omitempty"`
	SampledBoundsSampleCount *int64               `json:"sampled_bounds_sample_count,omitempty"`
}

type GaussianSplatKSplatTaskResultResponse struct {
	StorageRef string `json:"storage_ref,omitempty"`
	FileName   string `json:"file_name,omitempty"`
}

type GaussianSplatKSplatTaskResponse struct {
	ID                  uint                                   `json:"id"`
	TenantID            uint                                   `json:"tenant_id"`
	TaskType            string                                 `json:"task_type"`
	Name                string                                 `json:"name"`
	Description         string                                 `json:"description,omitempty"`
	Enabled             bool                                   `json:"enabled"`
	Schedule            string                                 `json:"schedule,omitempty"`
	NextRunAt           *time.Time                             `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                             `json:"last_run_at,omitempty"`
	LastExecutionID     *string                                `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                                `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                                  `json:"created_by,omitempty"`
	Config              commonModels.JSONMap                   `json:"config"`
	Source              *GaussianSplatKSplatTaskSourceResponse `json:"source,omitempty"`
	Result              *GaussianSplatKSplatTaskResultResponse `json:"result,omitempty"`
	CreatedAt           time.Time                              `json:"created_at"`
	UpdatedAt           time.Time                              `json:"updated_at"`
}

type PointCloudCOPCTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Schedule    string               `json:"schedule,omitempty"`
	NextRunAt   *time.Time           `json:"next_run_at,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type PointCloudCOPCTaskSourceResponse struct {
	ItemLocator     string `json:"item_locator,omitempty"`
	SourceEngineID  uint   `json:"source_engine_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	ItemID          uint   `json:"item_id,omitempty"`
	Format          string `json:"format,omitempty"`
	SourceSizeBytes int64  `json:"source_size_bytes,omitempty"`
}

type PointCloudCOPCTaskResultResponse struct {
	StorageRef string `json:"storage_ref,omitempty"`
	FileName   string `json:"file_name,omitempty"`
}

type PointCloudCOPCTaskResponse struct {
	ID                  uint                              `json:"id"`
	TenantID            uint                              `json:"tenant_id"`
	TaskType            string                            `json:"task_type"`
	Name                string                            `json:"name"`
	Description         string                            `json:"description,omitempty"`
	Enabled             bool                              `json:"enabled"`
	Schedule            string                            `json:"schedule,omitempty"`
	NextRunAt           *time.Time                        `json:"next_run_at,omitempty"`
	LastRunAt           *time.Time                        `json:"last_run_at,omitempty"`
	LastExecutionID     *string                           `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                           `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                             `json:"created_by,omitempty"`
	Config              commonModels.JSONMap              `json:"config"`
	Source              *PointCloudCOPCTaskSourceResponse `json:"source,omitempty"`
	Result              *PointCloudCOPCTaskResultResponse `json:"result,omitempty"`
	CreatedAt           time.Time                         `json:"created_at"`
	UpdatedAt           time.Time                         `json:"updated_at"`
}

// ListTasks GET /api/v1/manager/tasks
// 查询参数：?task_type=vector_tile_cache_generation|vector_tile_set_generation|vector_materialized_view_generation|raster_cog_generation|raster_mosaic_generation|model_3d_glb_generation|model3d_tiles_generation|gaussian_splat_ksplat_generation|point_cloud_copc_generation|embedding
// @Summary 列出任务 | List tasks
// @Description 列出 Manager 模块的任务（矢量瓦片缓存生成、矢量物化视图、栅格快显 COG、栅格 mosaic、三维模型与点云快显、向量化任务）| List Manager module tasks
// @Tags Manager
// @Produce json
// @Param task_type query string false "任务类型过滤：vector_tile_cache_generation|vector_tile_set_generation|vector_materialized_view_generation|raster_cog_generation|raster_mosaic_generation|model_3d_glb_generation|model3d_tiles_generation|gaussian_splat_ksplat_generation|point_cloud_copc_generation|embedding | Task type filter"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} TaskListResponse "任务列表 | Task list"
// @Failure 400 {object} map[string]interface{} "不支持的任务类型 | Unsupported task type"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTasks(c *gin.Context) {
	h.listTasks(c, strings.TrimSpace(c.Query("task_type")))
}

func (h *TaskProviderHandler) listTasks(c *gin.Context, taskType string) {
	tenantID := tenantIDValue(c)

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
	case commonExecution.TaskTypeVectorTileSetGeneration:
		if h.vectorTileSetTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "vector tile set task service is unavailable"})
			return
		}
		tasks, t, err := h.vectorTileSetTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorTileSetGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
		}
	case commonExecution.TaskTypeVectorMaterializedViewGeneration:
		if h.vectorMaterializedViewTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "vector materialized view task service is unavailable"})
			return
		}
		tasks, t, err := h.vectorMaterializedViewTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorMaterializedViewGeneration,
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
	case commonExecution.TaskTypeModel3DGLBGeneration:
		if h.model3DGLBTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "model 3d GLB generation task service is unavailable"})
			return
		}
		tasks, t, err := h.model3DGLBTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeModel3DGLBGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypeGaussianSplatKSplatGeneration:
		if h.gaussianSplatKSplatTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "gaussian splat KSplat generation task service is unavailable"})
			return
		}
		tasks, t, err := h.gaussianSplatKSplatTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeGaussianSplatKSplatGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypePointCloudCOPCGeneration:
		if h.pointCloudCOPCTaskSvc == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "point cloud COPC generation task service is unavailable"})
			return
		}
		tasks, t, err := h.pointCloudCOPCTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{
				ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypePointCloudCOPCGeneration,
				Name: task.Name, Description: task.Description, Enabled: task.Enabled,
				LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus,
			})
		}
	case commonExecution.TaskTypePPTXPDFGeneration:
		if h.pptxPDFTaskSvc == nil {
			managerError(c, http.StatusServiceUnavailable, manageri18n.MsgPPTXPDFServiceUnavailable)
			return
		}
		tasks, t, err := h.pptxPDFTaskSvc.List(ctx, tenantID, page, pageSize)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		total = t
		for _, task := range tasks {
			items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypePPTXPDFGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
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
		if h.vectorTileSetTaskSvc != nil {
			tasks, t, err := h.vectorTileSetTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorTileSetGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.vectorMaterializedViewTaskSvc != nil {
			tasks, t, err := h.vectorMaterializedViewTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeVectorMaterializedViewGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
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
		if h.model3DGLBTaskSvc != nil {
			tasks, t, err := h.model3DGLBTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeModel3DGLBGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.gaussianSplatKSplatTaskSvc != nil {
			tasks, t, err := h.gaussianSplatKSplatTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeGaussianSplatKSplatGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.pointCloudCOPCTaskSvc != nil {
			tasks, t, err := h.pointCloudCOPCTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypePointCloudCOPCGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
			}
		}
		if h.pptxPDFTaskSvc != nil {
			tasks, t, err := h.pptxPDFTaskSvc.List(ctx, tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			total += t
			for _, task := range tasks {
				items = append(items, TaskListItem{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypePPTXPDFGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus})
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_tile_cache_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTileCacheTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /embedding_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListEmbeddingTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @Param task_type path string true "任务类型：vector_tile_cache_generation|vector_tile_set_generation|vector_materialized_view_generation|raster_cog_generation|raster_mosaic_generation|model_3d_glb_generation|model3d_tiles_generation|gaussian_splat_ksplat_generation|point_cloud_copc_generation|embedding | Task type"
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} TaskProviderTaskDetailResponse "任务详情与具体任务执行契约；任务类型专有字段位于同一顶层 | Task detail and concrete execution contract; task-type-specific fields remain at the same top level"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /tasks/{task_type}/{id} [get]
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /embedding_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskDetail(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
		respondManagerTaskDetail(c, taskType, tileCacheTaskResponse(task))
	case commonExecution.TaskTypeVectorTileSetGeneration:
		task, err := h.vectorTileSetTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		respondManagerTaskDetail(c, taskType, task)
	case commonExecution.TaskTypeVectorMaterializedViewGeneration:
		task, err := h.vectorMaterializedViewTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		respondManagerTaskDetail(c, taskType, vectorMaterializedViewTaskResponse(task))
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
		respondManagerTaskDetail(c, taskType, rasterCOGTaskResponse(task))
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
		respondManagerTaskDetail(c, taskType, rasterMosaicTaskResponse(task))
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
		respondManagerTaskDetail(c, taskType, model3DTilesTaskResponse(task))
	case commonExecution.TaskTypeModel3DGLBGeneration:
		task, err := h.model3DGLBTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		respondManagerTaskDetail(c, taskType, model3DGLBTaskResponse(task))
	case commonExecution.TaskTypeGaussianSplatKSplatGeneration:
		task, err := h.gaussianSplatKSplatTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		respondManagerTaskDetail(c, taskType, gaussianSplatKSplatTaskResponse(task))
	case commonExecution.TaskTypePointCloudCOPCGeneration:
		task, err := h.pointCloudCOPCTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		respondManagerTaskDetail(c, taskType, pointCloudCOPCTaskResponse(task))
	case commonExecution.TaskTypePPTXPDFGeneration:
		task, err := h.pptxPDFTaskSvc.GetByID(ctx, uint(id), tenantID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if task == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
			return
		}
		respondManagerTaskDetail(c, taskType, task)
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
		respondManagerTaskDetail(c, taskType, embeddingTaskResponse(task))
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的任务类型: " + taskType})
	}
}

func respondManagerTaskDetail(c *gin.Context, taskType string, task interface{}) {
	encoded, err := json.Marshal(task)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务详情序列化失败"})
		return
	}
	result := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "任务详情序列化失败"})
		return
	}
	result["execution_contract"] = managerTaskExecutionContract(taskType)
	c.JSON(http.StatusOK, result)
}

func managerTaskExecutionContract(taskType string) taskprovider.ExecutionContract {
	contract := taskprovider.EmptyExecutionContract()
	if !managerTaskRequiresExistingResultAction(taskType) {
		return contract
	}
	contract.InputSchema = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"existing_result_action": map[string]interface{}{
				"type": "string", "title": "已有结果处理", "enum": []interface{}{existingResultActionOverwrite},
			},
		},
		"additionalProperties": false,
	}
	contract.InputUISchema = map[string]interface{}{
		"existing_result_action": map[string]interface{}{"control": "select"},
	}
	return contract
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_tile_cache_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetTileCacheTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
	Parameters        map[string]interface{} `json:"parameters"`          // Manager 受管当前结果任务仅支持 existing_result_action=overwrite | Managed current-result tasks only support existing_result_action=overwrite
}

type TaskExecuteResponse struct {
	Status      string `json:"status" enums:"pending,running" example:"pending"`
	ExecutionID string `json:"execution_id"`
}

const (
	existingResultActionOverwrite    = "overwrite"
	existingResultActionRequiredCode = "existing_result_action_required"
)

func managerResultExecutionOverwrite(parameters map[string]interface{}) (bool, error) {
	if len(parameters) == 0 {
		return false, nil
	}
	if len(parameters) != 1 {
		return false, errors.New("Manager result execution parameters only support existing_result_action=overwrite")
	}
	value, ok := parameters["existing_result_action"]
	if !ok {
		return false, errors.New("Manager result execution parameters only support existing_result_action=overwrite")
	}
	action, ok := value.(string)
	if !ok {
		return false, errors.New("existing_result_action must be a string")
	}
	return existingResultActionAllowsOverwrite(action)
}

func existingResultActionAllowsOverwrite(action string) (bool, error) {
	action = strings.TrimSpace(action)
	if action == "" {
		return false, nil
	}
	if action != existingResultActionOverwrite {
		return false, errors.New("existing_result_action must be overwrite")
	}
	return true, nil
}

// TaskExecute POST /api/v1/manager/tasks/:task_type/:id/execute
// @Summary 执行任务 | Execute task
// @Description 触发指定任务立即执行 | Trigger immediate execution of a specific task
// @Tags Manager
// @Accept json
// @Produce json
// @Param task_type path string true "任务类型：vector_tile_cache_generation|vector_tile_set_generation|vector_materialized_view_generation|raster_cog_generation|raster_mosaic_generation|model_3d_glb_generation|model3d_tiles_generation|gaussian_splat_ksplat_generation|point_cloud_copc_generation|embedding | Task type"
// @Param id path int true "任务ID | Task ID"
// @Param body body TaskExecuteRequest false "执行配置 | Execution configuration"
// @Success 202 {object} TaskExecuteResponse "执行ID | Execution ID"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @Failure 409 {object} map[string]interface{} "任务已有活动执行或缺少已有结果动作 | Active execution or existing result action required"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /tasks/{task_type}/{id}/execute [post]
// @Security BearerAuth
func (h *TaskProviderHandler) TaskExecute(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
	overwriteExistingResult := false
	if managerTaskRequiresExistingResultAction(taskType) {
		overwriteExistingResult, err = managerResultExecutionOverwrite(req.Parameters)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	} else if len(req.Parameters) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Manager task provider does not support execution parameter overrides"})
		return
	}
	var parentExecID *string
	if req.ParentExecutionID != "" {
		parentExecID = &req.ParentExecutionID
	}

	ctx := c.Request.Context()
	var executionID string
	executionStatus := commonExecution.ExecutionStatusRunning

	switch taskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		executionID, err = h.tileCacheTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeVectorTileSetGeneration:
		executionID, err = h.vectorTileSetTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeVectorMaterializedViewGeneration:
		executionID, err = h.vectorMaterializedViewTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeRasterCOGGeneration:
		executionID, err = h.rasterCOGTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeRasterMosaicGeneration:
		executionID, err = h.rasterMosaicTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeModel3DTilesGeneration:
		executionID, err = h.model3DTilesTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeModel3DGLBGeneration:
		executionID, err = h.model3DGLBTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypeGaussianSplatKSplatGeneration:
		executionID, err = h.gaussianSplatKSplatTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypePointCloudCOPCGeneration:
		executionID, err = h.pointCloudCOPCTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
	case commonExecution.TaskTypePPTXPDFGeneration:
		executionID, err = h.pptxPDFTaskSvc.Execute(ctx, uint(id), tenantID, triggerType, source, parentExecID, overwriteExistingResult)
		executionStatus = commonExecution.ExecutionStatusPending
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
		if errors.Is(err, service.ErrTaskExecutionBusy) {
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, manageri18n.MsgTaskExecutionBusy)})
			return
		}
		if errors.Is(err, service.ErrModel3DTilesTaskExecutionBusy) {
			c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, manageri18n.MsgModel3DTilesExecutionBusy)})
			return
		}
		if errors.Is(err, service.ErrExistingResultActionRequired) {
			c.JSON(http.StatusConflict, gin.H{
				"error": commoni18n.T(c, manageri18n.MsgExistingResultActionRequired),
				"code":  existingResultActionRequiredCode,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, TaskExecuteResponse{
		Status:      executionStatus,
		ExecutionID: executionID,
	})
}

func managerTaskRequiresExistingResultAction(taskType string) bool {
	switch taskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration,
		commonExecution.TaskTypeVectorMaterializedViewGeneration,
		commonExecution.TaskTypeRasterCOGGeneration,
		commonExecution.TaskTypeModel3DGLBGeneration,
		commonExecution.TaskTypeModel3DTilesGeneration,
		commonExecution.TaskTypeGaussianSplatKSplatGeneration,
		commonExecution.TaskTypePointCloudCOPCGeneration,
		commonExecution.TaskTypePPTXPDFGeneration:
		return true
	default:
		return false
	}
}

// ExecutionStatus GET /api/v1/manager/executions/:execution_id
// @Summary 获取执行状态 | Get execution status
// @Description 获取任务执行记录的状态信息 | Get status information of a task execution record
// @Tags Manager
// @Produce json
// @Param execution_id path string true "执行ID | Execution ID"
// @Success 200 {object} execution.TaskExecution "执行状态 | Execution status"
// @Failure 404 {object} map[string]interface{} "执行记录不存在 | Execution not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /executions/{execution_id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ExecutionStatus(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /embedding_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateEmbeddingTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /embedding_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateEmbeddingTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /embedding_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteEmbeddingTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /vector_tile_cache_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateTileCacheTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /vector_tile_cache_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateTileCacheTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
	existing.Schedule = ""
	existing.NextRunAt = nil
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /vector_tile_cache_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteTileCacheTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

// ListVectorTileSetTasks lists business PMTiles generation task definitions.
// @Summary 获取业务矢量瓦片集任务 | List business vector tile set tasks
// @Tags Manager
// @Produce json
// @Success 200 {object} TaskListResponse "任务列表 | Task list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_tile_set_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListVectorTileSetTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.vectorTileSetTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": tasks, "total": total, "page": page, "page_size": pageSize})
}

// CreateVectorTileSetTask creates a business PMTiles generation task.
// @Summary 创建业务矢量瓦片集任务 | Create business vector tile set task
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body TileCacheTaskRequest true "任务配置 | Task configuration"
// @Success 201 {object} models.VectorTileSetTask "任务 | Task"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /vector_tile_set_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateVectorTileSetTask(c *gin.Context) {
	var req TileCacheTaskRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := &models.VectorTileSetTask{TenantID: tenantIDValue(c), Name: req.Name, Description: req.Description, Enabled: enabled, Config: req.Config}
	if userID := userIDValue(c); userID > 0 {
		task.CreatedBy = &userID
	}
	if err := h.vectorTileSetTaskSvc.Create(c.Request.Context(), task); err != nil {
		c.JSON(commonapi.MapErrorToHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, task)
}

// GetVectorTileSetTask returns a business PMTiles generation task.
// @Summary 获取业务矢量瓦片集任务 | Get business vector tile set task
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} models.VectorTileSetTask "任务 | Task"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_tile_set_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetVectorTileSetTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	task, err := h.vectorTileSetTaskSvc.GetByID(c.Request.Context(), uint(id), tenantIDValue(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, task)
}

// UpdateVectorTileSetTask updates a business PMTiles generation task.
// @Summary 更新业务矢量瓦片集任务 | Update business vector tile set task
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body TileCacheTaskRequest true "任务配置 | Task configuration"
// @Success 200 {object} models.VectorTileSetTask "任务 | Task"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /vector_tile_set_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateVectorTileSetTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	task, err := h.vectorTileSetTaskSvc.GetByID(c.Request.Context(), uint(id), tenantIDValue(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	var req TileCacheTaskRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	task.Name, task.Description, task.Config = req.Name, req.Description, req.Config
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}
	if err := h.vectorTileSetTaskSvc.Update(c.Request.Context(), task); err != nil {
		c.JSON(commonapi.MapErrorToHTTPStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, task)
}

// DeleteVectorTileSetTask deletes a business PMTiles generation task.
// @Summary 删除业务矢量瓦片集任务 | Delete business vector tile set task
// @Tags Manager
// @Param id path int true "任务ID | Task ID"
// @Success 204 "删除成功 | Deleted"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /vector_tile_set_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteVectorTileSetTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.vectorTileSetTaskSvc.Delete(c.Request.Context(), uint(id), tenantIDValue(c)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// ListVectorMaterializedViewTasks GET /api/v1/manager/vector_materialized_view_tasks
// @Summary 列出矢量物化视图任务配置 | List vector materialized view task configurations
// @Description 列出 Manager 模块的矢量物化视图任务配置。该私有入口固定返回 task_type=vector_materialized_view_generation；编排模块应使用标准 /tasks 入口。| List Manager vector materialized view task configurations. This private endpoint always returns task_type=vector_materialized_view_generation; orchestrator should use the standard /tasks endpoint.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_materialized_view_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListVectorMaterializedViewTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.vectorMaterializedViewTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]VectorMaterializedViewTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, vectorMaterializedViewTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateVectorMaterializedViewTask POST /api/v1/manager/vector_materialized_view_tasks
// @Summary 创建矢量物化视图任务配置 | Create vector materialized view task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body VectorMaterializedViewTaskRequest true "矢量物化视图任务配置 | Vector materialized view task configuration"
// @Success 201 {object} VectorMaterializedViewTaskResponse "创建的任务配置 | Created task configuration"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /vector_materialized_view_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateVectorMaterializedViewTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	req, err := decodeVectorMaterializedViewTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.VectorMaterializedViewTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.vectorMaterializedViewTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, vectorMaterializedViewTaskResponse(&task))
}

// GetVectorMaterializedViewTask GET /api/v1/manager/vector_materialized_view_tasks/:id
// @Summary 获取矢量物化视图任务配置 | Get vector materialized view task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} VectorMaterializedViewTaskResponse "任务配置 | Task configuration"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_materialized_view_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetVectorMaterializedViewTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeVectorMaterializedViewGeneration})
	h.TaskDetail(c)
}

// UpdateVectorMaterializedViewTask PUT /api/v1/manager/vector_materialized_view_tasks/:id
// @Summary 更新矢量物化视图任务配置 | Update vector materialized view task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body VectorMaterializedViewTaskRequest true "矢量物化视图任务配置 | Vector materialized view task configuration"
// @Success 200 {object} VectorMaterializedViewTaskResponse "更新后的任务配置 | Updated task configuration"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /vector_materialized_view_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateVectorMaterializedViewTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.vectorMaterializedViewTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeVectorMaterializedViewTaskRequest(c)
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
	if err := h.vectorMaterializedViewTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, vectorMaterializedViewTaskResponse(existing))
}

// DeleteVectorMaterializedViewTask DELETE /api/v1/manager/vector_materialized_view_tasks/:id
// @Summary 删除矢量物化视图任务配置 | Delete vector materialized view task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /vector_materialized_view_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteVectorMaterializedViewTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.vectorMaterializedViewTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /raster_cog_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListRasterCOGTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

// GetRasterCOGTask GET /api/v1/manager/raster_cog_tasks/:id
// @Summary 获取栅格快显 COG 任务配置 | Get raster COG generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} RasterCOGTaskResponse "任务配置 | Task configuration"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /raster_cog_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetRasterCOGTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeRasterCOGGeneration})
	h.TaskDetail(c)
}

// DeleteRasterCOGTask DELETE /api/v1/manager/raster_cog_tasks/:id
// @Summary 删除栅格快显 COG 任务配置 | Delete raster COG generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /raster_cog_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteRasterCOGTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /raster_mosaic_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListRasterMosaicTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /raster_mosaic_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateRasterMosaicTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /raster_mosaic_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateRasterMosaicTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /raster_mosaic_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteRasterMosaicTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

// ListModel3DTilesTasks GET /api/v1/manager/model3d_tiles_tasks
// @Summary 列出三维模型 3D Tiles 任务配置 | List model 3D Tiles generation task configurations
// @Description 列出 Manager 模块的分块三维模型瓦片任务配置，target_format 区分 3D Tiles 与 S3M。该私有入口固定返回 task_type=model3d_tiles_generation。| List Manager model3d tiles generation task configurations for 3D Tiles and S3M.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model3d_tiles_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListModel3DTilesTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

// GetModel3DTilesTask GET /api/v1/manager/model3d_tiles_tasks/:id
// @Summary 获取三维模型 3D Tiles 任务配置 | Get model 3D Tiles generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} Model3DTilesTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model3d_tiles_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetModel3DTilesTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeModel3DTilesGeneration})
	h.TaskDetail(c)
}

// DeleteModel3DTilesTask DELETE /api/v1/manager/model3d_tiles_tasks/:id
// @Summary 删除三维模型 3D Tiles 任务配置 | Delete model 3D Tiles generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /model3d_tiles_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteModel3DTilesTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

// ListModel3DTilesResults GET /api/v1/manager/model3d_tiles
// @Summary 列出分块三维模型瓦片快显结果 | List model3d tiles quick view results
// @Description 列出 Manager infra MinIO 中受管的 3D Tiles 与 S3M 快显结果。| List Manager-owned 3D Tiles and S3M quick view results stored in infra MinIO.
// @Tags Manager
// @Produce json
// @Param item_id query int false "数据项ID | Item ID"
// @Param item_fingerprint query string false "数据项指纹 | Item fingerprint"
// @Param task_id query int false "任务ID | Task ID"
// @Param target_format query string false "目标格式：3d_tiles 或 s3m | Target format: 3d_tiles or s3m"
// @Param status query string false "状态 | Status"
// @Param q query string false "关键词 | Keyword"
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "结果列表 | Result list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model3d_tiles [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListModel3DTilesResults(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.model3DTilesTaskSvc.ListResults(c.Request.Context(), repository.Model3DTilesFilter{
		TenantID: tenantID, ItemID: uint(itemID64), ItemFingerprint: c.Query("item_fingerprint"),
		TaskID: uint(taskID64), TargetFormat: c.Query("target_format"), Status: c.Query("status"), Q: c.Query("q"),
		Page: page, PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": results, "total": total, "page": page, "page_size": pageSize})
}

// DeleteModel3DTilesResult DELETE /api/v1/manager/model3d_tiles/:id
// @Summary 删除分块三维模型瓦片快显结果 | Delete model3d tiles quick view result
// @Description 删除 Manager infra MinIO 中的受管 3D Tiles 或 S3M 瓦片，并软删除对应结果记录；不删除源 item、任务定义或 execution 历史。| Delete managed 3D Tiles or S3M assets from Manager infra MinIO and soft-delete the result record without deleting the source item, task definition, or execution history.
// @Tags Manager
// @Produce json
// @Param id path int true "结果 ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "结果不存在 | Result not found"
// @Failure 500 {object} map[string]interface{} "删除失败 | Delete failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /model3d_tiles/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteModel3DTilesResult(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidModel3DTilesResultID)
		return
	}
	if err := h.model3DTilesTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
		if errors.Is(err, service.ErrModel3DTilesResultNotFound) {
			managerError(c, http.StatusNotFound, manageri18n.MsgModel3DTilesResultNotFound)
			return
		}
		managerErrorWithDetail(c, http.StatusInternalServerError, manageri18n.MsgDeleteModel3DTilesFailed, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, manageri18n.MsgModel3DTilesResultDeleted)})
}

// ListModel3DGLBTasks GET /api/v1/manager/model_3d_glb_tasks
// @Summary 列出三维模型 GLB 快显任务配置 | List model 3D GLB generation task configurations
// @Description 列出 Manager 模块的 OSGB / glTF / FBX / OBJ / STL / IFC 转 GLB 快显任务配置。该私有入口固定返回 task_type=model_3d_glb_generation；编排模块应使用标准 /tasks 入口。| List Manager model 3D GLB generation task configurations.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model_3d_glb_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListModel3DGLBTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.model3DGLBTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]Model3DGLBTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, model3DGLBTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateModel3DGLBTask POST /api/v1/manager/model_3d_glb_tasks
// @Summary 创建三维模型 GLB 快显任务配置 | Create model 3D GLB generation task configuration
// @Description 创建新的 OSGB / glTF / FBX / OBJ / STL / IFC 转 GLB 快显任务配置。任务从 OSGB、glTF、FBX、OBJ、STL 或 IFC model_3d item 读取源数据，并将 GLB artifact 写入 Manager infra MinIO。| Create a model 3D GLB task from an OSGB, glTF, FBX, OBJ, STL or IFC model item into Manager infra MinIO.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body Model3DGLBTaskRequest true "model 3D GLB generation task configuration"
// @Success 201 {object} Model3DGLBTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /model_3d_glb_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateModel3DGLBTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	req, err := decodeModel3DGLBTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.Model3DGLBTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.model3DGLBTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, model3DGLBTaskResponse(&task))
}

// GetModel3DGLBTask GET /api/v1/manager/model_3d_glb_tasks/:id
// @Summary 获取三维模型 GLB 快显任务配置 | Get model 3D GLB generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} Model3DGLBTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model_3d_glb_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetModel3DGLBTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeModel3DGLBGeneration})
	h.TaskDetail(c)
}

// UpdateModel3DGLBTask PUT /api/v1/manager/model_3d_glb_tasks/:id
// @Summary 更新三维模型 GLB 快显任务配置 | Update model 3D GLB generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body Model3DGLBTaskRequest true "model 3D GLB generation task configuration"
// @Success 200 {object} Model3DGLBTaskResponse "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /model_3d_glb_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateModel3DGLBTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.model3DGLBTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeModel3DGLBTaskRequest(c)
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
	if err := h.model3DGLBTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, model3DGLBTaskResponse(existing))
}

// DeleteModel3DGLBTask DELETE /api/v1/manager/model_3d_glb_tasks/:id
// @Summary 删除三维模型 GLB 快显任务配置 | Delete model 3D GLB generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /model_3d_glb_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteModel3DGLBTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.model3DGLBTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListGaussianSplatKSplatTasks GET /api/v1/manager/gaussian_splat_ksplat_tasks
// @Summary 列出 3DGS - KSplat 快显任务配置 | List 3DGS - KSplat quick view generation task configurations
// @Description 列出 Manager 模块的 3DGS - KSplat 快显任务配置。该私有入口固定返回 task_type=gaussian_splat_ksplat_generation；编排模块应使用标准 /tasks 入口。| List Manager 3DGS - KSplat quick view generation task configurations.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /gaussian_splat_ksplat_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListGaussianSplatKSplatTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.gaussianSplatKSplatTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]GaussianSplatKSplatTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, gaussianSplatKSplatTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateGaussianSplatKSplatTask POST /api/v1/manager/gaussian_splat_ksplat_tasks
// @Summary 创建 3DGS - KSplat 快显任务配置 | Create 3DGS - KSplat quick view generation task configuration
// @Description 创建新的 3DGS - KSplat 快显任务配置。源必须是 format=ply 或 splat 的 gaussian_splat item，并转换为 KSplat artifact 写入 Manager infra MinIO；format=ksplat 的源文件直接基础预览，不创建快显任务。| Create a 3DGS - KSplat quick view task from a format=ply or splat gaussian_splat item into Manager infra MinIO. format=ksplat sources are previewed directly and do not create quick view tasks.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body GaussianSplatKSplatTaskRequest true "gaussian splat KSplat generation task configuration"
// @Success 201 {object} GaussianSplatKSplatTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /gaussian_splat_ksplat_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreateGaussianSplatKSplatTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	req, err := decodeGaussianSplatKSplatTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.GaussianSplatKSplatTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.gaussianSplatKSplatTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gaussianSplatKSplatTaskResponse(&task))
}

// GetGaussianSplatKSplatTask GET /api/v1/manager/gaussian_splat_ksplat_tasks/:id
// @Summary 获取 3DGS - KSplat 快显任务配置 | Get 3DGS - KSplat quick view generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} GaussianSplatKSplatTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /gaussian_splat_ksplat_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetGaussianSplatKSplatTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypeGaussianSplatKSplatGeneration})
	h.TaskDetail(c)
}

// UpdateGaussianSplatKSplatTask PUT /api/v1/manager/gaussian_splat_ksplat_tasks/:id
// @Summary 更新 3DGS - KSplat 快显任务配置 | Update 3DGS - KSplat quick view generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body GaussianSplatKSplatTaskRequest true "gaussian splat KSplat generation task configuration"
// @Success 200 {object} GaussianSplatKSplatTaskResponse "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /gaussian_splat_ksplat_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdateGaussianSplatKSplatTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.gaussianSplatKSplatTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodeGaussianSplatKSplatTaskRequest(c)
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
	if err := h.gaussianSplatKSplatTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gaussianSplatKSplatTaskResponse(existing))
}

// DeleteGaussianSplatKSplatTask DELETE /api/v1/manager/gaussian_splat_ksplat_tasks/:id
// @Summary 删除 3DGS - KSplat 快显任务配置 | Delete 3DGS - KSplat quick view generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /gaussian_splat_ksplat_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteGaussianSplatKSplatTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.gaussianSplatKSplatTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListPointCloudCOPCTasks GET /api/v1/manager/point_cloud_copc_tasks
// @Summary 列出点云 COPC 快显任务配置 | List point cloud COPC quick view generation task configurations
// @Description 列出 Manager 模块的点云 COPC 快显任务配置。该私有入口固定返回 task_type=point_cloud_copc_generation；编排模块应使用标准 /tasks 入口。| List Manager point cloud COPC quick view generation task configurations.
// @Tags Manager
// @Produce json
// @Param page query int false "页码，默认1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认20 | Page size, default 20"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /point_cloud_copc_tasks [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListPointCloudCOPCTasks(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	tasks, total, err := h.pointCloudCOPCTaskSvc.List(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]PointCloudCOPCTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, pointCloudCOPCTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreatePointCloudCOPCTask POST /api/v1/manager/point_cloud_copc_tasks
// @Summary 创建点云 COPC 快显任务配置 | Create point cloud COPC quick view generation task configuration
// @Description 创建新的点云 COPC 快显任务配置。源必须是 format=las、laz、e57、pcd 或 xyz 的 point_cloud item，并转换为 COPC artifact 写入 Manager infra MinIO；format=copc 的源文件直接基础预览，不创建快显任务。| Create a point cloud COPC quick view task from a format=las, laz, e57, pcd, or xyz point_cloud item into Manager infra MinIO. format=copc sources are previewed directly and do not create quick view tasks.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body PointCloudCOPCTaskRequest true "point cloud COPC generation task configuration"
// @Success 201 {object} PointCloudCOPCTaskResponse "创建的任务配置 | Created task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.create"]
// @Router /point_cloud_copc_tasks [post]
// @Security BearerAuth
func (h *TaskProviderHandler) CreatePointCloudCOPCTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	req, err := decodePointCloudCOPCTaskRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := models.PointCloudCOPCTask{
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: strings.TrimSpace(req.Description),
		Enabled:     enabled,
		Schedule:    strings.TrimSpace(req.Schedule),
		NextRunAt:   req.NextRunAt,
		Config:      req.Config,
		CreatedBy:   &userID,
	}
	if err := h.pointCloudCOPCTaskSvc.Create(c.Request.Context(), &task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, pointCloudCOPCTaskResponse(&task))
}

// GetPointCloudCOPCTask GET /api/v1/manager/point_cloud_copc_tasks/:id
// @Summary 获取点云 COPC 快显任务配置 | Get point cloud COPC quick view generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} PointCloudCOPCTaskResponse "任务配置 | Task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /point_cloud_copc_tasks/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetPointCloudCOPCTask(c *gin.Context) {
	c.Params = append(c.Params, gin.Param{Key: "task_type", Value: commonExecution.TaskTypePointCloudCOPCGeneration})
	h.TaskDetail(c)
}

// UpdatePointCloudCOPCTask PUT /api/v1/manager/point_cloud_copc_tasks/:id
// @Summary 更新点云 COPC 快显任务配置 | Update point cloud COPC quick view generation task configuration
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Param body body PointCloudCOPCTaskRequest true "point cloud COPC generation task configuration"
// @Success 200 {object} PointCloudCOPCTaskResponse "更新后的任务配置 | Updated task configuration"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 404 {object} map[string]interface{} "任务不存在 | Task not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.update"]
// @Router /point_cloud_copc_tasks/{id} [put]
// @Security BearerAuth
func (h *TaskProviderHandler) UpdatePointCloudCOPCTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	existing, err := h.pointCloudCOPCTaskSvc.GetByID(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任务不存在"})
		return
	}
	req, err := decodePointCloudCOPCTaskRequest(c)
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
	if err := h.pointCloudCOPCTaskSvc.Update(c.Request.Context(), existing); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, pointCloudCOPCTaskResponse(existing))
}

// DeletePointCloudCOPCTask DELETE /api/v1/manager/point_cloud_copc_tasks/:id
// @Summary 删除点云 COPC 快显任务配置 | Delete point cloud COPC quick view generation task configuration
// @Tags Manager
// @Produce json
// @Param id path int true "任务ID | Task ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /point_cloud_copc_tasks/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeletePointCloudCOPCTask(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的任务ID"})
		return
	}
	if err := h.pointCloudCOPCTaskSvc.Delete(c.Request.Context(), uint(id), tenantID); err != nil {
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /raster_cog [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListRasterCOGs(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /raster_cog/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetRasterCOG(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /raster_cog/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteRasterCOG(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

// ListModel3DGLBs GET /api/v1/manager/model_3d_glb
// @Summary 列出三维模型 GLB 快显结果 | List model 3D GLB results
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model_3d_glb [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListModel3DGLBs(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.model3DGLBTaskSvc.ListResults(c.Request.Context(), repository.Model3DGLBFilter{
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

// GetModel3DGLB GET /api/v1/manager/model_3d_glb/:id
// @Summary 获取三维模型 GLB 快显详情 | Get model 3D GLB detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.Model3DGLB "结果详情 | Result detail"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /model_3d_glb/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetModel3DGLB(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.model3DGLBTaskSvc.GetResult(c.Request.Context(), uint(id), tenantID)
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

// DeleteModel3DGLB DELETE /api/v1/manager/model_3d_glb/:id
// @Summary 删除三维模型 GLB 快显 | Delete model 3D GLB
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /model_3d_glb/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteModel3DGLB(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.model3DGLBTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListGaussianSplatKSplats GET /api/v1/manager/gaussian_splat_ksplat
// @Summary 列出 3DGS - KSplat 快显结果 | List 3DGS - KSplat quick view results
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /gaussian_splat_ksplat [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListGaussianSplatKSplats(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.gaussianSplatKSplatTaskSvc.ListResults(c.Request.Context(), repository.GaussianSplatKSplatFilter{
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

// GetGaussianSplatKSplat GET /api/v1/manager/gaussian_splat_ksplat/:id
// @Summary 获取 3DGS - KSplat 快显详情 | Get 3DGS - KSplat quick view detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.GaussianSplatKSplat "结果详情 | Result detail"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /gaussian_splat_ksplat/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetGaussianSplatKSplat(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.gaussianSplatKSplatTaskSvc.GetResult(c.Request.Context(), uint(id), tenantID)
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

// DeleteGaussianSplatKSplat DELETE /api/v1/manager/gaussian_splat_ksplat/:id
// @Summary 删除 3DGS - KSplat 快显 | Delete 3DGS - KSplat quick view
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /gaussian_splat_ksplat/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteGaussianSplatKSplat(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.gaussianSplatKSplatTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListPointCloudCOPCs GET /api/v1/manager/point_cloud_copc
// @Summary 列出点云 COPC 快显结果 | List point cloud COPC quick view results
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /point_cloud_copc [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListPointCloudCOPCs(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.pointCloudCOPCTaskSvc.ListResults(c.Request.Context(), repository.PointCloudCOPCFilter{
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

// GetPointCloudCOPC GET /api/v1/manager/point_cloud_copc/:id
// @Summary 获取点云 COPC 快显详情 | Get point cloud COPC quick view detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.PointCloudCOPC "结果详情 | Result detail"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /point_cloud_copc/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetPointCloudCOPC(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.pointCloudCOPCTaskSvc.GetResult(c.Request.Context(), uint(id), tenantID)
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

// DeletePointCloudCOPC DELETE /api/v1/manager/point_cloud_copc/:id
// @Summary 删除点云 COPC 快显 | Delete point cloud COPC quick view
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /point_cloud_copc/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeletePointCloudCOPC(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.pointCloudCOPCTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已删除"})
}

// ListVectorMaterializedViews GET /api/v1/manager/vector_materialized_view
// @Summary 列出矢量物化视图结果 | List vector materialized view results
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_materialized_view [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListVectorMaterializedViews(c *gin.Context) {
	tenantID := tenantIDValue(c)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	itemID64, _ := strconv.ParseUint(c.Query("item_id"), 10, 32)
	taskID64, _ := strconv.ParseUint(c.Query("task_id"), 10, 32)
	results, total, err := h.vectorMaterializedViewTaskSvc.ListResults(c.Request.Context(), repository.VectorMaterializedViewFilter{
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

// GetVectorMaterializedView GET /api/v1/manager/vector_materialized_view/:id
// @Summary 获取矢量物化视图结果详情 | Get vector materialized view result detail
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} models.VectorMaterializedView "结果详情 | Result detail"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_materialized_view/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetVectorMaterializedView(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	result, err := h.vectorMaterializedViewTaskSvc.GetResult(c.Request.Context(), uint(id), tenantID)
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

// DeleteVectorMaterializedView DELETE /api/v1/manager/vector_materialized_view/:id
// @Summary 删除矢量物化视图结果 | Delete vector materialized view result
// @Tags Manager
// @Produce json
// @Param id path int true "结果ID | Result ID"
// @Success 200 {object} map[string]interface{} "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /vector_materialized_view/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteVectorMaterializedView(c *gin.Context) {
	tenantID := tenantIDValue(c)
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的结果ID"})
		return
	}
	if err := h.vectorMaterializedViewTaskSvc.DeleteResult(c.Request.Context(), uint(id), tenantID); err != nil {
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_tile_cache [get]
// @Security BearerAuth
func (h *TaskProviderHandler) ListTileCaches(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.read"]
// @Router /vector_tile_cache/{id} [get]
// @Security BearerAuth
func (h *TaskProviderHandler) GetTileCache(c *gin.Context) {
	tenantID := tenantIDValue(c)
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
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.derived_artifact.delete"]
// @Router /vector_tile_cache/{id} [delete]
// @Security BearerAuth
func (h *TaskProviderHandler) DeleteTileCache(c *gin.Context) {
	tenantID := tenantIDValue(c)
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

func decodeVectorMaterializedViewTaskRequest(c *gin.Context) (VectorMaterializedViewTaskRequest, error) {
	var req VectorMaterializedViewTaskRequest
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

func decodeModel3DGLBTaskRequest(c *gin.Context) (Model3DGLBTaskRequest, error) {
	var req Model3DGLBTaskRequest
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

func decodeGaussianSplatKSplatTaskRequest(c *gin.Context) (GaussianSplatKSplatTaskRequest, error) {
	var req GaussianSplatKSplatTaskRequest
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

func decodePointCloudCOPCTaskRequest(c *gin.Context) (PointCloudCOPCTaskRequest, error) {
	var req PointCloudCOPCTaskRequest
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
			SourceKind:      stringFromConfig(target["source_kind"]),
			FullName:        stringFromConfig(target["full_name"]),
			Schema:          stringFromConfig(target["schema"]),
			Table:           stringFromConfig(target["table"]),
		}
	}
	if tile, ok := asJSONMap(task.Config["tile"]); ok {
		options, _ := asJSONMap(task.Config["options"])
		resp.Tile = &TileCacheTaskTileResponse{
			ArchiveFormat:  stringFromConfig(tile["archive_format"]),
			TileType:       stringFromConfig(tile["tile_type"]),
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
			ItemLocator: stringFromConfig(source["item_locator"]), SourceEngineID: uintFromConfig(source["source_engine_id"]),
			ItemFingerprint: stringFromConfig(source["item_fingerprint"]), ItemID: uintFromConfig(source["item_id"]),
			Format: stringFromConfig(source["format"]), SourceSizeBytes: int64FromAPIConfig(source["source_size_bytes"], 0),
		}
	}
	resp.TargetFormat = stringFromConfig(task.Config["target_format"])
	if result, ok := asJSONMap(task.Config["result"]); ok {
		resp.Result = &Model3DTilesTaskResultResponse{StorageRef: stringFromConfig(result["storage_ref"])}
	}
	return resp
}

func model3DGLBTaskResponse(task *models.Model3DGLBTask) Model3DGLBTaskResponse {
	resp := Model3DGLBTaskResponse{}
	if task == nil {
		return resp
	}
	resp = Model3DGLBTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeModel3DGLBGeneration,
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
		resp.Source = &Model3DGLBTaskSourceResponse{
			ItemLocator:     stringFromConfig(source["item_locator"]),
			SourceEngineID:  uintFromConfig(source["source_engine_id"]),
			ItemFingerprint: stringFromConfig(source["item_fingerprint"]),
			ItemID:          uintFromConfig(source["item_id"]),
			Format:          stringFromConfig(source["format"]),
			SourceSizeBytes: int64FromAPIConfig(source["source_size_bytes"], 0),
		}
	}
	if result, ok := asJSONMap(task.Config["result"]); ok {
		resp.Result = &Model3DGLBTaskResultResponse{
			StorageRef: stringFromConfig(result["storage_ref"]),
			FileName:   stringFromConfig(result["file_name"]),
		}
	}
	return resp
}

func gaussianSplatKSplatTaskResponse(task *models.GaussianSplatKSplatTask) GaussianSplatKSplatTaskResponse {
	resp := GaussianSplatKSplatTaskResponse{}
	if task == nil {
		return resp
	}
	resp = GaussianSplatKSplatTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeGaussianSplatKSplatGeneration,
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
		resp.Source = &GaussianSplatKSplatTaskSourceResponse{
			ItemLocator:              stringFromConfig(source["item_locator"]),
			SourceEngineID:           uintFromConfig(source["source_engine_id"]),
			ItemFingerprint:          stringFromConfig(source["item_fingerprint"]),
			ItemID:                   uintFromConfig(source["item_id"]),
			Format:                   stringFromConfig(source["format"]),
			SourceSizeBytes:          int64FromAPIConfig(source["source_size_bytes"], 0),
			Bounds3D:                 jsonMapFromAPIConfig(source["bounds_3d"]),
			SampledBounds3D:          jsonMapFromAPIConfig(source["sampled_bounds_3d"]),
			SampledBoundsSampleCount: int64PtrFromAPIConfig(source["sampled_bounds_sample_count"]),
		}
	}
	if result, ok := asJSONMap(task.Config["result"]); ok {
		resp.Result = &GaussianSplatKSplatTaskResultResponse{
			StorageRef: stringFromConfig(result["storage_ref"]),
			FileName:   stringFromConfig(result["file_name"]),
		}
	}
	return resp
}

func pointCloudCOPCTaskResponse(task *models.PointCloudCOPCTask) PointCloudCOPCTaskResponse {
	resp := PointCloudCOPCTaskResponse{}
	if task == nil {
		return resp
	}
	resp = PointCloudCOPCTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypePointCloudCOPCGeneration,
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
		resp.Source = &PointCloudCOPCTaskSourceResponse{
			ItemLocator:     stringFromConfig(source["item_locator"]),
			SourceEngineID:  uintFromConfig(source["source_engine_id"]),
			ItemFingerprint: stringFromConfig(source["item_fingerprint"]),
			ItemID:          uintFromConfig(source["item_id"]),
			Format:          stringFromConfig(source["format"]),
			SourceSizeBytes: int64FromAPIConfig(source["source_size_bytes"], 0),
		}
	}
	if result, ok := asJSONMap(task.Config["result"]); ok {
		resp.Result = &PointCloudCOPCTaskResultResponse{
			StorageRef: stringFromConfig(result["storage_ref"]),
			FileName:   stringFromConfig(result["file_name"]),
		}
	}
	return resp
}

func vectorMaterializedViewTaskResponse(task *models.VectorMaterializedViewTask) VectorMaterializedViewTaskResponse {
	resp := VectorMaterializedViewTaskResponse{}
	if task == nil {
		return resp
	}
	resp = VectorMaterializedViewTaskResponse{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		TaskType:            commonExecution.TaskTypeVectorMaterializedViewGeneration,
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
		resp.Target = &VectorMaterializedViewTaskTargetResponse{
			ItemID:          uintFromConfig(target["item_id"]),
			ItemFingerprint: stringFromConfig(target["item_fingerprint"]),
			Locator:         stringFromConfig(target["locator"]),
			SourceEngineID:  uintFromConfig(target["source_engine_id"]),
			Schema:          stringFromConfig(target["schema"]),
			Table:           stringFromConfig(target["table"]),
		}
	}
	if geometry, ok := asJSONMap(task.Config["geometry"]); ok {
		resp.Geometry = &VectorMaterializedViewTaskGeometryResponse{
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

func int64PtrFromAPIConfig(value interface{}) *int64 {
	if value == nil {
		return nil
	}
	parsed := int64FromAPIConfig(value, 0)
	return &parsed
}

func jsonMapFromAPIConfig(value interface{}) commonModels.JSONMap {
	payload, ok := asJSONMap(value)
	if !ok {
		return nil
	}
	return payload
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
