package api

import (
	"fmt"
	"net/http"
	"strings"

	commoni18n "github.com/addp/common/middleware/i18n"
	graphi18n "github.com/addp/graph/i18n"
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

// GetBrowseSnapshot godoc
// @Summary      图谱浏览快照 | Get graph browse snapshot
// @Description  从同一组图事实返回 Schema、统计和聚合概览 | Return schema, statistics and aggregate overview derived from the same graph facts
// @Tags         图谱浏览 | Graph Browse
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} models.BrowseSnapshot
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
// @Router       /graphs/{id}/browse-snapshot [get]
func (h *BrowseHandler) GetBrowseSnapshot(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	snapshot, err := h.neo4jSvc.GetBrowseSnapshot(c.Request.Context(), id, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snapshot)
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

// ExpandTarget godoc
// @Summary      展开图视图目标 | Expand graph view target
// @Description  展开聚合桶的代表性实体，或按跳数和双预算展开实体局部子图 | Expand representative entities of an aggregate bucket, or an entity-centric subgraph with depth and node/relationship budgets
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
func (h *BrowseHandler) ExpandTarget(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.ExpandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgExpandTargetInvalid)})
		return
	}
	if err := validateExpandRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgExpandTargetInvalid)})
		return
	}
	result, err := h.neo4jSvc.Expand(c.Request.Context(), id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func validateExpandRequest(req *models.ExpandRequest) error {
	if req == nil {
		return fmt.Errorf("request is required")
	}
	switch req.Target.Kind {
	case "entity":
		if strings.TrimSpace(req.Target.ID) == "" {
			return fmt.Errorf("entity target id is required")
		}
	case "aggregate":
		if len(req.Target.Labels) == 0 {
			return fmt.Errorf("aggregate target labels are required")
		}
	default:
		return fmt.Errorf("unsupported target kind")
	}
	if req.Depth < 0 || req.Depth > service.MaxExpandDepth ||
		req.NodeLimit < 0 || req.NodeLimit > service.MaxExpandNodeLimit ||
		req.RelationshipLimit < 0 || req.RelationshipLimit > service.MaxExpandRelationshipLimit {
		return fmt.Errorf("expand budget is out of range")
	}
	return nil
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
