package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type RevokeProtectionExemptionRequest = models.RevokeProtectionExemptionRequest
type ProtectionExemptionResponse = models.ProtectionExemptionResponse
type ProtectionExemptionListResponse = models.ProtectionExemptionListResponse

type ExemptionHandler struct{ exemptions *service.ExemptionService }

func NewExemptionHandler(exemptions *service.ExemptionService) *ExemptionHandler {
	return &ExemptionHandler{exemptions: exemptions}
}

// @Summary 临时原值授权列表 | List temporary plaintext grants
// @Description 分页返回经申请和审批形成的按用户、字段、出口、时限约束的原值授权；可按受保护资源聚合筛选 | Return approved plaintext grants scoped by user, field, outlet, and deadline, optionally filtered by protection enrollment
// @Tags Protection Access Grant
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

// @Summary 临时原值授权详情 | Get temporary plaintext grant
// @Description 返回当前有效状态、当前修订和不可变历史 | Return effective state, current revision, and immutable history
// @Tags Protection Access Grant
// @Produce json
// @Param id path string true "授权 ID | Grant ID"
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

// @Summary 撤销临时原值授权 | Revoke temporary plaintext grant
// @Description 使用资源版本追加 revoked 修订并立即回落到默认或字段保护规则 | Append a revoked revision with resource-version concurrency control and immediately fall back to the default or field protection rule
// @Tags Protection Access Grant
// @Accept json
// @Produce json
// @Param id path string true "授权 ID | Grant ID"
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
