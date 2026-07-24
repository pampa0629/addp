package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

const deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

type OAuthHandler struct {
	tokenService *service.TokenService
}

func NewOAuthHandler(tokenService *service.TokenService) *OAuthHandler {
	return &OAuthHandler{tokenService: tokenService}
}

// CreateAuthorizationRequest godoc
// @Summary      创建 OAuth 授权请求 | Create OAuth authorization request
// @Description  校验公共客户端、动态 loopback Redirect URI、Scope 和 PKCE，并创建五分钟有效的一次性浏览器授权请求 | Validate the public client, dynamic loopback redirect URI, scope, and PKCE, then create a one-time browser authorization request valid for five minutes
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        client_id formData string true "OAuth Client ID"
// @Param        redirect_uri formData string true "Redirect URI"
// @Param        scope formData string false "OAuth Scope"
// @Param        code_challenge formData string true "PKCE challenge"
// @Param        code_challenge_method formData string true "PKCE challenge method" Enums(S256)
// @Success      201 {object} models.OAuthAuthorizationRequestCreatedResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "public"
// @Router       /oauth/authorization_requests [post]
func (h *OAuthHandler) CreateAuthorizationRequest(c *gin.Context) {
	clientID, scope := c.PostForm("client_id"), c.PostForm("scope")
	response, err := h.tokenService.CreateAuthorizationRequest(
		clientID,
		c.PostForm("redirect_uri"),
		scope,
		c.PostForm("code_challenge"),
		c.PostForm("code_challenge_method"),
	)
	if err != nil {
		errorCode := service.OAuthErrorCode(err)
		if errors.Is(err, service.ErrInvalidPKCE) {
			errorCode = "invalid_request"
		}
		middleware.SetOAuthSecurityAudit(c, "oauth.authorization_request.failed", "failed", clientID, "", "", scope, errorCode)
		c.JSON(oauthRequestErrorStatus(err, http.StatusBadRequest), gin.H{"error": errorCode})
		return
	}
	middleware.SetOAuthSecurityAudit(c, "oauth.authorization_request.created", "created", clientID, "", "", scope, "")
	c.JSON(http.StatusCreated, response)
}

// GetAuthorizationRequest godoc
// @Summary      获取 OAuth 授权请求 | Get OAuth authorization request
// @Description  向当前已认证用户返回 System 已校验的待处理客户端和 Scope；失效请求不返回原始授权参数 | Return the pending client and scope already validated by System to the authenticated user; unavailable requests do not expose original authorization parameters
// @Tags         OAuth
// @Produce      json
// @Security     BearerAuth
// @Param        request_id path string true "Authorization Request ID"
// @Success      200 {object} models.OAuthAuthorizationRequestView
// @Failure      401 {object} models.ErrorResponse
// @Failure      410 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "authenticated"
// @Router       /oauth/authorization_requests/{request_id} [get]
func (h *OAuthHandler) GetAuthorizationRequest(c *gin.Context) {
	response, err := h.tokenService.GetAuthorizationRequest(c.Param("request_id"))
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, service.ErrAuthorizationRequestUnavailable) {
			status = http.StatusGone
		}
		c.JSON(status, gin.H{"error": service.OAuthErrorCode(err)})
		return
	}
	c.JSON(http.StatusOK, response)
}

// CancelAuthorizationRequest godoc
// @Summary      取消 OAuth 授权请求 | Cancel OAuth authorization request
// @Description  使用只向 CLI 返回一次的请求凭据幂等取消待处理授权请求 | Idempotently cancel a pending authorization request using the request secret returned once to the CLI
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        request_id path string true "Authorization Request ID"
// @Param        Authorization header string true "Bearer Authorization Request Secret"
// @Success      204
// @Failure      400 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "public"
// @Router       /oauth/authorization_requests/{request_id} [delete]
func (h *OAuthHandler) CancelAuthorizationRequest(c *gin.Context) {
	requestSecret := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	clientID, scope, cancelled, err := h.tokenService.CancelAuthorizationRequest(c.Param("request_id"), requestSecret)
	if err != nil {
		errorCode := service.OAuthErrorCode(err)
		middleware.SetOAuthSecurityAudit(c, "oauth.authorization_request.failed", "failed", "", "", "", "", errorCode)
		c.JSON(oauthRequestErrorStatus(err, http.StatusBadRequest), gin.H{"error": errorCode})
		return
	}
	event, result := "oauth.authorization_request.cancel_ignored", "ignored"
	if cancelled {
		event, result = "oauth.authorization_request.cancelled", "cancelled"
	}
	middleware.SetOAuthSecurityAudit(c, event, result, clientID, "", "", scope, "")
	c.Status(http.StatusNoContent)
}

