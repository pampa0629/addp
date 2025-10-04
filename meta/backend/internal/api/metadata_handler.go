package api

import (
	"net/http"
	"strconv"

	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/service"
	"github.com/gin-gonic/gin"
)

type MetadataHandler struct {
	metadataService *service.MetadataService
}

func NewMetadataHandler(metadataService *service.MetadataService) *MetadataHandler {
	return &MetadataHandler{metadataService: metadataService}
}

// ListDatasources 获取数据源列表
// GET /api/meta/datasources
func (h *MetadataHandler) ListDatasources(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	datasources, err := h.metadataService.ListDatasources(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": datasources})
}

// GetDatasource 获取数据源详情
// GET /api/meta/datasources/:id
func (h *MetadataHandler) GetDatasource(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	datasource, err := h.metadataService.GetDatasource(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "datasource not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": datasource})
}

// ListDatabases 获取数据库列表
// GET /api/meta/datasources/:id/databases
func (h *MetadataHandler) ListDatabases(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	datasourceIDStr := c.Param("id")
	datasourceID, err := strconv.ParseUint(datasourceIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid datasource_id"})
		return
	}

	databases, err := h.metadataService.ListDatabases(uint(datasourceID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": databases})
}

// GetDatabase 获取数据库详情
// GET /api/meta/databases/:id
func (h *MetadataHandler) GetDatabase(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	database, err := h.metadataService.GetDatabase(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "database not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": database})
}

// ListTables 获取表列表
// GET /api/meta/databases/:database_id/tables
func (h *MetadataHandler) ListTables(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	databaseIDStr := c.Param("id")
	databaseID, err := strconv.ParseUint(databaseIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid database_id"})
		return
	}

	tables, err := h.metadataService.ListTables(uint(databaseID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tables})
}

// GetTable 获取表详情
// GET /api/meta/tables/:id
func (h *MetadataHandler) GetTable(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	table, err := h.metadataService.GetTable(uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "table not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": table})
}

// ListFields 获取字段列表
// GET /api/meta/tables/:table_id/fields
func (h *MetadataHandler) ListFields(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	tableIDStr := c.Param("id")
	tableID, err := strconv.ParseUint(tableIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid table_id"})
		return
	}

	fields, err := h.metadataService.ListFields(uint(tableID), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": fields})
}

// ListSyncLogs 获取同步日志列表
// GET /api/meta/logs
func (h *MetadataHandler) ListSyncLogs(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	datasourceIDStr := c.Query("datasource_id")
	var datasourceID uint = 0
	if datasourceIDStr != "" {
		id, err := strconv.ParseUint(datasourceIDStr, 10, 32)
		if err == nil {
			datasourceID = uint(id)
		}
	}

	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)

	logs, err := h.metadataService.ListSyncLogs(datasourceID, tenantID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": logs})
}

// SearchTables 搜索表
// GET /api/meta/search/tables?keyword=xxx
func (h *MetadataHandler) SearchTables(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	keyword := c.Query("keyword")

	tables, err := h.metadataService.SearchTables(tenantID, keyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": tables})
}

// SearchFields 搜索字段
// GET /api/meta/search/fields?keyword=xxx
func (h *MetadataHandler) SearchFields(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)
	keyword := c.Query("keyword")

	fields, err := h.metadataService.SearchFields(tenantID, keyword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": fields})
}

// GetStats 获取元数据统计信息
// GET /api/meta/stats
func (h *MetadataHandler) GetStats(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	stats, err := h.metadataService.GetMetadataStats(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}
