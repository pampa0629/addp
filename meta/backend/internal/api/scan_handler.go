package api

import (
	"net/http"
	"strconv"

	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type ScanHandler struct {
	scanService *service.ScanService
}

func NewScanHandler(scanService *service.ScanService) *ScanHandler {
	return &ScanHandler{scanService: scanService}
}

// DeepScanDatabase 深度扫描数据库
// POST /api/meta/scan/database/:database_id
func (h *ScanHandler) DeepScanDatabase(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	databaseIDStr := c.Param("database_id")
	databaseID, err := strconv.ParseUint(databaseIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid database_id"})
		return
	}

	if err := h.scanService.DeepScanDatabase(uint(databaseID), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Database deep scan completed successfully",
	})
}

// DeepScanTable 深度扫描表
// POST /api/meta/scan/table/:table_id
func (h *ScanHandler) DeepScanTable(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	tableIDStr := c.Param("table_id")
	tableID, err := strconv.ParseUint(tableIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table_id"})
		return
	}

	if err := h.scanService.DeepScanTable(uint(tableID), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Table deep scan completed successfully",
	})
}

// GetSchemas 获取数据源的Schema列表
// GET /api/meta/scan/schemas/:resource_id
func (h *ScanHandler) GetSchemas(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Param("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource_id"})
		return
	}

	schemas, err := h.scanService.GetSchemas(uint(resourceID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": schemas,
	})
}

// ScanMetadata 扫描元数据
// POST /api/meta/scan
func (h *ScanHandler) ScanMetadata(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	var req models.ScanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.scanService.ScanMetadata(&req, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
