package api

import (
	"fmt"
	"net/http"

	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// NotebookHandler Notebook 开发 API 处理器
type NotebookHandler struct {
	jupyterService *service.JupyterService
}

// NewNotebookHandler 创建 Notebook 处理器
func NewNotebookHandler(jupyterService *service.JupyterService) *NotebookHandler {
	return &NotebookHandler{
		jupyterService: jupyterService,
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
// @Summary 获取 Jupyter Lab 访问 URL
// @Tags Notebook
// @Accept json
// @Produce json
// @Param body body GetJupyterURLRequest true "请求"
// @Success 200 {object} GetJupyterURLResponse
// @Router /api/develop/notebooks/jupyter-url [post]
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
// @Summary 执行 Notebook
// @Tags Notebook
// @Accept json
// @Produce json
// @Param body body ExecuteNotebookRequest true "执行请求"
// @Success 200 {object} ExecuteNotebookResponse
// @Router /api/develop/notebooks/execute [post]
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
// @Summary 列出可用的 Kernel
// @Tags Notebook
// @Produce json
// @Success 200 {object} ListKernelsResponse
// @Router /api/develop/notebooks/kernels [get]
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
// @Summary 检查 Jupyter Engine 健康状态
// @Tags Notebook
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/develop/notebooks/health [get]
func (h *NotebookHandler) HealthCheck(c *gin.Context) {
	err := h.jupyterService.HealthCheck(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "unhealthy",
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"message": "Jupyter Engine 运行正常",
	})
}
