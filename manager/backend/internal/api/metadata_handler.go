package api

import (
	"net/http"
	"strconv"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type MetadataHandler struct {
	metadataService *service.MetadataService
}

func NewMetadataHandler(metadataService *service.MetadataService) *MetadataHandler {
	return &MetadataHandler{
		metadataService: metadataService,
	}
}

// ScanResource 扫描资源元数据
// POST /api/resources/:id/scan
func (h *MetadataHandler) ScanResource(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	result, err := h.metadataService.ScanResource(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

// GetTables 获取资源的表列表
// GET /api/resources/:id/tables?managed=true/false
func (h *MetadataHandler) GetTables(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	var isManaged *bool
	if managedStr := c.Query("managed"); managedStr != "" {
		managed := managedStr == "true"
		isManaged = &managed
	}

	tables, err := h.metadataService.GetTables(uint(id), isManaged)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  tables,
		"total": len(tables),
	})
}

// ManageTable 纳管表
// POST /api/tables/:id/manage
func (h *MetadataHandler) ManageTable(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table id"})
		return
	}

	if err := h.metadataService.ManageTable(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "table managed successfully"})
}

// UnmanageTable 取消纳管表
// POST /api/tables/:id/unmanage
func (h *MetadataHandler) UnmanageTable(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table id"})
		return
	}

	if err := h.metadataService.UnmanageTable(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "table unmanaged successfully"})
}
