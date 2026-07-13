package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
)

type CADPreviewHandler struct {
	service       *service.CADPreviewTaskService
	repo          *repository.CADPreviewRepository
	minioClient   *minio.Client
	defaultBucket string
}

func NewCADPreviewHandler(service *service.CADPreviewTaskService, repo *repository.CADPreviewRepository, client *minio.Client, bucket string) *CADPreviewHandler {
	return &CADPreviewHandler{service: service, repo: repo, minioClient: client, defaultBucket: bucket}
}

type CADPreviewTaskRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Enabled     *bool                `json:"enabled,omitempty"`
	Config      commonModels.JSONMap `json:"config"`
}

type CADPreviewTaskResponse struct {
	ID                  uint                 `json:"id"`
	TenantID            uint                 `json:"tenant_id"`
	TaskType            string               `json:"task_type"`
	Name                string               `json:"name"`
	Description         string               `json:"description,omitempty"`
	Enabled             bool                 `json:"enabled"`
	LastExecutionID     *string              `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string              `json:"last_execution_status,omitempty"`
	Config              commonModels.JSONMap `json:"config"`
	CreatedBy           *uint                `json:"created_by,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

func cadPreviewTaskResponse(task *models.CADPreviewTask) CADPreviewTaskResponse {
	if task == nil {
		return CADPreviewTaskResponse{}
	}
	return CADPreviewTaskResponse{ID: task.ID, TenantID: task.TenantID, TaskType: commonExecution.TaskTypeCADPreviewGeneration, Name: task.Name, Description: task.Description, Enabled: task.Enabled, LastExecutionID: task.LastExecutionID, LastExecutionStatus: task.LastExecutionStatus, Config: task.Config.Clone(), CreatedBy: task.CreatedBy, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
}

// ListTasks 列出 CAD 预览任务。
// @Summary 列出 CAD 预览生成任务 | List CAD preview generation tasks
// @Tags Manager
// @Produce json
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} map[string]interface{} "任务列表 | Task list"
// @Router /cad-preview-tasks [get]
// @Security BearerAuth
func (h *CADPreviewHandler) ListTasks(c *gin.Context) {
	page, pageSize := pagination(c)
	tasks, total, err := h.service.List(c.Request.Context(), c.GetUint("tenant_id"), page, pageSize)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	items := make([]CADPreviewTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, cadPreviewTaskResponse(task))
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// CreateTask 创建 CAD 预览任务。
// @Summary 创建 CAD 预览生成任务 | Create CAD preview generation task
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body CADPreviewTaskRequest true "任务配置 | Task configuration"
// @Success 201 {object} CADPreviewTaskResponse
// @Failure 400 {object} map[string]interface{}
// @Router /cad-preview-tasks [post]
// @Security BearerAuth
func (h *CADPreviewHandler) CreateTask(c *gin.Context) {
	var req CADPreviewTaskRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	task := &models.CADPreviewTask{TenantID: c.GetUint("tenant_id"), Name: req.Name, Description: req.Description, Enabled: enabled, Config: req.Config}
	if userID := c.GetUint("user_id"); userID > 0 {
		task.CreatedBy = &userID
	}
	if err := h.service.Create(c.Request.Context(), task); err != nil {
		commonAPI.BadRequestError(c, err.Error())
		return
	}
	c.JSON(http.StatusCreated, cadPreviewTaskResponse(task))
}

// GetTask 获取 CAD 预览任务。
// @Summary 获取 CAD 预览生成任务 | Get CAD preview generation task
// @Tags Manager
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Success 200 {object} CADPreviewTaskResponse
// @Router /cad-preview-tasks/{id} [get]
// @Security BearerAuth
func (h *CADPreviewHandler) GetTask(c *gin.Context) { h.respondTask(c, false) }

// UpdateTask 更新 CAD 预览任务。
// @Summary 更新 CAD 预览生成任务 | Update CAD preview generation task
// @Tags Manager
// @Accept json
// @Produce json
// @Param id path int true "任务 ID | Task ID"
// @Param body body CADPreviewTaskRequest true "任务配置 | Task configuration"
// @Success 200 {object} CADPreviewTaskResponse
// @Router /cad-preview-tasks/{id} [put]
// @Security BearerAuth
func (h *CADPreviewHandler) UpdateTask(c *gin.Context) { h.respondTask(c, true) }

func (h *CADPreviewHandler) respondTask(c *gin.Context, update bool) {
	id, ok := positiveID(c)
	if !ok {
		return
	}
	task, err := h.service.GetByID(c.Request.Context(), id, c.GetUint("tenant_id"))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if task == nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "CAD preview task not found")
		return
	}
	if update {
		var req CADPreviewTaskRequest
		if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
		task.Name, task.Description, task.Config = req.Name, req.Description, req.Config
		if req.Enabled != nil {
			task.Enabled = *req.Enabled
		}
		if err := h.service.Update(c.Request.Context(), task); err != nil {
			commonAPI.BadRequestError(c, err.Error())
			return
		}
	}
	c.JSON(http.StatusOK, cadPreviewTaskResponse(task))
}

// DeleteTask 删除 CAD 预览任务。
// @Summary 删除 CAD 预览生成任务 | Delete CAD preview generation task
// @Tags Manager
// @Param id path int true "任务 ID | Task ID"
// @Success 204
// @Router /cad-preview-tasks/{id} [delete]
// @Security BearerAuth
func (h *CADPreviewHandler) DeleteTask(c *gin.Context) {
	id, ok := positiveID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id, c.GetUint("tenant_id")); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// ListResults 列出 CAD 预览结果。
