package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	developi18n "github.com/addp/develop/backend/i18n"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// NotebookHandler Notebook 开发 API 处理器
type NotebookHandler struct {
	jupyterService               *service.JupyterService
	notebookExecutionService     *service.NotebookExecutionService
	devTaskService               *service.DevTaskService
	notebookSessionService       *service.NotebookSessionService
	notebookCopilotService       *service.NotebookCopilotService
	listSessionEngineDescriptors func(context.Context, string, string) ([]commonModels.EngineRuntimeDescriptor, error)
}

func (h *NotebookHandler) SetProtectionGate(gate service.NotebookProtectionGate) {
	if h != nil && h.notebookSessionService != nil {
		h.notebookSessionService.SetProtectionGate(gate)
	}
}

func (h *NotebookHandler) HasActiveExecutionsForTenant(tenantID int64) bool {
	return h != nil && h.notebookSessionService != nil && h.notebookSessionService.HasActiveExecutionsForTenant(tenantID)
}

// NewNotebookHandler 创建 Notebook 处理器
func NewNotebookHandler(
	jupyterService *service.JupyterService,
	notebookExecutionService *service.NotebookExecutionService,
	devTaskService *service.DevTaskService,
	notebookEngineCatalog service.NotebookSessionControlPlane,
	developServiceURL string,
	copilotServiceURL string,
) *NotebookHandler {
	sessionService := service.NewNotebookSessionService(jupyterService, devTaskService, notebookEngineCatalog, time.Hour, developServiceURL)
	return &NotebookHandler{
		jupyterService:               jupyterService,
		notebookExecutionService:     notebookExecutionService,
		devTaskService:               devTaskService,
		notebookSessionService:       sessionService,
		notebookCopilotService:       service.NewNotebookCopilotService(sessionService, copilotServiceURL, nil),
		listSessionEngineDescriptors: sessionService.ListDataEngineDescriptors,
	}
}

// CreateNotebook 创建空白 Notebook 和对应 script 任务。
// @Summary 新建空白 Notebook | Create blank Notebook
// @Tags Notebook
// @Accept json
// @Produce json
// @Param body body models.CreateNotebookSwaggerRequest true "Notebook 创建参数 | Notebook creation parameters"
// @Success 201 {object} models.UploadNotebookSwaggerResponse "已创建的 Notebook | Created Notebook"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.create"]
// @Router /notebooks [post]
func (h *NotebookHandler) CreateNotebook(c *gin.Context) {
	var req models.CreateNotebookSwaggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookCreateFailed)})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Kernel = strings.TrimSpace(req.Kernel)
	if req.Name == "" || req.EngineID == 0 || req.Kernel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookCreateFailed)})
		return
	}
	tenantID := tenantIDValue(c)
	if _, err := h.jupyterService.GetNotebookEngine(c.Request.Context(), tenantID, req.EngineID); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookEngineUnavailable, err.Error())})
		return
	}
	if err := h.jupyterService.ValidateKernel(c.Request.Context(), tenantID, req.EngineID, req.Kernel); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookKernelUnavailable, err.Error())})
		return
	}

	notebookPath := uuid.NewString() + ".ipynb"
	minioPath := fmt.Sprintf("tenant_%d/notebooks/%s", tenantID, notebookPath)
	cellID := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	blankNotebook, err := json.Marshal(map[string]interface{}{
		"cells": []map[string]interface{}{{
			"cell_type": "code", "execution_count": nil, "id": cellID,
			"metadata": map[string]interface{}{}, "outputs": []interface{}{}, "source": []string{},
		}},
		"metadata": map[string]interface{}{
			"kernelspec":    map[string]interface{}{"display_name": "Python 3", "language": "python", "name": req.Kernel},
			"language_info": map[string]interface{}{"name": "python"},
		},
		"nbformat": 4, "nbformat_minor": 5,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookCreateFailed)})
		return
	}
	if err := h.notebookExecutionService.SaveNotebookToMinIO(c.Request.Context(), minioPath, blankNotebook); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookStoreFailed, err.Error())})
		return
	}
	devTask, err := h.createNotebookTask(req.Name, req.Description, notebookPath, minioPath, req.Kernel, map[string]interface{}{}, req.EngineID, tenantID, userIDValue(c))
	if err != nil {
		_ = h.notebookExecutionService.DeleteNotebookFromMinIO(c.Request.Context(), minioPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookCreateFailed, err.Error())})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"message": "Notebook 创建成功", "dev_task": devTask})
}

