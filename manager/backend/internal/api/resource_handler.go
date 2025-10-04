package api

import (
	"net/http"
	"strconv"

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
// GET /api/resources?page=1&page_size=10&resource_type=postgresql
func (h *ResourceHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	resourceType := c.Query("resource_type")

	resources, total, err := h.resourceService.List(page, pageSize, resourceType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  resources,
		"total": total,
	})
}

// GetByID 获取单个资源
// GET /api/resources/:id
func (h *ResourceHandler) GetByID(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	resource, err := h.resourceService.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resource)
}
