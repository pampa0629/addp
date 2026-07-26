package api

import (
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
// @Summary 查询数据表 | Query data table
// @Tags DataService
// @Accept json
// @Produce json
// @Param request body models.DataQueryRequest true "查询请求 | Query request"
// @Success 200 {object} models.DataQueryResponse "查询结果 | Query result"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /data/query [post]
func (h *DataServiceHandler) Query(c *gin.Context) {
	var req models.DataQueryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.queryService.Query(c.Request.Context(), &req)
	if err != nil {
		if data.IsInvalidResourceLocatorError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// Aggregate 执行聚合查询
// @Summary 聚合查询 | Aggregate query
// @Tags DataService
// @Accept json
// @Produce json
// @Param request body models.AggregationRequest true "聚合请求 | Aggregation request"
// @Success 200 {object} models.AggregationResponse "聚合结果 | Aggregation result"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /data/aggregate [post]
func (h *DataServiceHandler) Aggregate(c *gin.Context) {
	var req models.AggregationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.queryService.Aggregate(c.Request.Context(), &req)
	if err != nil {
		if data.IsInvalidResourceLocatorError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetTableStructure 获取表结构
// @Summary 获取表结构 | Get table structure
// @Tags DataService
// @Accept json
// @Produce json
// @Param locator query string true "ResourceLocator，必须指向 table item | ResourceLocator pointing to table item"
// @Success 200 {object} []models.ColumnInfo "表结构 | Table structure"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["service.definition.read"]
// @Router /data/structure [get]
func (h *DataServiceHandler) GetTableStructure(c *gin.Context) {
	locator := c.Query("locator")
	if locator == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "locator is required"})
		return
	}

	columns, err := h.queryService.GetTableStructure(c.Request.Context(), locator)
	if err != nil {
		if data.IsInvalidResourceLocatorError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"columns": columns})
}
