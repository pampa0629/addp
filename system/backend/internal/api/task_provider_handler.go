package api

import (
	"errors"
	"net/http"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type TaskProviderHandler struct {
	service *service.TaskProviderService
}

func NewTaskProviderHandler(service *service.TaskProviderService) *TaskProviderHandler {
	return &TaskProviderHandler{service: service}
}

// RegisterOrUpdate 模块启动时调用此接口注册自己
// POST /api/task-providers/register
func (h *TaskProviderHandler) RegisterOrUpdate(c *gin.Context) {
	var req models.TaskProvider
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	provider, err := h.service.RegisterOrUpdate(&req)
	if err != nil {
		var validationErr *service.TaskProviderValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusBadRequest, gin.H{"error": validationErr.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, provider)
}

// List 查询所有启用的任务提供者
// GET /api/task-providers
func (h *TaskProviderHandler) List(c *gin.Context) {
	providers, err := h.service.ListEnabled()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, providers)
}

// Get 获取单个任务提供者详情
// GET /api/task-providers/:module_name
func (h *TaskProviderHandler) Get(c *gin.Context) {
	moduleName := c.Param("module_name")

	provider, err := h.service.GetByModuleName(moduleName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task provider not found"})
		return
	}

	c.JSON(http.StatusOK, provider)
}
