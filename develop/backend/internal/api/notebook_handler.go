package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// NotebookHandler Notebook 开发 API 处理器
type NotebookHandler struct {
	jupyterService           *service.JupyterService
	notebookExecutionService *service.NotebookExecutionService
	devTaskService           *service.DevTaskService
}

// NewNotebookHandler 创建 Notebook 处理器
func NewNotebookHandler(
	jupyterService *service.JupyterService,
	notebookExecutionService *service.NotebookExecutionService,
	devTaskService *service.DevTaskService,
) *NotebookHandler {
	return &NotebookHandler{
		jupyterService:           jupyterService,
		notebookExecutionService: notebookExecutionService,
		devTaskService:           devTaskService,
	}
}

// GetJupyterURLRequest 获取 Jupyter URL 请求
type GetJupyterURLRequest struct {
	NotebookPath string `json:"notebook_path"` // Notebook 文件路径
}

// GetJupyterURLResponse 获取 Jupyter URL 响应
type GetJupyterURLResponse struct {
	JupyterURL string `json:"jupyter_url"` // Jupyter Lab URL
}

// GetJupyterURL 获取 Jupyter Lab 访问 URL
// @Summary 获取 Jupyter Lab 访问 URL | Get Jupyter Lab access URL
// @Tags Notebook
// @Accept json
// @Produce json
// @Param body body GetJupyterURLRequest true "请求 | Request"
// @Success 200 {object} GetJupyterURLResponse "Jupyter URL | Jupyter URL"
// @Router /notebooks/jupyter-url [post]
func (h *NotebookHandler) GetJupyterURL(c *gin.Context) {
	var req GetJupyterURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 构造 Jupyter Lab URL
	// 格式: http://jupyter-engine:8088/lab/tree/<path>
	jupyterURL := fmt.Sprintf("http://localhost:8088/lab/tree/%s", req.NotebookPath)

	c.JSON(http.StatusOK, GetJupyterURLResponse{
		JupyterURL: jupyterURL,
	})
}

// ExecuteNotebookRequest 执行 Notebook 请求
type ExecuteNotebookRequest struct {
	InputPath  string                 `json:"input_path" binding:"required"`
	OutputPath string                 `json:"output_path" binding:"required"`
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    int                    `json:"timeout"` // 超时时间（秒）
}

// ExecuteNotebookResponse 执行 Notebook 响应
type ExecuteNotebookResponse struct {
	Status               string                   `json:"status"`
	Message              string                   `json:"message"`
	ExecutionTimeSeconds float64                  `json:"execution_time_seconds"`
	OutputPath           string                   `json:"output_path"`
	OutputCount          int                      `json:"output_count"`
	Outputs              []map[string]interface{} `json:"outputs"`
	ErrorMessage         string                   `json:"error_message,omitempty"`
}

// ExecuteNotebook 执行 Notebook
// @Summary 执行 Notebook | Execute Notebook
// @Tags Notebook
// @Accept json
// @Produce json
// @Param body body ExecuteNotebookRequest true "执行请求 | Execution request"
// @Success 200 {object} ExecuteNotebookResponse "执行结果 | Execution result"
// @Router /notebooks/execute [post]
func (h *NotebookHandler) ExecuteNotebook(c *gin.Context) {
	var req ExecuteNotebookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 调用 JupyterService 执行
	result, err := h.jupyterService.ExecuteNotebook(
		c.Request.Context(),
		req.InputPath,
		req.OutputPath,
		req.Parameters,
		req.Timeout,
	)

	if err != nil {
		// 即使执行失败，也可能有部分结果
		if result != nil {
			c.JSON(http.StatusOK, ExecuteNotebookResponse{
				Status:               result.Status,
				Message:              result.Message,
				ExecutionTimeSeconds: result.ExecutionTimeSeconds,
				OutputPath:           result.OutputPath,
				OutputCount:          result.OutputCount,
				Outputs:              result.Outputs,
				ErrorMessage:         result.ErrorMessage,
			})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Notebook 执行失败",
				"details": err.Error(),
			})
		}
		return
	}

	c.JSON(http.StatusOK, ExecuteNotebookResponse{
		Status:               result.Status,
		Message:              result.Message,
		ExecutionTimeSeconds: result.ExecutionTimeSeconds,
		OutputPath:           result.OutputPath,
		OutputCount:          result.OutputCount,
		Outputs:              result.Outputs,
	})
}

// ListKernelsResponse 列出 Kernel 响应
type ListKernelsResponse struct {
	Kernels []service.KernelInfo `json:"kernels"`
}

// ListKernels 列出可用的 Kernel
// @Summary 列出可用的 Kernel | List available kernels
// @Tags Notebook
// @Produce json
// @Success 200 {object} ListKernelsResponse "Kernel列表 | Kernel list"
// @Router /notebooks/kernels [get]
func (h *NotebookHandler) ListKernels(c *gin.Context) {
	kernels, err := h.jupyterService.ListKernels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "获取 Kernel 列表失败",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ListKernelsResponse{
		Kernels: kernels,
	})
}

// HealthCheck 检查 Jupyter Engine 健康状态
// @Summary 检查 Jupyter Engine 健康状态 | Check Jupyter Engine health status
// @Tags Notebook
// @Produce json
// @Success 200 {object} map[string]string "健康状态 | Health status"
// @Router /notebooks/health [get]
func (h *NotebookHandler) HealthCheck(c *gin.Context) {
	err := h.jupyterService.HealthCheck(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "Jupyter Engine 运行正常",
	})
}

