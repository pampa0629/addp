package api

import (
	"net/http"
	"strconv"

	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type RuleApplicationHandler struct {
	svc *service.RuleEngineService
}

func NewRuleApplicationHandler(svc *service.RuleEngineService) *RuleApplicationHandler {
	return &RuleApplicationHandler{svc: svc}
}

// @Summary 获取规则应用列表
// @Tags RuleApplication
// @Produce json
// @Param engine_id query int false "引擎ID"
// @Param schema_name query string false "Schema名称"
// @Param table_name query string false "表名"
// @Success 200 {array} map[string]interface{}
// @Router /rule-applications [get]
// @Security BearerAuth
func (h *RuleApplicationHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	engineID, _ := strconv.ParseInt(c.Query("engine_id"), 10, 64)
	schemaName := c.Query("schema_name")
	tableName := c.Query("table_name")

	items, err := h.svc.ListRuleApplications(tenantID, engineID, schemaName, tableName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, items)
}

// @Summary 获取规则应用详情
// @Tags RuleApplication
// @Produce json
// @Param id path int true "规则应用ID"
// @Success 200 {object} map[string]interface{}
// @Router /rule-applications/{id} [get]
// @Security BearerAuth
func (h *RuleApplicationHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	item, err := h.svc.GetRuleApplication(id, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 创建规则应用
// @Tags RuleApplication
// @Accept json
// @Produce json
// @Param body body map[string]interface{} true "规则应用信息"
// @Success 201 {object} map[string]interface{}
// @Router /rule-applications [post]
// @Security BearerAuth
func (h *RuleApplicationHandler) Create(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	var req service.CreateRuleApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.svc.CreateRuleApplication(tenantID, userID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新规则应用
// @Tags RuleApplication
// @Accept json
// @Produce json
// @Param id path int true "规则应用ID"
// @Param body body map[string]interface{} true "更新信息"
// @Success 200 {object} map[string]interface{}
// @Router /rule-applications/{id} [put]
// @Security BearerAuth
func (h *RuleApplicationHandler) Update(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req service.UpdateRuleApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	item, err := h.svc.UpdateRuleApplication(id, tenantID, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除规则应用
// @Tags RuleApplication
// @Produce json
// @Param id path int true "规则应用ID"
// @Success 200 {object} map[string]string
// @Router /rule-applications/{id} [delete]
// @Security BearerAuth
func (h *RuleApplicationHandler) Delete(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.svc.DeleteRuleApplication(id, tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "删除成功"})
}