// @Summary 列出 CAD 预览结果 | List CAD preview results
// @Tags Manager
// @Produce json
// @Param task_id query int false "任务 ID | Task ID"
// @Param status query string false "结果状态 | Result status"
// @Param q query string false "关键词 | Keyword"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} map[string]interface{}
// @Router /cad-previews [get]
// @Security BearerAuth
func (h *CADPreviewHandler) ListResults(c *gin.Context) {
	page, pageSize := pagination(c)
	taskID, _ := strconv.ParseUint(strings.TrimSpace(c.Query("task_id")), 10, 32)
	items, total, err := h.service.ListResults(c.Request.Context(), repository.CADPreviewFilter{
		TenantID: c.GetUint("tenant_id"),
		TaskID:   uint(taskID),
		Status:   c.Query("status"),
		Q:        c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize})
}

// GetResult 获取 CAD 预览结果。
// @Summary 获取 CAD 预览结果 | Get CAD preview result
// @Tags Manager
// @Produce json
// @Param id path int true "CAD preview ID"
// @Success 200 {object} models.CADPreview
// @Router /cad-previews/{id} [get]
// @Security BearerAuth
func (h *CADPreviewHandler) GetResult(c *gin.Context) {
	id, ok := positiveID(c)
	if !ok {
		return
	}
	result, err := h.service.GetResult(c.Request.Context(), id, c.GetUint("tenant_id"))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	if result == nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "CAD preview not found")
		return
	}
	c.JSON(http.StatusOK, result)
}

// DeleteResult 删除 CAD 预览结果及受管瓦片。
// @Summary 删除 CAD 预览结果 | Delete CAD preview result
// @Tags Manager
// @Param id path int true "CAD preview ID"
// @Success 204
// @Router /cad-previews/{id} [delete]
// @Security BearerAuth
func (h *CADPreviewHandler) DeleteResult(c *gin.Context) {
	id, ok := positiveID(c)
	if !ok {
		return
	}
	if err := h.service.DeleteResult(c.Request.Context(), id, c.GetUint("tenant_id")); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// GetManifest 返回 CAD 预览 manifest。
// @Summary 读取 CAD 预览清单 | Read CAD preview manifest
// @Tags Manager
// @Produce json
// @Param id path int true "CAD preview ID"
// @Success 200 {object} map[string]interface{}
// @Router /cad-previews/{id}/manifest [get]
// @Security BearerAuth
func (h *CADPreviewHandler) GetManifest(c *gin.Context) {
	result, bucket, prefix, ok := h.readyResult(c)
	if !ok {
		return
	}
	object, err := h.minioClient.GetObject(c.Request.Context(), bucket, path.Join(prefix, result.ManifestRef), minio.GetObjectOptions{})
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer object.Close()
	var manifest map[string]interface{}
	if err := json.NewDecoder(object).Decode(&manifest); err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	manifest["tile_url_template"] = fmt.Sprintf("/api/v1/manager/cad-previews/%d/tiles/{z}/{x}/{y}", result.ID)
	manifest["default_space"] = "model-space"
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, manifest)
}

// GetTile 返回 CAD WebP 瓦片。
// @Summary 读取 CAD 预览瓦片 | Read CAD preview tile
// @Tags Manager
// @Produce image/webp
// @Param id path int true "CAD preview ID"
// @Param z path int true "Zoom"
// @Param x path int true "Tile X"
// @Param y path int true "Tile Y"
// @Success 200 "WebP tile"
// @Router /cad-previews/{id}/tiles/{z}/{x}/{y} [get]
// @Security BearerAuth
func (h *CADPreviewHandler) GetTile(c *gin.Context) {
	result, bucket, prefix, ok := h.readyResult(c)
	if !ok {
		return
	}
	z, errZ := strconv.Atoi(c.Param("z"))
	x, errX := strconv.Atoi(c.Param("x"))
	y, errY := strconv.Atoi(c.Param("y"))
	if errZ != nil || errX != nil || errY != nil || z < result.MinZoom || z > result.MaxZoom || x < 0 || y < 0 || x >= 1<<z || y >= 1<<z {
		commonAPI.BadRequestError(c, "invalid CAD tile coordinate")
		return
	}
	objectName := path.Join(prefix, "model-space", strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".webp")
	object, err := h.minioClient.GetObject(c.Request.Context(), bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}
	defer object.Close()
	if _, err := object.Stat(); err != nil {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "CAD tile not found")
		return
	}
	c.Header("Content-Type", "image/webp")
	c.Header("Cache-Control", "private, max-age=3600")
	if _, err := io.Copy(c.Writer, object); err != nil {
		return
	}
}

func (h *CADPreviewHandler) readyResult(c *gin.Context) (*models.CADPreview, string, string, bool) {
	if h == nil || h.repo == nil || h.minioClient == nil {
		commonAPI.InternalServerError(c, "CAD preview service is not available")
		return nil, "", "", false
	}
	id, ok := positiveID(c)
	if !ok {
		return nil, "", "", false
	}
	result, err := h.repo.GetByID(c.Request.Context(), id, c.GetUint("tenant_id"))
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return nil, "", "", false
	}
	if result == nil || result.Status != models.CADPreviewStatusReady {
		commonAPI.ErrorResponse(c, http.StatusNotFound, "CAD preview is not ready")
		return nil, "", "", false
	}
	bucket, prefix, err := rastercogref.ObjectLocation(result.StorageRef, h.defaultBucket)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return nil, "", "", false
	}
	return result, bucket, strings.Trim(prefix, "/"), true
}

func positiveID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || value == 0 {
		commonAPI.BadRequestError(c, "invalid id")
		return 0, false
	}
	return uint(value), true
}
func pagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}