// UploadNotebookRequest 上传 Notebook 请求
type UploadNotebookRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	DataSources []uint                 `json:"data_sources"` // 数据源 engine IDs
	Parameters  map[string]interface{} `json:"parameters"`
	Kernel      string                 `json:"kernel"` // 默认 python3
	Tags        []string               `json:"tags"`
}

// UploadNotebook 上传 Notebook 文件并创建开发任务
// @Summary 上传 Notebook | Upload Notebook
// @Tags Notebook
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Notebook 文件 (.ipynb) | Notebook file (.ipynb)"
// @Param name formData string true "Notebook 名称 | Notebook name"
// @Param description formData string false "描述 | Description"
// @Param data_sources formData string false "数据源 IDs (JSON 数组) | Data source IDs (JSON array)"
// @Param parameters formData string false "参数 (JSON 对象) | Parameters (JSON object)"
// @Success 200 {object} models.UploadNotebookSwaggerResponse "已上传的Notebook | Uploaded Notebook"
// @Router /notebooks/upload [post]
func (h *NotebookHandler) UploadNotebook(c *gin.Context) {
	// 获取用户信息
	tenantID, _ := c.Get("tenant_id")
	userID, _ := c.Get("user_id")

	// 读取表单数据
	name := c.PostForm("name")
	description := c.PostForm("description")
	dataSourcesStr := c.PostForm("data_sources")
	parametersStr := c.PostForm("parameters")
	kernel := c.PostForm("kernel")
	if kernel == "" {
		kernel = "python3"
	}

	// 解析数据源 IDs
	var dataSources []uint
	if dataSourcesStr != "" {
		if err := json.Unmarshal([]byte(dataSourcesStr), &dataSources); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 data_sources 格式"})
			return
		}
	}

	// 解析参数
	var parameters map[string]interface{}
	if parametersStr != "" {
		if err := json.Unmarshal([]byte(parametersStr), &parameters); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 parameters 格式"})
			return
		}
	}
	if parameters == nil {
		parameters = make(map[string]interface{})
	}

	// 读取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未上传文件或文件读取失败"})
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

	// 只存储文件名作为 notebook_path（相对路径）
	// Jupyter Engine 会根据 tenant_id 自动拼接完整路径：tenant_{tenant_id}/notebooks/{notebook_path}
	notebookPath := file.Filename

	// 生成完整 MinIO 路径用于保存文件
	minioPath := fmt.Sprintf("tenant_%d/notebooks/%s", tenantID, file.Filename)

	// 保存到 MinIO
	if h.notebookExecutionService != nil {
		if err := h.notebookExecutionService.SaveNotebookToMinIO(c.Request.Context(), minioPath, notebookContent); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存到 MinIO 失败: %v", err)})
			return
		}
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "NotebookExecutionService 未初始化"})
		return
	}

	// 创建开发任务
	content := models.DevTaskContent{
		"notebook_path": notebookPath, // 相对路径（仅文件名）
		"minio_path":    minioPath,
		"kernel":        kernel,
		"parameters":    parameters,
		"data_sources":  dataSources,
		"description":   description,
	}

	createReq := &models.CreateDevTaskRequest{
		Name:        name,
		DisplayName: name,
		DevType:     "script",
		Content:     content,
		Description: description,
		Timeout:     600, // 默认 10 分钟
	}

	devTask, err := h.devTaskService.CreateDevTask(createReq, tenantID.(uint), userID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("创建开发任务失败: %v", err)})
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
// @Router /notebooks/:id/download [get]
func (h *NotebookHandler) DownloadNotebook(c *gin.Context) {
	// 获取用户信息
	tenantID, _ := c.Get("tenant_id")

	// 获取开发任务 ID
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	// 查询开发任务
	devTask, err := h.devTaskService.GetDevTask(uri.ID, tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notebook 不存在"})
		return
	}

	if !isNotebookScript(devTask) {
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
	if h.notebookExecutionService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "NotebookExecutionService 未初始化"})
		return
	}

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
// @Router /notebooks [get]
func (h *NotebookHandler) ListNotebooks(c *gin.Context) {
	// 获取用户信息
	tenantID, _ := c.Get("tenant_id")

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
	items, total, err := h.devTaskService.ListNotebookScripts(&req, tenantID.(uint))
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

// DeleteNotebook 删除 Notebook
// @Summary 删除 Notebook | Delete Notebook
// @Tags Notebook
// @Param id path int true "DevTask ID | DevTask ID"
// @Success 200 {object} map[string]string "删除成功 | Deleted successfully"
// @Router /notebooks/:id [delete]
func (h *NotebookHandler) DeleteNotebook(c *gin.Context) {
	// 获取用户信息
	tenantID, _ := c.Get("tenant_id")

	// 获取开发任务 ID
	var uri struct {
		ID uint `uri:"id" binding:"required"`
	}
	if err := c.ShouldBindUri(&uri); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	// 查询开发任务
	devTask, err := h.devTaskService.GetDevTask(uri.ID, tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Notebook 不存在"})
		return
	}

	if !isNotebookScript(devTask) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不是 Notebook 类型"})
		return
	}

	// 删除开发任务（软删除）
	if err := h.devTaskService.DeleteDevTask(uri.ID, tenantID.(uint)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除失败: %v", err)})
		return
	}

	// TODO: 可选择从 MinIO 删除文件（暂时保留文件，仅软删除开发任务）

	c.JSON(http.StatusOK, gin.H{
		"message": "Notebook 删除成功",
	})
}

func isNotebookScript(devTask *models.DevTask) bool {
	if devTask == nil || devTask.DevType != "script" || devTask.Content == nil {
		return false
	}
	notebookPath, ok := devTask.Content["notebook_path"].(string)
	return ok && notebookPath != ""
}
