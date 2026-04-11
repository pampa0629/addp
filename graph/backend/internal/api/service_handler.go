package api

import (
	"net/http"
	"strconv"

	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

// ServiceHandler 知识服务 API Handler
type ServiceHandler struct {
	knowledgeSvc *service.KnowledgeService
}

func NewServiceHandler(knowledgeSvc *service.KnowledgeService) *ServiceHandler {
	return &ServiceHandler{knowledgeSvc: knowledgeSvc}
}

// checkAccess 鉴权辅助：is_public 图谱无需 JWT，否则需要 JWT 中提取 tenantID
// 返回 (tenantID, ok)；ok=false 表示已写入错误响应
func (h *ServiceHandler) checkAccess(c *gin.Context, graphIDStr string) (graphID, tenantID uint, ok bool) {
	id, err := strconv.ParseUint(graphIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的图谱 ID"})
		return 0, 0, false
	}
	graphID = uint(id)

	// 先尝试从 JWT 获取 tenantID（中间件注入的）
	if tid, exists := c.Get("tenant_id"); exists {
		tenantID = tid.(uint)
		kg, err := h.knowledgeSvc.GetGraph(graphID, tenantID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "图谱不存在"})
			return 0, 0, false
		}
		_ = kg
		return graphID, tenantID, true
	}

	// 没有 JWT，检查是否 is_public
	kg, err := h.knowledgeSvc.GetGraphPublic(graphID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "图谱不存在"})
		return 0, 0, false
	}
	if !kg.IsPublic {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "该图谱需要认证访问"})
		return 0, 0, false
	}
	return graphID, kg.TenantID, true
}

// @Summary 按类型列出实体（分页）| List entities by type (paginated)
// @Tags         知识服务 | Knowledge Service
// @Param graphId path int true "图谱 ID | Graph ID"
// @Param type path string true "本体实体类型名称 | Ontology entity type name"
// @Param page query int false "页码（默认 1）| Page number (default 1)"
// @Param page_size query int false "每页大小（默认 20）| Page size (default 20)"
// @Router /kg/{graphId}/entities/{type} [get]
func (h *ServiceHandler) ListEntities(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	entityType := c.Param("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	entities, total, err := h.knowledgeSvc.ListEntitiesByType(
		c.Request.Context(), graphID, tenantID, entityType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	c.JSON(http.StatusOK, gin.H{
		"data":        entities,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// @Summary 获取实体详情 | Get entity detail
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/entities/{type}/{nodeId} [get]
func (h *ServiceHandler) GetEntity(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	nodeID := c.Param("nodeId")
	entity, err := h.knowledgeSvc.GetEntityDetail(c.Request.Context(), graphID, tenantID, nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, entity)
}

// @Summary 获取节点邻居 | Get node neighbors
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/nodes/{nodeId}/neighbors [get]
func (h *ServiceHandler) GetNeighbors(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	nodeID := c.Param("nodeId")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))

	result, err := h.knowledgeSvc.GetEntityNeighbors(c.Request.Context(), graphID, tenantID, nodeID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 路径查找 | Find paths
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/paths [post]
func (h *ServiceHandler) FindPaths(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	var req models.KSPathRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.knowledgeSvc.FindPaths(c.Request.Context(), graphID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 获取实体中心子图 | Get entity-centric subgraph
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/subgraph [post]
func (h *ServiceHandler) GetSubgraph(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	var req models.KSSubgraphRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := h.knowledgeSvc.GetSubgraph(c.Request.Context(), graphID, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 全文搜索实体 | Full-text search entities
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/search [get]
func (h *ServiceHandler) SearchEntities(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索关键词 q 不能为空"})
		return
	}
	entityType := c.Query("type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	entities, total, err := h.knowledgeSvc.SearchEntities(
		c.Request.Context(), graphID, tenantID, q, entityType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))
	c.JSON(http.StatusOK, gin.H{
		"data":        entities,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// @Summary 获取图谱本体描述 | Get graph ontology description
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/ontology [get]
func (h *ServiceHandler) GetOntology(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	result, err := h.knowledgeSvc.GetOntologyDescription(c.Request.Context(), graphID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 图谱统计信息 | Graph statistics
// @Tags         知识服务 | Knowledge Service
// @Router /kg/{graphId}/stats [get]
func (h *ServiceHandler) GetStats(c *gin.Context) {
	graphID, tenantID, ok := h.checkAccess(c, c.Param("graphId"))
	if !ok {
		return
	}
	result, err := h.knowledgeSvc.GetStats(c.Request.Context(), graphID, tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// optionalAuthMiddleware 尝试解析 JWT 但不强制要求（注入 tenant_id/user_id 如果 token 有效）
func optionalAuthMiddleware(systemServiceURL string) gin.HandlerFunc {
	required := commonAuth.SystemAuthMiddleware(systemServiceURL)
	return func(c *gin.Context) {
		// 若有 Authorization header，走正常鉴权；否则跳过
		if c.GetHeader("Authorization") != "" {
			required(c)
		} else {
			c.Next()
		}
	}
}
