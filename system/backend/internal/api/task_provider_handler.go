package api

import (
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonmodels "github.com/addp/common/models"
	sysi18n "github.com/addp/system/i18n"
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

// ListService godoc
// @Summary      读取 TaskProvider 模块角色 | Read TaskProvider module roles
// @Description  返回全部 Provider 声明，并按当前模块 Backend 租约动态解析可用性和有效端点池 | Returns all Provider declarations and dynamically resolves availability and the eligible endpoint pool from current module Backend leases
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array} commonmodels.TaskProvider
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/task-providers [get]
func (h *TaskProviderHandler) ListService(c *gin.Context) {
	providers, err := h.service.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, providers)
}

// GetService godoc
// @Summary      读取 TaskProvider 详情 | Read TaskProvider detail
// @Description  每次读取都重新解析当前有效 Backend 池；模块离线时仍返回声明但 available=false | Resolves the current valid Backend pool on every read; an offline module keeps its declaration with available=false
// @Tags         运行时注册 | Runtime Registry
// @Produce      json
// @Security     BearerAuth
// @Param        module_name path string true "模块名 | Module name"
// @Success      200 {object} commonmodels.TaskProvider
// @Failure      404 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.runtime_registry.read"]
// @Router       /runtime/task-providers/{module_name} [get]
func (h *TaskProviderHandler) GetService(c *gin.Context) {
	provider, err := h.service.GetByModuleName(c.Param("module_name"))
	if err != nil {
		if errors.Is(err, commonapi.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgTaskProviderNotFound)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, provider)
}

var _ = commonmodels.TaskProvider{}
var _ = models.ErrorResponse{}
