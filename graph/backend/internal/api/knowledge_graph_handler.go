package api

import (
	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	graphi18n "github.com/addp/graph/i18n"
	"net/http"

	"github.com/addp/graph/internal/models"
	"github.com/addp/graph/internal/service"
	"github.com/gin-gonic/gin"
)

type KnowledgeGraphHandler struct {
	svc *service.KnowledgeGraphService
}

func NewKnowledgeGraphHandler(svc *service.KnowledgeGraphService) *KnowledgeGraphHandler {
	return &KnowledgeGraphHandler{svc: svc}
}

// List godoc
// @Summary      知识图谱列表 | List knowledge graphs
// @Description  获取当前租户的所有知识图谱实例 | Get all knowledge graph instances for current tenant
// @Tags         知识图谱 | Knowledge Graph
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array}  models.KnowledgeGraph
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
// @Router       /graphs [get]
func (h *KnowledgeGraphHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	graphs, err := h.svc.List(tenantID)
	if err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, graphs)
}

// Get godoc
// @Summary      知识图谱详情 | Get knowledge graph detail
// @Description  获取知识图谱实例详情 | Get knowledge graph instance detail
// @Tags         知识图谱 | Knowledge Graph
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Success      200 {object} models.KnowledgeGraph
// @Failure      404 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.read"]
// @Router       /graphs/{id} [get]
func (h *KnowledgeGraphHandler) Get(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	kg, err := h.svc.GetByID(id, tenantID)
	if err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, kg)
}

// Create godoc
// @Summary      创建知识图谱 | Create knowledge graph
// @Description  创建新的知识图谱实例，绑定本体、业务图目录路径和索引管理范围 | Bind ontology, business graph catalog path and index management scope
// @Tags         知识图谱 | Knowledge Graph
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.CreateKnowledgeGraphRequest true "创建知识图谱请求 | Create knowledge graph request"
// @Success      201 {object} models.KnowledgeGraph
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.create"]
// @Failure 403 {object} models.ErrorResponse "数据保护拒绝 | Data protection denied"
// @Router       /graphs [post]
func (h *KnowledgeGraphHandler) Create(c *gin.Context) {
	tenantID := getTenantID(c)
	var req models.CreateKnowledgeGraphRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kg, err := h.svc.Create(c.Request.Context(), tenantID, &req)
	if err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusCreated, kg)
}

// Update godoc
// @Summary      更新知识图谱 | Update knowledge graph
// @Description  更新名称、描述、状态或知识服务公开状态；省略 is_public 保留原值，false 关闭公开 | Update name, description, status or knowledge-service public access; omit is_public to retain it, false disables public access
// @Tags         知识图谱 | Knowledge Graph
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                                   true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.UpdateKnowledgeGraphRequest   true "更新知识图谱请求 | Update knowledge graph request"
// @Success      200 {object} models.KnowledgeGraph
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.update"]
// @Router       /graphs/{id} [put]
func (h *KnowledgeGraphHandler) Update(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.UpdateKnowledgeGraphRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}
	kg, err := h.svc.Update(c.Request.Context(), id, tenantID, &req)
	if err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, kg)
}

// Delete godoc
// @Summary      删除知识图谱 | Delete knowledge graph
// @Description  删除知识图谱实例 | Delete a knowledge graph instance
// @Tags         知识图谱 | Knowledge Graph
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.GraphControlRequest true "当前版本 | Current version"
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Success      200 {object} models.SuccessResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.delete"]
// @Router       /graphs/{id} [delete]
func (h *KnowledgeGraphHandler) Delete(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.GraphControlRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id, tenantID, req.Version); err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, graphi18n.MsgGraphDeleted)})
}

// Reconcile godoc
// @Summary      核验关闭图谱的持久证据 | Reconcile durable evidence of a closed graph
// @Description  仅接受当前版本；证据不足保持阻断，不自动重跑 | Accept only the current version; insufficient evidence remains blocked without automatic rerun
// @Tags         知识图谱 | Knowledge Graph
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.GraphControlRequest true "当前版本 | Current version"
// @Success      200 {object} models.KnowledgeGraph
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.reconcile"]
// @Router       /graphs/{id}/reconcile [post]
func (h *KnowledgeGraphHandler) Reconcile(c *gin.Context) {
	var req models.GraphControlRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}
	kg, err := h.svc.Reconcile(c.Request.Context(), parseUintParam(c, "id"), getTenantID(c), commonauth.GetUserID(c), req.Version)
	if err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, kg)
}

// Reopen godoc
// @Summary      重新开放原物理图谱 | Reopen the original physical graph
// @Description  仅接受当前版本；证据不足保持阻断，不自动重跑 | Accept only the current version; insufficient evidence remains blocked without automatic rerun
// @Tags         知识图谱 | Knowledge Graph
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID | Knowledge graph ID"
// @Param        request body models.GraphControlRequest true "当前版本 | Current version"
// @Success      200 {object} models.KnowledgeGraph
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["graph.graph.update", "graph.graph.reconcile"]
// @Router       /graphs/{id}/reopen [post]
func (h *KnowledgeGraphHandler) Reopen(c *gin.Context) {
	if commonauth.GetUserID(c) == 0 {
		graphOperationError(c, commonapi.ErrForbidden)
		return
	}
	var req models.GraphControlRequest
	if err := commonapi.BindOptionalJSONStrict(c, &req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, graphi18n.MsgInvalidRequest)})
		return
	}
	kg, err := h.svc.Reopen(c.Request.Context(), parseUintParam(c, "id"), getTenantID(c), req.Version)
	if err != nil {
		graphOperationError(c, err)
		return
	}
	c.JSON(http.StatusOK, kg)
}
