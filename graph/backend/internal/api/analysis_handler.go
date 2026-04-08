package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

type AnalysisHandler struct {
	analysisSvc *service.AnalysisService
}

func NewAnalysisHandler(analysisSvc *service.AnalysisService) *AnalysisHandler {
	return &AnalysisHandler{analysisSvc: analysisSvc}
}

// GetCapabilities godoc
// @Summary      算法能力探测
// @Description  探测知识图谱支持的图算法能力（GDS/Cypher）
// @Tags         图算法分析
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} models.AlgorithmCapabilities
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/analysis/capabilities [get]
func (h *AnalysisHandler) GetCapabilities(c *gin.Context) {
	graphID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	caps, err := h.analysisSvc.CheckCapabilities(ctx, graphID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, caps)
}

// SyncSpatialLayers godoc
// @Summary      同步空间图层
// @Description  将本体中所有有效空间类型（含从父类型继承）的图层同步到 Neo4j，并注册已有节点（幂等）
// @Tags         图算法分析
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/analysis/sync-spatial [post]
func (h *AnalysisHandler) SyncSpatialLayers(c *gin.Context) {
	graphID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	synced, err := h.analysisSvc.SyncAllSpatialLayers(ctx, graphID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"synced_layers": synced,
		"count":         len(synced),
		"message":       "空间图层同步成功",
	})
}
// @Summary      执行图算法
// @Description  执行指定图算法（度中心性/K跳/最短路径/PageRank/Louvain/WCC/介数中心性）
// @Tags         图算法分析
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                         true "知识图谱 ID"
// @Param        request body models.AlgorithmRunRequest  true "算法执行请求"
// @Success      200 {object} models.AlgorithmResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/analysis/run [post]
func (h *AnalysisHandler) RunAlgorithm(c *gin.Context) {
	graphID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	var req models.AlgorithmRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	result, err := h.analysisSvc.RunAlgorithm(ctx, graphID, tenantID, &req)
	if err != nil {
		errMsg := err.Error()
		if errMsg == "GDS_UNAVAILABLE" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "该算法需要 Neo4j GDS 插件，当前实例未安装"})
			return
		}
		if strings.HasPrefix(errMsg, "UNKNOWN_ALGO:") {
			algo := strings.TrimPrefix(errMsg, "UNKNOWN_ALGO:")
			c.JSON(http.StatusBadRequest, gin.H{"error": "未知算法：" + algo})
			return
		}
		if ctx.Err() == context.DeadlineExceeded {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "算法执行超时（30s）"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行失败：" + errMsg})
		return
	}
	c.JSON(http.StatusOK, result)
}
