package api

import (
	"net/http"
	"strconv"

	commonAPI "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/repository"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type RuleApplicationHandler struct {
	svc *service.RuleEngineService
}

func NewRuleApplicationHandler(svc *service.RuleEngineService) *RuleApplicationHandler {
	return &RuleApplicationHandler{svc: svc}
}

// @Summary 获取规则应用列表 | List rule applications
// @Tags RuleApplication
// @Produce json
// @Param engine_id query int false "引擎ID | Engine ID"
// @Param schema_name query string false "Schema名称 | Schema name"
// @Param table_name query string false "表名 | Table name"
// @Param page query int false "页码 | Page" default(1)
// @Param page_size query int false "每页数量 | Page size" default(20) maximum(100)
// @Success 200 {object} qualityRuleApplicationListResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule_application.read"]
// @Router /rule-applications [get]
// @Security BearerAuth
func (h *RuleApplicationHandler) List(c *gin.Context) {
	tenantID := getTenantID(c)
	engineID, err := optionalPositiveID(c.Query("engine_id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	schemaName := c.Query("schema_name")
	tableName := c.Query("table_name")

	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.svc.ListRuleApplications(repository.RuleApplicationListOptions{TenantID: tenantID, EngineID: engineID, SchemaName: schemaName, TableName: tableName, Page: page, PageSize: pageSize})
	if err != nil {
		respondQualityServiceError(c, err, "", qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 获取规则应用详情 | Get rule application detail
// @Tags RuleApplication
// @Produce json
// @Param id path int true "规则应用ID | Rule application ID"
// @Success 200 {object} qualityRuleApplicationResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule_application.read"]
// @Router /rule-applications/{id} [get]
// @Security BearerAuth
func (h *RuleApplicationHandler) Get(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	item, err := h.svc.GetRuleApplication(id, tenantID)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleAppNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 创建规则应用 | Create rule application
// @Tags RuleApplication
// @Accept json
// @Produce json
// @Param body body service.CreateRuleApplicationRequest true "规则应用信息 | Rule application info"
// @Success 201 {object} qualityRuleApplicationResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule_application.create"]
// @Router /rule-applications [post]
// @Security BearerAuth
func (h *RuleApplicationHandler) Create(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	var req service.CreateRuleApplicationRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	item, err := h.svc.CreateRuleApplication(c.Request.Context(), tenantID, userID, &req)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleAppNotFound, qualityi18n.MsgRuleAppCreateFailed)
		return
	}
	c.JSON(http.StatusCreated, item)
}

// @Summary 更新规则应用 | Update rule application
// @Tags RuleApplication
// @Accept json
// @Produce json
// @Param id path int true "规则应用ID | Rule application ID"
// @Param body body service.UpdateRuleApplicationRequest true "更新信息 | Update info"
// @Success 200 {object} qualityRuleApplicationResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule_application.update"]
// @Router /rule-applications/{id} [put]
// @Security BearerAuth
func (h *RuleApplicationHandler) Update(c *gin.Context) {
	tenantID := getTenantID(c)
	userID := getUserID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var req service.UpdateRuleApplicationRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &req); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	item, err := h.svc.UpdateRuleApplication(id, tenantID, userID, &req)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleAppNotFound, qualityi18n.MsgRuleAppUpdateFailed)
		return
	}
	c.JSON(http.StatusOK, item)
}

// @Summary 删除规则应用 | Delete rule application
// @Tags RuleApplication
// @Produce json
// @Param id path int true "规则应用ID | Rule application ID"
// @Success 200 {object} qualityMessageResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule_application.delete"]
// @Router /rule-applications/{id} [delete]
// @Security BearerAuth
func (h *RuleApplicationHandler) Delete(c *gin.Context) {
	tenantID := getTenantID(c)
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	if err := h.svc.DeleteRuleApplication(id, tenantID); err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleAppNotFound, qualityi18n.MsgRuleAppDeleteFailed)
		return
	}
	c.JSON(http.StatusOK, qualityMessageResponse{Message: commoni18n.T(c, qualityi18n.MsgDeleted)})
}