func (h *NotebookHandler) createNotebookTask(name, description, notebookPath, minioPath, kernel string, parameters map[string]interface{}, engineID, tenantID, userID uint) (*models.DevTask, error) {
	return h.devTaskService.CreateDevTask(&models.CreateDevTaskRequest{
		Name: name, DisplayName: name, DevType: "script", Description: description, Timeout: 600,
		Content: models.DevTaskContent{
			"notebook_path": notebookPath, "minio_path": minioPath, "kernel": kernel,
			"parameters": parameters, "description": description,
		},
		ExecutionConfig: models.DevTaskContent{"engine_id": engineID},
	}, tenantID, userID)
}

// ListKernelsResponse 列出 Kernel 响应
type ListKernelsResponse struct {
	Kernels []service.KernelInfo `json:"kernels"`
}

type NotebookEngineListResponse []commonModels.EngineRuntimeDescriptor

// ListNotebookEngines 列出支持 Notebook 的 Script Engine 实例及其当前状态。
// @Summary 列出 Notebook 引擎选择项 | List Notebook engine options
// @Description 返回 active 且支持 notebook 模式的注册 Script Engine；非 online 项由前端展示并禁选 | Return active registered Script Engines supporting notebook mode; clients must show but disable non-online options
// @Tags Notebook
// @Produce json
// @Success 200 {array} commonModels.EngineRuntimeDescriptor "Notebook引擎列表 | Notebook engine list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /notebook-engines [get]
func (h *NotebookHandler) ListNotebookEngines(c *gin.Context) {
	engines, err := h.jupyterService.ListNotebookEngines(c.Request.Context(), tenantIDValue(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgNotebookEngineListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, NotebookEngineListResponse(engines))
}

// ListKernels 列出指定 Notebook 引擎实例的 Kernel。
// @Summary 按 Notebook 引擎列出 Kernel | List kernels by Notebook engine
// @Tags Notebook
// @Produce json
// @Param id path int true "Notebook 引擎实例 ID | Notebook engine instance ID"
// @Success 200 {object} ListKernelsResponse "Kernel列表 | Kernel list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /notebook-engines/{id}/kernels [get]
func (h *NotebookHandler) ListKernels(c *gin.Context) {
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || engineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookEngineRequired)})
		return
	}
	kernels, err := h.jupyterService.ListKernels(c.Request.Context(), tenantIDValue(c), uint(engineID))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgNotebookKernelListFailed, err.Error()),
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ListKernelsResponse{
		Kernels: kernels,
	})
}

// UploadNotebookRequest 上传 Notebook 请求
type UploadNotebookRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Kernel      string                 `json:"kernel" binding:"required"`
	Tags        []string               `json:"tags"`
	EngineID    uint                   `json:"engine_id" binding:"required"`
}

