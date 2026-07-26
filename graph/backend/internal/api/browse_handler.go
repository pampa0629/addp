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
// @Summary      图谱 Schema | Get graph schema
// @Description  获取知识图谱的节点形状、关系形状和连接模式 Schema | Get node shape, relationship shape and pattern schema of a knowledge graph
// @Tags         图谱浏览 | Graph Browse
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} models.BrowseSchema
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
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
// @Summary      图谱统计 | Get graph statistics
// @Description  获取知识图谱的节点数、关系数、按标签分组统计 | Get node count, relation count and label-grouped statistics of a knowledge graph
// @Tags         图谱浏览 | Graph Browse
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} models.BrowseStats
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
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
// @Summary      图谱概览 | Get graph overview
// @Description  获取图谱概览子图（采样约100条关系）| Get graph overview subgraph (sampling ~100 relations)
// @Tags         图谱浏览 | Graph Browse
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} models.SubgraphResult
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
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
// @Summary      全文搜索节点 | Full-text search nodes
// @Description  在知识图谱中全文搜索实体节点 | Full-text search entity nodes in a knowledge graph
// @Tags         图谱浏览 | Graph Browse
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                   true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.SearchRequest  true "搜索请求 | Search request"
// @Success      200 {object} models.SubgraphResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
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
// @Summary      展开节点邻居 | Expand node neighbors
// @Description  获取指定节点的邻居节点和关系 | Get neighbor nodes and relations of a specified node
// @Tags         图谱浏览 | Graph Browse
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                   true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.ExpandRequest  true "展开请求 | Expand request"
// @Success      200 {object} models.SubgraphResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
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
// @Summary      最短路径查询 | Find shortest path
// @Description  查询两个节点之间的最短路径 | Find the shortest path between two nodes
// @Tags         图谱浏览 | Graph Browse
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                 true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.PathRequest  true "路径查询请求 | Path query request"
// @Success      200 {object} models.SubgraphResult
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
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

// GetConstraints godoc
// @Summary      获取图约束 | Get graph constraints
// @Tags         图谱浏览 | Graph Browse
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
// @Router       /graphs/{id}/constraints [get]
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

// InferSchema godoc
// @Summary      推断图谱 Schema | Infer graph schema
// @Tags         图谱浏览 | Graph Browse
// @Produce      json
// @Security     BearerAuth
// @Param        id          path  int true  "知识图谱 ID | Knowledge graph ID"
// @Param        ontology_id query int false "本体 ID | Ontology ID"
// @Success      200 {object} map[string]interface{}
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
// @Router       /graphs/{id}/infer-schema [get]
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

// ApplyInferredSchema godoc
// @Summary      应用推断 Schema | Apply inferred schema
// @Tags         图谱浏览 | Graph Browse
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.ApplyInferredSchemaRequest true "应用请求 | Apply request"
// @Success      200 {object} map[string]interface{}
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.update"]
// @Router       /graphs/{id}/infer-schema/apply [post]
func (h *BrowseHandler) ApplyInferredSchema(c *gin.Context) {
	graphID := parseUintParam(c, "id")
	tenantID := getTenantID(c)

	var req models.ApplyInferredSchemaRequest
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
