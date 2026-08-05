package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/gin-gonic/gin"
)

type IAMSecurityPolicyResponse struct {
	Version                          int64     `json:"version"`
	AppliedVersion                   int64     `json:"applied_version"`
	PendingRestart                   bool      `json:"pending_restart"`
	AccessTokenTTLMinutes            int       `json:"access_token_ttl_minutes"`
	DelegatedAccessTokenTTLMinutes   int       `json:"delegated_access_token_ttl_minutes"`
	ResourceAccessTicketTTLMinutes   int       `json:"resource_access_ticket_ttl_minutes"`
	RefreshTokenTTLDays              int       `json:"refresh_token_ttl_days"`
	OAuthAuthorizationCodeTTLMinutes int       `json:"oauth_authorization_code_ttl_minutes"`
	OAuthDeviceCodeTTLMinutes        int       `json:"oauth_device_code_ttl_minutes"`
	OAuthDevicePollIntervalSeconds   int       `json:"oauth_device_poll_interval_seconds"`
	TenantInvitationTTLHours         int       `json:"tenant_invitation_ttl_hours"`
	EnrollmentTicketTTLMinutes       int       `json:"enrollment_ticket_ttl_minutes"`
	OAuthPublicRateLimitPerMinute    int       `json:"oauth_public_rate_limit_per_minute"`
	OAuthUserRateLimitPerMinute      int       `json:"oauth_user_rate_limit_per_minute"`
	UpdatedByPrincipalID             *string   `json:"updated_by_principal_id,omitempty"`
	UpdatedAt                        time.Time `json:"updated_at"`
}

type IAMUpdateSecurityPolicyRequest struct {
	Version                          int64 `json:"version"`
	AccessTokenTTLMinutes            int   `json:"access_token_ttl_minutes"`
	DelegatedAccessTokenTTLMinutes   int   `json:"delegated_access_token_ttl_minutes"`
	ResourceAccessTicketTTLMinutes   int   `json:"resource_access_ticket_ttl_minutes"`
	RefreshTokenTTLDays              int   `json:"refresh_token_ttl_days"`
	OAuthAuthorizationCodeTTLMinutes int   `json:"oauth_authorization_code_ttl_minutes"`
	OAuthDeviceCodeTTLMinutes        int   `json:"oauth_device_code_ttl_minutes"`
	OAuthDevicePollIntervalSeconds   int   `json:"oauth_device_poll_interval_seconds"`
	TenantInvitationTTLHours         int   `json:"tenant_invitation_ttl_hours"`
	EnrollmentTicketTTLMinutes       int   `json:"enrollment_ticket_ttl_minutes"`
	OAuthPublicRateLimitPerMinute    int   `json:"oauth_public_rate_limit_per_minute"`
	OAuthUserRateLimitPerMinute      int   `json:"oauth_user_rate_limit_per_minute"`
}

type iamSecurityPolicyService interface {
	Get(context.Context) (*iam.SecurityPolicy, error)
	Update(context.Context, iam.UpdateSecurityPolicyInput) (*iam.SecurityPolicy, error)
}

type IAMSecurityPolicyHandler struct{ service iamSecurityPolicyService }

func NewIAMSecurityPolicyHandler(service iamSecurityPolicyService) (*IAMSecurityPolicyHandler, error) {
	if service == nil {
		return nil, fmt.Errorf("%w: IAM security policy service is required", commonapi.ErrBadRequest)
	}
	return &IAMSecurityPolicyHandler{service: service}, nil
}

// Get godoc
// @Summary      查询 IAM 安全策略 | Get IAM security policy
// @Tags         IAM 安全策略 | IAM Security Policy
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} IAMSecurityPolicyResponse
// @Failure      401 {object} map[string]any
// @Failure      403 {object} map[string]any
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.security_policy.read"]
// @Router       /platform/security_policy [get]
func (h *IAMSecurityPolicyHandler) Get(c *gin.Context) {
	policy, err := h.service.Get(c.Request.Context())
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMSecurityPolicy(*policy))
}