// UploadNotebook 上传 Notebook 文件并创建开发任务
// @Summary 上传 Notebook | Upload Notebook
// @Tags Notebook
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Notebook 文件 (.ipynb) | Notebook file (.ipynb)"
// @Param name formData string true "Notebook 名称 | Notebook name"
// @Param description formData string false "描述 | Description"
// @Param parameters formData string false "参数 (JSON 对象) | Parameters (JSON object)"
// @Param engine_id formData int true "Notebook 引擎实例 ID | Notebook engine instance ID"
// @Param kernel formData string true "Kernel 名称 | Kernel name"
// @Success 200 {object} models.UploadNotebookSwaggerResponse "已上传的Notebook | Uploaded Notebook"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.create"]
// @Router /notebooks/upload [post]
func (h *NotebookHandler) UploadNotebook(c *gin.Context) {
	tenantID := tenantIDValue(c)
	userID := userIDValue(c)

	// 读取表单数据
	name := c.PostForm("name")
	description := c.PostForm("description")
	parametersStr := c.PostForm("parameters")
	kernel := strings.TrimSpace(c.PostForm("kernel"))
	if kernel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookKernelRequired)})
		return
	}
	engineID64, err := strconv.ParseUint(c.PostForm("engine_id"), 10, 32)
	if err != nil || engineID64 == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookEngineRequired)})
		return
	}
	engineID := uint(engineID64)
	if _, err := h.jupyterService.GetNotebookEngine(c.Request.Context(), tenantID, engineID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgNotebookEngineUnavailable, err.Error()),
			"details": err.Error(),
		})
		return
	}
	if err := h.jupyterService.ValidateKernel(c.Request.Context(), tenantID, engineID, kernel); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   commoni18n.TWithDetail(c, developi18n.MsgNotebookKernelUnavailable, err.Error()),
			"details": err.Error(),
		})
		return
	}

	// 解析参数
	var parameters map[string]interface{}
	if parametersStr != "" {
		if err := json.Unmarshal([]byte(parametersStr), &parameters); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookInvalidParameters)})
			return
		}
	}
	if parameters == nil {
		parameters = make(map[string]interface{})
	}

	// 读取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookFileRequired)})
		return
	}

	// 打开文件
	uploadedFile, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法打开上传的文件"})
		return
	}
	defer uploadedFile.Close()

	// 读取文件内容
	notebookContent, err := io.ReadAll(uploadedFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法读取文件内容"})
		return
	}

	// 对象路径使用服务端生成的不可冲突名称；用户文件名只作为导入来源信息。
	notebookPath := uuid.NewString() + ".ipynb"

	// 生成完整 MinIO 路径用于保存文件
	minioPath := fmt.Sprintf("tenant_%d/notebooks/%s", tenantID, notebookPath)

	// 保存到 MinIO
	if err := h.notebookExecutionService.SaveNotebookToMinIO(c.Request.Context(), minioPath, notebookContent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookStoreFailed, err.Error())})
		return
	}

	devTask, err := h.createNotebookTask(name, description, notebookPath, minioPath, kernel, parameters, engineID, tenantID, userID)
	if err != nil {
		_ = h.notebookExecutionService.DeleteNotebookFromMinIO(c.Request.Context(), minioPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookCreateFailed, err.Error())})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "Notebook 上传成功",
		"dev_task": devTask,
	})
}

// DownloadNotebook 下载 Notebook 文件
// @Summary 下载 Notebook | Download Notebook
// @Tags Notebook
// @Produce application/json
// @Param id path int true "DevTask ID | DevTask ID"
// @Success 200 {file} binary "Notebook文件 | Notebook file"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /notebooks/:id/download [get]
func (h *NotebookHandler) DownloadNotebook(c *gin.Context) {
	tenantID := tenantIDValue(c)

	// 获取开发任务 ID
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	// 查询开发任务
	devTask, err := h.devTaskService.GetDevTask(uri.ID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notebook 不存在"})
		return
	}

	if !devTask.IsNotebookScript() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不是 Notebook 类型"})
		return
	}

	// 获取 MinIO 路径
	minioPath, ok := devTask.Content["minio_path"].(string)
	if !ok || minioPath == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Notebook 路径不存在"})
		return
	}

	// 从 MinIO 读取文件
	notebookContent, err := h.notebookExecutionService.ReadNotebookFromMinIO(c.Request.Context(), minioPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取 Notebook 失败: %v", err)})
		return
	}

	// 设置响应头
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.ipynb", devTask.Name))
	c.Header("Content-Type", "application/json")
	c.Data(http.StatusOK, "application/json", notebookContent)
}

// ListNotebooks 列出用户的 Notebooks
// @Summary 列出 Notebooks | List Notebooks
// @Tags Notebook
// @Produce json
// @Param page query int false "页码 | Page number" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20)
// @Success 200 {object} models.ListDevTasksSwaggerResponse "Notebook列表 | Notebook list"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.read"]
// @Router /notebooks [get]
func (h *NotebookHandler) ListNotebooks(c *gin.Context) {
	tenantID := tenantIDValue(c)

	// 解析查询参数
	var req models.ListDevTasksRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 设置默认值
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	// Notebook 是 script 类型开发任务的当前承载形态，通过 content.notebook_path 标识。
	items, total, err := h.devTaskService.ListNotebookScripts(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("查询失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, models.ListDevTasksResponse{
		Items:    items,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	})
}

