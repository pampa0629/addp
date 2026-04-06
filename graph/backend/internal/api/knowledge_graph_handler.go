package api

import (
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
// @Summary      知识图谱列表
// @Description  获取当前租户的所有知识图谱实例
// @Tags         知识图谱
// @Produce      json
// @Security     BearerAuth
// @Success      200 {array}  models.KnowledgeGraph
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs [get]
func (h *KnowledgeGraphHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	graphs, err := h.svc.List(tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, graphs)
}

// Get godoc
// @Summary      知识图谱详情
// @Description  获取知识图谱实例详情
// @Tags         知识图谱
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} models.KnowledgeGraph
// @Failure      404 {object} models.ErrorResponse
// @Router       /graphs/{id} [get]
func (h *KnowledgeGraphHandler) Get(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	kg, err := h.svc.GetByID(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "knowledge graph not found"})
		return
	}
	c.JSON(http.StatusOK, kg)
}

// Create godoc
// @Summary      创建知识图谱
// @Description  创建新的知识图谱实例，绑定本体和 Neo4j 引擎
// @Tags         知识图谱
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.CreateKnowledgeGraphRequest true "创建知识图谱请求"
// @Success      201 {object} models.KnowledgeGraph
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs [post]
func (h *KnowledgeGraphHandler) Create(c *gin.Context) {
	tenantID := getTenantID(c)
	var req models.CreateKnowledgeGraphRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kg, err := h.svc.Create(tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, kg)
}

// Update godoc
// @Summary      更新知识图谱
// @Description  更新知识图谱的名称、描述或状态
// @Tags         知识图谱
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path int                                   true "知识图谱 ID"
// @Param        request body models.UpdateKnowledgeGraphRequest   true "更新知识图谱请求"
// @Success      200 {object} models.KnowledgeGraph
// @Failure      400 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id} [put]
func (h *KnowledgeGraphHandler) Update(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	var req models.UpdateKnowledgeGraphRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	kg, err := h.svc.Update(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, kg)
}

// Delete godoc
// @Summary      删除知识图谱
// @Description  删除知识图谱实例
// @Tags         知识图谱
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "知识图谱 ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /graphs/{id} [delete]
func (h *KnowledgeGraphHandler) Delete(c *gin.Context) {
	id := parseUintParam(c, "id")
	tenantID := getTenantID(c)
	if err := h.svc.Delete(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}
