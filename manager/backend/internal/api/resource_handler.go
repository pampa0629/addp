package api

import (
	"net/http"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type ResourceHandler struct {
	resourceService *service.ResourceService
}

func NewResourceHandler(resourceService *service.ResourceService) *ResourceHandler {
	return &ResourceHandler{
		resourceService: resourceService,
	}
}

// List 获取资源列表
// GET /api/engines?page=1&page_size=10&resource_type=postgresql
func (h *ResourceHandler) List(c *gin.Context) {
	page, pageSize := commonAPI.GetPaginationParams(c)
	resourceType := c.Query("resource_type")

	engines, total, err := h.resourceService.List(page, pageSize, resourceType)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	commonAPI.SendPaginatedResponse(c, engines, int64(total), page, pageSize)
}

// GetByID 获取单个资源
// GET /api/engines/:id
func (h *ResourceHandler) GetByID(c *gin.Context) {
	id, ok := commonAPI.ParseUintParam(c, "id")
	if !ok {
		return
	}

	resource, err := h.resourceService.GetByID(id)
	if err != nil {
		commonAPI.InternalServerError(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, resource)
}