// Update godoc
// @Summary      更新 IAM 安全策略 | Update IAM security policy
// @Description  保存新策略版本；策略在 System 受控重启后生效 | Saves a new policy version that takes effect after a controlled System restart
// @Tags         IAM 安全策略 | IAM Security Policy
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body IAMUpdateSecurityPolicyRequest true "IAM 安全策略 | IAM security policy"
// @Success      200 {object} IAMSecurityPolicyResponse
// @Failure      400 {object} map[string]any
// @Failure      401 {object} map[string]any
// @Failure      403 {object} map[string]any
// @Failure      409 {object} map[string]any
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["iam.security_policy.update"]
// @Router       /platform/security_policy [put]
func (h *IAMSecurityPolicyHandler) Update(c *gin.Context) {
	actorID, err := iamPlatformUserActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request IAMUpdateSecurityPolicyRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondIAMError(c, fmt.Errorf("%w: invalid IAM security policy request", commonapi.ErrBadRequest))
		return
	}
	policy, err := h.service.Update(c.Request.Context(), iam.UpdateSecurityPolicyInput{
		ExpectedVersion:                  request.Version,
		AccessTokenTTLMinutes:            request.AccessTokenTTLMinutes,
		DelegatedAccessTokenTTLMinutes:   request.DelegatedAccessTokenTTLMinutes,
		ResourceAccessTicketTTLMinutes:   request.ResourceAccessTicketTTLMinutes,
		RefreshTokenTTLDays:              request.RefreshTokenTTLDays,
		OAuthAuthorizationCodeTTLMinutes: request.OAuthAuthorizationCodeTTLMinutes,
		OAuthDeviceCodeTTLMinutes:        request.OAuthDeviceCodeTTLMinutes,
		OAuthDevicePollIntervalSeconds:   request.OAuthDevicePollIntervalSeconds,
		TenantInvitationTTLHours:         request.TenantInvitationTTLHours,
		EnrollmentTicketTTLMinutes:       request.EnrollmentTicketTTLMinutes,
		OAuthPublicRateLimitPerMinute:    request.OAuthPublicRateLimitPerMinute,
		OAuthUserRateLimitPerMinute:      request.OAuthUserRateLimitPerMinute,
		UpdatedByPrincipalID:             actorID,
		Audit:                            iamAuditMetadataWithStatus(c, http.StatusOK),
	})
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, mapIAMSecurityPolicy(*policy))
}

func mapIAMSecurityPolicy(policy iam.SecurityPolicy) IAMSecurityPolicyResponse {
	response := IAMSecurityPolicyResponse{
		Version: policy.Version, AppliedVersion: policy.AppliedVersion,
		PendingRestart:                   policy.Version != policy.AppliedVersion,
		AccessTokenTTLMinutes:            policy.AccessTokenTTLMinutes,
		DelegatedAccessTokenTTLMinutes:   policy.DelegatedAccessTokenTTLMinutes,
		ResourceAccessTicketTTLMinutes:   policy.ResourceAccessTicketTTLMinutes,
		RefreshTokenTTLDays:              policy.RefreshTokenTTLDays,
		OAuthAuthorizationCodeTTLMinutes: policy.OAuthAuthorizationCodeTTLMinutes,
		OAuthDeviceCodeTTLMinutes:        policy.OAuthDeviceCodeTTLMinutes,
		OAuthDevicePollIntervalSeconds:   policy.OAuthDevicePollIntervalSeconds,
		TenantInvitationTTLHours:         policy.TenantInvitationTTLHours,
		EnrollmentTicketTTLMinutes:       policy.EnrollmentTicketTTLMinutes,
		OAuthPublicRateLimitPerMinute:    policy.OAuthPublicRateLimitPerMinute,
		OAuthUserRateLimitPerMinute:      policy.OAuthUserRateLimitPerMinute,
		UpdatedAt:                        policy.UpdatedAt.UTC(),
	}
	if policy.UpdatedByPrincipalID != nil {
		value := strconv.FormatInt(*policy.UpdatedByPrincipalID, 10)
		response.UpdatedByPrincipalID = &value
	}
	return response
}