// UpdateRuntimeBinding 完整替换原 Notebook 任务的运行时绑定。
// @Summary 更换 Notebook 运行时绑定 | Replace Notebook runtime binding
// @Description 校验目标引擎和 Kernel 后更新原任务，仅影响后续执行，历史执行快照保持不变。| Validate the target engine and kernel, then update the original task for future executions only; historical execution snapshots remain unchanged.
// @Tags Notebook
// @Accept json
// @Produce json
// @Param id path int true "DevTask ID | DevTask ID"
// @Param body body models.NotebookRuntimeBindingSwaggerRequest true "运行时绑定 | Runtime binding"
// @Success 200 {object} models.DevTaskSwagger "已更新的 Notebook | Updated Notebook"
// @Failure 400 {object} models.ErrorResponse "请求参数错误 | Invalid request"
// @Failure 404 {object} models.ErrorResponse "Notebook 不存在 | Notebook not found"
// @Failure 422 {object} models.ErrorResponse "引擎、Kernel 或任务类型校验失败 | Engine, kernel, or task type validation failed"
// @Failure 500 {object} models.ErrorResponse "更新失败 | Update failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.update"]
// @Router /notebooks/{id}/runtime-binding [put]
func (h *NotebookHandler) UpdateRuntimeBinding(c *gin.Context) {
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookInvalidID)})
		return
	}

	var req models.NotebookRuntimeBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookInvalidRuntimeBinding)})
		return
	}
	req.Kernel = strings.TrimSpace(req.Kernel)
	if req.EngineID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookEngineRequired)})
		return
	}
	if req.Kernel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookKernelRequired)})
		return
	}

	tenantID := tenantIDValue(c)
	userID := userIDValue(c)
	task, err := h.devTaskService.GetDevTask(uri.ID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookNotFound)})
		return
	}
	if !task.IsNotebookScript() {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotNotebook)})
		return
	}
	if _, err := h.jupyterService.GetNotebookEngine(c.Request.Context(), tenantID, req.EngineID); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookEngineUnavailable, err.Error()),
		})
		return
	}
	if err := h.jupyterService.ValidateKernel(c.Request.Context(), tenantID, req.EngineID, req.Kernel); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": commoni18n.TWithDetail(c, developi18n.MsgNotebookKernelUnavailable, err.Error()),
		})
		return
	}

	updated, err := h.devTaskService.RebindNotebookRuntime(uri.ID, tenantID, userID, req.EngineID, req.Kernel)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNotebookNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookNotFound)})
		case errors.Is(err, service.ErrTaskNotNotebook):
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": commoni18n.T(c, developi18n.MsgTaskNotNotebook)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, developi18n.MsgNotebookRuntimeBindingFailed)})
		}
		return
	}
	c.JSON(http.StatusOK, updated)
}

// DeleteNotebook 删除 Notebook
// @Summary 删除 Notebook | Delete Notebook
// @Tags Notebook
// @Param id path int true "DevTask ID | DevTask ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.notebook.delete"]
// @Router /notebooks/:id [delete]
func (h *NotebookHandler) DeleteNotebook(c *gin.Context) {
	tenantID := tenantIDValue(c)

	// 获取开发任务 ID
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	// 查询开发任务
	devTask, err := h.devTaskService.GetDevTask(uri.ID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notebook 不存在"})
		return
	}

	if !devTask.IsNotebookScript() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不是 Notebook 类型"})
		return
	}

	// 删除开发任务（软删除）
	if err := h.devTaskService.DeleteDevTask(uri.ID, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除失败: %v", err)})
		return
	}

	// TODO: 可选择从 MinIO 删除文件（暂时保留文件，仅软删除开发任务）

	c.JSON(http.StatusOK, gin.H{
		"message": "Notebook 删除成功",
	})
}
