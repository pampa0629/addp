package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
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
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
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

// RegisterOrUpdateService godoc
// @Summary      注册当前模块 TaskProvider | Register current module TaskProvider
// @Description  平台 Service Principal 只能发布与自身 OAuth Client 对应的 TaskProvider | A platform service principal can only publish the TaskProvider matching its OAuth client
// @Tags         运行时注册 | Runtime Registry
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.TaskProvider true "TaskProvider 契约 | TaskProvider contract"
// @Success      200 {object} models.TaskProvider
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.update"]
// @Router       /runtime/task-providers [post]
func (h *TaskProviderHandler) RegisterOrUpdateService(c *gin.Context) {
	var req models.TaskProvider
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := iamServiceOwnsModule(c, req.ModuleName); err != nil {
		respondIAMError(c, err)
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

// ListService godoc
// @Summary      读取 TaskProvider 注册表 | Read TaskProvider registry
// @Description  仅 Platform Runtime Service Principal 可读取已启用 TaskProvider | Only platform runtime service principals may read enabled TaskProviders
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} models.TaskProvider
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/task-providers [get]
func (h *TaskProviderHandler) ListService(c *gin.Context) { h.List(c) }

// GetService godoc
// @Summary      读取 TaskProvider 详情 | Read TaskProvider detail
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Param        module_name path string true "模块名 | Module name"
// @Success      200 {object} models.TaskProvider
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/task-providers/{module_name} [get]
func (h *TaskProviderHandler) GetService(c *gin.Context) { h.Get(c) }
