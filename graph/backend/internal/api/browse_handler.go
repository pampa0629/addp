package api

import (
	"fmt"
	"net/http"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

type BrowseHandler struct {
	neo4jSvc        *service.Neo4jService
	schemaInference *service.SchemaInferenceService
}

func NewBrowseHandler(neo4jSvc *service.Neo4jService, schemaInference *service.SchemaInferenceService) *BrowseHandler {
	return &BrowseHandler{neo4jSvc: neo4jSvc, schemaInference: schemaInference}
}

// GetSchema godoc
// @Summary      图谱 Schema
// @Description  获取知识图谱的节点标签和关系类型 Schema
// @Tags         图谱浏览
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} models.BrowseSchema
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/schema [get]
func (h *BrowseHandler) GetSchema(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	schema, err := h.neo4jSvc.GetSchema(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, schema)
}

// GetStats godoc
// @Summary      图谱统计
// @Description  获取知识图谱的节点数、关系数、按标签分组统计
// @Tags         图谱浏览
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} models.BrowseStats
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/stats [get]
func (h *BrowseHandler) GetStats(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	stats, err := h.neo4jSvc.GetStats(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

// GetOverview godoc
// @Summary      图谱概览
// @Description  获取图谱概览子图（采样约100条关系）
// @Tags         图谱浏览
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} models.SubgraphResult
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/overview [get]
func (h *BrowseHandler) GetOverview(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	result, err := h.neo4jSvc.GetOverview(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// SearchNodes godoc
// @Summary      全文搜索节点
// @Description  在知识图谱中全文搜索实体节点
// @Tags         图谱浏览
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                   true "知识图谱 ID"
// @Param        request body models.SearchRequest  true "搜索请求"
// @Success      200 {object} models.SubgraphResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/search [post]
func (h *BrowseHandler) SearchNodes(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.SearchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.neo4jSvc.SearchNodes(c.Request.Context(), id, tenantID, req.Query, req.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ExpandNode godoc
// @Summary      展开节点邻居
// @Description  获取指定节点的邻居节点和关系
// @Tags         图谱浏览
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                   true "知识图谱 ID"
// @Param        request body models.ExpandRequest  true "展开请求"
// @Success      200 {object} models.SubgraphResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/expand [post]
func (h *BrowseHandler) ExpandNode(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.ExpandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.neo4jSvc.ExpandNode(c.Request.Context(), id, tenantID, req.NodeID, req.Limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// FindPath godoc
// @Summary      最短路径查询
// @Description  查询两个节点之间的最短路径
// @Tags         图谱浏览
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                 true "知识图谱 ID"
// @Param        request body models.PathRequest  true "路径查询请求"
// @Success      200 {object} models.SubgraphResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id}/path [post]
func (h *BrowseHandler) FindPath(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.PathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.neo4jSvc.FindPath(c.Request.Context(), id, tenantID, req.SourceID, req.TargetID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *BrowseHandler) GetConstraints(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	constraints, err := h.neo4jSvc.GetConstraints(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, constraints)
}

// InferSchema GET /graphs/:id/infer-schema?ontology_id=1
func (h *BrowseHandler) InferSchema(c *gin.Context) {
	graphID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	var ontologyID *uint
	if v := c.Query("ontology_id"); v != "" {
		id := parseUintParam(c, "ontology_id") // won't work from query
		// parse manually
		var oid uint
		if _, err := fmt.Sscanf(v, "%d", &oid); err == nil && oid > 0 {
			ontologyID = &oid
		}
		_ = id
	}

	preview, err := h.schemaInference.InferSchema(c.Request.Context(), graphID, tenantID, ontologyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, preview)
}

// ApplyInferredSchema POST /graphs/:id/infer-schema/apply
func (h *BrowseHandler) ApplyInferredSchema(c *gin.Context) {
	graphID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	var req service.ApplyInferredSchemaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := h.schemaInference.ApplyInferredSchema(c.Request.Context(), graphID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
