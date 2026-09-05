package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type CreateProtectionExemptionRequest = models.CreateProtectionExemptionRequest
type RenewProtectionExemptionRequest = models.RenewProtectionExemptionRequest
type RevokeProtectionExemptionRequest = models.RevokeProtectionExemptionRequest
type ProtectionExemptionResponse = models.ProtectionExemptionResponse
type ProtectionExemptionListResponse = models.ProtectionExemptionListResponse

type ExemptionHandler struct{ exemptions *service.ExemptionService }

func NewExemptionHandler(exemptions *service.ExemptionService) *ExemptionHandler {
	return &ExemptionHandler{exemptions: exemptions}
}

// @Summary 保护豁免列表 | List protection exemptions
// @Description 分页返回当前租户显式、限时的出口明文豁免；可按受保护资源聚合筛选 | Return explicit time-bounded plaintext exemptions for the current tenant, optionally filtered by protection enrollment
// @Tags Protection Exemption
// @Produce json
// @Param enrollment_id query string false "受保护资源 ID | Protection enrollment ID"
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量，最大 100 | Page size, maximum 100"
// @Success 200 {object} ProtectionExemptionListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_exemption.read"]
// @Router /protection-exemptions [get]
// @Security BearerAuth
func (h *ExemptionHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	result, err := h.exemptions.List(c.Request.Context(), getTenantID(c), c.Query("enrollment_id"), int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 创建保护豁免 | Create protection exemption
// @Description 为正式敏感 Assessment 的一个已支持出口创建最长 30 天的租户级明文豁免，并原子发布带默认回落决策的新投影；不授予 Owner 资源访问权 | Create a tenant-wide plaintext exemption of at most 30 days for one supported outlet of a formal sensitive Assessment and atomically publish a projection with its default fallback; this grants no owner resource access
// @Tags Protection Exemption
// @Accept json
// @Produce json
// @Param request body CreateProtectionExemptionRequest true "创建请求 | Create request"
// @Success 201 {object} ProtectionExemptionResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_exemption.create"]
// @Router /protection-exemptions [post]
// @Security BearerAuth
func (h *ExemptionHandler) Create(c *gin.Context) {
	var request models.CreateProtectionExemptionRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.exemptions.Create(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 保护豁免详情 | Get protection exemption
// @Description 返回当前有效状态、当前修订和不可变历史 | Return effective state, current revision, and immutable history
// @Tags Protection Exemption
// @Produce json
// @Param id path string true "豁免 ID | Exemption ID"
// @Success 200 {object} ProtectionExemptionResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_exemption.read"]
// @Router /protection-exemptions/{id} [get]
// @Security BearerAuth
func (h *ExemptionHandler) Get(c *gin.Context) {
	result, err := h.exemptions.Get(c.Request.Context(), getTenantID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 续期保护豁免 | Renew protection exemption
// @Description 使用资源版本追加最长 30 天的 active 修订，并原子发布新的限时投影 | Append an active revision of at most 30 days with resource-version concurrency control and atomically publish a new time-bounded projection
// @Tags Protection Exemption
// @Accept json
// @Produce json
// @Param id path string true "豁免 ID | Exemption ID"
// @Param request body RenewProtectionExemptionRequest true "续期请求 | Renewal request"
// @Success 200 {object} ProtectionExemptionResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_exemption.update"]
// @Router /protection-exemptions/{id} [put]
// @Security BearerAuth
func (h *ExemptionHandler) Renew(c *gin.Context) {
	var request models.RenewProtectionExemptionRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.exemptions.Renew(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 撤销保护豁免 | Revoke protection exemption
// @Description 使用资源版本追加 revoked 修订并立即回落到 ProtectionPolicy 与 ProtectionBaseline | Append a revoked revision with resource-version concurrency control and immediately fall back to ProtectionPolicy and ProtectionBaseline
// @Tags Protection Exemption
// @Accept json
// @Produce json
// @Param id path string true "豁免 ID | Exemption ID"
// @Param request body RevokeProtectionExemptionRequest true "撤销请求 | Revoke request"
// @Success 200 {object} ProtectionExemptionResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_exemption.delete"]
// @Router /protection-exemptions/{id} [delete]
// @Security BearerAuth
func (h *ExemptionHandler) Revoke(c *gin.Context) {
	var request models.RevokeProtectionExemptionRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.exemptions.Revoke(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
