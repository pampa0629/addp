package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMAdministratorRecoveryHandler struct {
	service *iam.AdministratorCredentialRecoveryService
}
type IAMBeginAdministratorRecoveryRequest struct {
	RecoveryToken string `json:"recovery_token"`
}
type IAMCompleteAdministratorRecoveryRequest struct {
	EnrollmentToken string `json:"enrollment_token"`
	Password        string `json:"password"`
	Code            string `json:"code"`
}
type IAMAdministratorRecoveryEnrollmentResponse struct {
	EnrollmentToken string `json:"enrollment_token"`
	Secret          string `json:"secret"`
	OTPAuthURI      string `json:"otpauth_uri"`
	ExpiresAt       string `json:"expires_at"`
}

// Begin godoc
// @Summary 开始管理员凭据重新登记 | Begin administrator credential recovery enrollment
// @Description 使用双人批准的一次性恢复凭据，仅返回本次登记材料 | Consume a dual-approved recovery proof and return enrollment material once
// @Tags 平台权限治理 | Platform Authorization Governance
// @Accept json
// @Produce json
// @Param request body IAMBeginAdministratorRecoveryRequest true "恢复凭据 | Recovery proof"
// @Success 201 {object} IAMAdministratorRecoveryEnrollmentResponse
// @x-addp-auth-mode "public"
// @Router /auth/administrator-recovery/enrollments [post]
func (h *IAMAdministratorRecoveryHandler) Begin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var in IAMBeginAdministratorRecoveryRequest
	if err := commonapi.BindOptionalJSONStrict(c, &in); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.Begin(c.Request.Context(), in.RecoveryToken, iamAuditMetadataWithStatus(c, http.StatusCreated))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusCreated, IAMAdministratorRecoveryEnrollmentResponse{EnrollmentToken: result.EnrollmentToken, Secret: result.Secret, OTPAuthURI: result.OTPAuthURI, ExpiresAt: result.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")})
}

// Complete godoc
// @Summary 完成管理员凭据重新登记 | Complete administrator credential recovery enrollment
// @Description 验证新密码和TOTP后原子替换凭据，不签发会话或改变角色与暂停状态 | Atomically replace credentials after validating password and TOTP without issuing sessions or changing roles or suspension
// @Tags 平台权限治理 | Platform Authorization Governance
// @Accept json
// @Param request body IAMCompleteAdministratorRecoveryRequest true "新凭据和登记证明 | New credentials and enrollment proof"
// @Success 204
// @x-addp-auth-mode "public"
// @Router /auth/administrator-recovery/completions [post]
func (h *IAMAdministratorRecoveryHandler) Complete(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var in IAMCompleteAdministratorRecoveryRequest
	if err := commonapi.BindOptionalJSONStrict(c, &in); err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	if err := h.service.Complete(c.Request.Context(), in.EnrollmentToken, in.Password, in.Code, iamAuditMetadataWithStatus(c, http.StatusNoContent)); err != nil {
		respondIAMError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
