package api

import (
	commoni18n "github.com/addp/common/middleware/i18n"
	"net/http"

	commonAPI "github.com/addp/common/api"
	qualityi18n "github.com/addp/quality/i18n"
	"github.com/addp/quality/internal/service"
	"github.com/gin-gonic/gin"
)

type RuleHandler struct {
	service *service.RuleService
}

func NewRuleHandler(ruleService *service.RuleService) *RuleHandler {
	return &RuleHandler{service: ruleService}
}

type ruleDeleteRequest struct {
	Version int64 `json:"version"`
}

// @Summary 列出质量规则 | List quality rules
// @Tags QualityRule
// @Produce json
// @Param search query string false "名称或编码 | Name or code"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} qualityRuleListResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.read"]
// @Router /rules [get]
// @Security BearerAuth
func (h *RuleHandler) List(c *gin.Context) {
	page, pageSize := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.service.List(c.Request.Context(), getTenantID(c), c.Query("search"), page, pageSize)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": pageSize, "total_pages": totalPages(total, pageSize)})
}

// @Summary 创建质量规则 | Create quality rule
// @Tags QualityRule
// @Accept json
// @Produce json
// @Param request body service.RuleWriteRequest true "质量规则定义 | Quality rule definition"
// @Success 201 {object} qualityRuleResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.create"]
// @Router /rules [post]
// @Security BearerAuth
func (h *RuleHandler) Create(c *gin.Context) {
	var request service.RuleWriteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	result, err := h.service.Create(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 获取质量规则 | Get quality rule
// @Tags QualityRule
// @Produce json
// @Param id path int true "规则 ID | Rule ID"
// @Success 200 {object} qualityRuleResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.read"]
// @Router /rules/{id} [get]
// @Security BearerAuth
func (h *RuleHandler) Get(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	result, err := h.service.Get(c.Request.Context(), getTenantID(c), id)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 更新质量规则 | Update quality rule
// @Tags QualityRule
// @Accept json
// @Produce json
// @Param id path int true "规则 ID | Rule ID"
// @Param request body service.RuleWriteRequest true "完整质量规则定义 | Complete quality rule definition"
// @Success 200 {object} qualityRuleResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.update"]
// @Router /rules/{id} [put]
// @Security BearerAuth
func (h *RuleHandler) Update(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request service.RuleWriteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	result, err := h.service.Update(c.Request.Context(), getTenantID(c), getUserID(c), id, request)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 删除质量规则 | Delete quality rule
// @Tags QualityRule
// @Accept json
// @Produce json
// @Param id path int true "规则 ID | Rule ID"
// @Param request body ruleDeleteRequest true "删除版本 | Delete version"
// @Success 200 {object} qualityMessageResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 404 {object} qualityErrorResponse
// @Failure 409 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.delete"]
// @Router /rules/{id} [delete]
// @Security BearerAuth
func (h *RuleHandler) Delete(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	var request ruleDeleteRequest
	if err := commonAPI.BindOptionalJSONStrict(c, &request); err != nil {
		respondInvalidRequest(c, err.Error())
		return
	}
	if err := h.service.Delete(c.Request.Context(), getTenantID(c), id, request.Version); err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, qualityMessageResponse{Message: commoni18n.T(c, qualityi18n.MsgDeleted)})
}

// @Summary 搜索标准规则来源 | Search standard rule sources
// @Tags QualityRule
// @Produce json
// @Param keyword query string true "搜索词 | Search keyword"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} qualityElementCandidateListResponse
// @Failure 400 {object} qualityErrorResponse
// @Failure 500 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.read"]
// @Router /rules/element-candidates [get]
// @Security BearerAuth
func (h *RuleHandler) ListElementCandidates(c *gin.Context) {
	page, size := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.service.ListElementCandidates(c.Request.Context(), getTenantID(c), c.Query("keyword"), page, size)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}

// @Summary 查询引用规则的方案 | List plans referencing a rule
// @Tags QualityRule
// @Produce json
// @Param id path int true "规则 ID | Rule ID"
// @Param page query int false "页码 | Page"
// @Param page_size query int false "每页数量 | Page size"
// @Success 200 {object} qualityPlanListResponse
// @Failure 404 {object} qualityErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["quality.rule.read","quality.plan.read"]
// @Router /rules/{id}/plans [get]
// @Security BearerAuth
func (h *RuleHandler) Plans(c *gin.Context) {
	id, err := requiredPositiveID(c.Param("id"))
	if err != nil {
		respondInvalidRequest(c, "")
		return
	}
	page, size := pageParams(c.Query("page"), c.Query("page_size"))
	items, total, err := h.service.Plans(c.Request.Context(), getTenantID(c), id, page, size)
	if err != nil {
		respondQualityServiceError(c, err, qualityi18n.MsgRuleNotFound, qualityi18n.MsgInternal)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}
