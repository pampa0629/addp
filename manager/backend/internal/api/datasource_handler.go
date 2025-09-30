package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DataSourceHandler struct {
	dataSourceService *service.DataSourceService
}

func NewDataSourceHandler(dataSourceService *service.DataSourceService) *DataSourceHandler {
	return &DataSourceHandler{
		dataSourceService: dataSourceService,
	}
}

// SyncFromSystem 从 System 模块同步存储引擎
func (h *DataSourceHandler) SyncFromSystem(c *gin.Context) {
	// 从请求头获取 token
	token := ""
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			token = parts[1]
		}
	}

	if err := h.dataSourceService.SyncFromSystem(token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "同步成功",
		"success": true,
	})
}

// List 获取数据源列表
func (h *DataSourceHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	dataSources, total, err := h.dataSourceService.List(page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  dataSources,
		"total": total,
		"page":  page,
		"page_size": pageSize,
	})
}

// GetByID 获取单个数据源
func (h *DataSourceHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的数据源ID"})
		return
	}

	dataSource, err := h.dataSourceService.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据源不存在"})
		return
	}

	c.JSON(http.StatusOK, dataSource)
}

// Delete 删除数据源
func (h *DataSourceHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的数据源ID"})
		return
	}

	if err := h.dataSourceService.Delete(uint(id)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}