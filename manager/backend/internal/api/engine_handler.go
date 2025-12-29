package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type EngineHandler struct {
	engineService *service.EngineService
}

func NewEngineHandler(engineService *service.EngineService) *EngineHandler {
	return &EngineHandler{
		engineService: engineService,
	}
}

// List 获取资源列表
// GET /api/engines?page=1&page_size=10&resource_type=postgresql
func (h *EngineHandler) List(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	resourceType := c.Query("resource_type")

	engines, total, err := h.engineService.List(page, pageSize, resourceType)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	commonAPI.SendPaginatedResponse(c, engines, int64(total), page, pageSize)
}

// GetByID 获取单个资源
// GET /api/engines/:id
func (h *EngineHandler) GetByID(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	resource, err := h.engineService.GetByID(id)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, resource)
}
