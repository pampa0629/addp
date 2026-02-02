package api

import (
	"fmt"
	"net/http"

	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/service/data"
	"github.com/gin-gonic/gin"
)

type DataServiceHandler struct {
	queryService *data.QueryService
}

func NewDataServiceHandler(queryService *data.QueryService) *DataServiceHandler {
	return &DataServiceHandler{
		queryService: queryService,
	}
}

// Query 执行数据查询
// @Summary 查询数据表
// @Tags DataService
// @Accept json
// @Produce json
// @Param request body models.DataQueryRequest true "查询请求"
// @Success 200 {object} models.DataQueryResponse
// @Router /api/service/data/query [post]
func (h *DataServiceHandler) Query(c *gin.Context) {
	var req models.DataQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.queryService.Query(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// Aggregate 执行聚合查询
// @Summary 聚合查询
// @Tags DataService
// @Accept json
// @Produce json
// @Param request body models.AggregationRequest true "聚合请求"
// @Success 200 {object} models.AggregationResponse
// @Router /api/service/data/aggregate [post]
func (h *DataServiceHandler) Aggregate(c *gin.Context) {
	var req models.AggregationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.queryService.Aggregate(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetTableStructure 获取表结构
// @Summary 获取表结构
// @Tags DataService
// @Accept json
// @Produce json
// @Param engine_id query int true "引擎ID"
// @Param schema query string true "Schema名称"
// @Param table query string true "表名"
// @Success 200 {object} []models.ColumnInfo
// @Router /api/service/data/structure [get]
func (h *DataServiceHandler) GetTableStructure(c *gin.Context) {
	engineID := c.Query("engine_id")
	schema := c.Query("schema")
	table := c.Query("table")

	if engineID == "" || schema == "" || table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少必要参数: engine_id, schema, table"})
		return
	}

	// 转换 engine_id 为 uint
	var id uint
	if _, err := fmt.Sscanf(engineID, "%d", &id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的引擎ID"})
		return
	}

	columns, err := h.queryService.GetTableStructure(c.Request.Context(), id, schema, table)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"columns": columns})
}
