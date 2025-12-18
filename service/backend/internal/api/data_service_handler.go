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