// Authorize godoc
// @Summary      处理 OAuth 授权决定 | Handle OAuth authorization decision
// @Description  锁定短期 Authorization Request，复核当前用户、客户端和授权边界；批准时签发一次性 Authorization Code，拒绝时返回 access_denied 回跳 | Lock the short-lived authorization request and revalidate the current user, client, and authorization boundary; issue a one-time authorization code when approved or return an access_denied redirect when rejected
// @Tags         OAuth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.OAuthAuthorizationDecisionRequest true "授权决定 | Authorization decision"
// @Success      200 {object} models.OAuthAuthorizationResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Failure      410 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "authenticated"
// @Router       /oauth/authorizations [post]
func (h *OAuthHandler) Authorize(c *gin.Context) {
	var req models.OAuthAuthorizationDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.SetOAuthSecurityAudit(c, "oauth.authorization.failed", "failed", "", "", "", "", "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	userID := c.GetUint("user_id")
	result, err := h.tokenService.DecideAuthorization(userID, &req)
	if err != nil {
		errorCode := service.OAuthErrorCode(err)
		middleware.SetOAuthSecurityAudit(c, "oauth.authorization.failed", "failed", "", "", req.Decision, "", errorCode)
		status := oauthRequestErrorStatus(err, http.StatusGone)
		c.JSON(status, gin.H{"error": errorCode})
		return
	}
	event := "oauth.authorization.approved"
	if req.Decision == models.OAuthAuthorizationDecisionRejected {
		event = "oauth.authorization.rejected"
	}
	middleware.SetOAuthSecurityAudit(c, event, req.Decision, result.ClientID, "", req.Decision, result.Scope, "")
	c.JSON(http.StatusOK, models.OAuthAuthorizationResponse{RedirectURL: result.RedirectURL})
}

func oauthRequestErrorStatus(err error, unavailableStatus int) int {
	if errors.Is(err, service.ErrAuthorizationRequestUnavailable) {
		return unavailableStatus
	}
	if service.OAuthErrorCode(err) == "server_error" {
		return http.StatusInternalServerError
	}
	return http.StatusBadRequest
}

// DeviceCode godoc
// @Summary      创建设备授权 | Create device authorization
// @Description  为允许 Device Flow 的公共客户端创建短期 Device Code 和 User Code | Create a short-lived device code and user code for a public client allowed to use Device Flow
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        client_id formData string true "OAuth Client ID"
// @Param        scope formData string false "OAuth Scope"
// @Success      200 {object} models.DeviceAuthorizationResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "public"
// @Router       /oauth/device/code [post]
func (h *OAuthHandler) DeviceCode(c *gin.Context) {
	clientID, scope := c.PostForm("client_id"), c.PostForm("scope")
	response, err := h.tokenService.CreateDeviceAuthorization(clientID, scope)
	if err != nil {
		errorCode := service.OAuthErrorCode(err)
		middleware.SetOAuthSecurityAudit(c, "oauth.device.code.failed", "failed", clientID, "", "", scope, errorCode)
		c.JSON(http.StatusBadRequest, gin.H{"error": errorCode})
		return
	}
	middleware.SetOAuthSecurityAudit(c, "oauth.device.code.issued", "issued", clientID, "", "", scope, "")
	c.JSON(http.StatusOK, response)
}

// ApproveDevice godoc
// @Summary      批准设备授权 | Approve device authorization
// @Description  使用当前用户身份批准或拒绝待处理的 User Code | Approve or reject a pending user code with the current user identity
// @Tags         OAuth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.DeviceApprovalRequest true "设备授权确认 | Device authorization approval"
// @Success      204
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "authenticated"
// @Router       /oauth/device/authorizations [post]
func (h *OAuthHandler) ApproveDevice(c *gin.Context) {
	var req models.DeviceApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.SetOAuthSecurityAudit(c, "oauth.device.authorization.failed", "failed", "", "", "", "", "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if err := h.tokenService.ApproveDeviceAuthorization(c.GetUint("user_id"), req.UserCode, req.Approve); err != nil {
		errorCode := service.OAuthErrorCode(err)
		middleware.SetOAuthSecurityAudit(c, "oauth.device.authorization.failed", "failed", "", "", "", "", errorCode)
		c.JSON(http.StatusBadRequest, gin.H{"error": errorCode})
		return
	}
	event, result := "oauth.device.authorization.rejected", "rejected"
	if req.Approve {
		event, result = "oauth.device.authorization.approved", "approved"
	}
	middleware.SetOAuthSecurityAudit(c, event, result, "", "", "", "", "")
	c.Status(http.StatusNoContent)
}

// Token godoc
// @Summary      兑换 OAuth Token | Exchange OAuth token
// @Description  处理 Authorization Code、Device Code 或 Refresh Token grant，并返回严格轮换的 opaque Token | Handle authorization-code, device-code, or refresh-token grants and return strictly rotated opaque tokens
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        grant_type formData string true "OAuth grant type"
// @Param        client_id formData string true "OAuth Client ID"
// @Param        code formData string false "Authorization Code"
// @Param        redirect_uri formData string false "Redirect URI"
// @Param        code_verifier formData string false "PKCE verifier"
// @Param        device_code formData string false "Device Code"
// @Param        refresh_token formData string false "Refresh Token"
// @Success      200 {object} models.TokenResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "public"
// @Router       /oauth/token [post]
func (h *OAuthHandler) Token(c *gin.Context) {
	clientID := c.PostForm("client_id")
	grantType := c.PostForm("grant_type")
	var pair *service.IssuedTokenPair
	var err error
	switch grantType {
	case "authorization_code":
		pair, err = h.tokenService.ExchangeAuthorizationCode(clientID, c.PostForm("code"), c.PostForm("redirect_uri"), c.PostForm("code_verifier"))
	case deviceGrantType:
		pair, err = h.tokenService.ExchangeDeviceCode(clientID, c.PostForm("device_code"))
	case "refresh_token":
		pair, err = h.tokenService.RotateOAuthRefreshToken(c.PostForm("refresh_token"), clientID)
	default:
		err = service.ErrUnsupportedGrantType
	}
	if err != nil {
		statusCode := http.StatusBadRequest
		if errors.Is(err, service.ErrSlowDown) {
			statusCode = http.StatusTooManyRequests
		}
		errorCode := service.OAuthErrorCode(err)
		event := "oauth.token.failed"
		if errors.Is(err, service.ErrRefreshTokenReuse) {
			event = "oauth.token.refresh_reuse_detected"
		}
		middleware.SetOAuthSecurityAudit(c, event, "failed", clientID, grantType, "", "", errorCode)
		c.JSON(statusCode, gin.H{"error": errorCode})
		return
	}
	middleware.SetOAuthSecurityAudit(c, "oauth.token.issued", "issued", clientID, grantType, "", pair.Scope, "")
	c.JSON(http.StatusOK, models.TokenResponse{
		AccessToken:  pair.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    pair.AccessExpiresIn,
		RefreshToken: pair.RefreshToken,
		Scope:        pair.Scope,
	})
}

// Revoke godoc
// @Summary      撤销 OAuth Token | Revoke OAuth token
// @Description  使用 Refresh Token 撤销对应的整个 OAuth Refresh Token Family | Revoke the complete OAuth refresh-token family identified by a refresh token
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        client_id formData string true "OAuth Client ID"
// @Param        token formData string true "Refresh Token"
// @Success      200
// @Failure      429 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @x-addp-auth-mode "public"
// @Router       /oauth/revoke [post]
func (h *OAuthHandler) Revoke(c *gin.Context) {
	clientID := c.PostForm("client_id")
	if err := h.tokenService.RevokeOAuthRefreshToken(c.PostForm("token"), clientID); err != nil {
		middleware.SetOAuthSecurityAudit(c, "oauth.token.revoke_ignored", "ignored", clientID, "", "", "", "invalid_token")
	} else {
		middleware.SetOAuthSecurityAudit(c, "oauth.token.revoked", "revoked", clientID, "", "", "", "")
	}
	c.Status(http.StatusOK)
}
