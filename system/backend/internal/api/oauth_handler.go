package api

import (
	"errors"
	"net/http"

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

// Authorize godoc
// @Summary      批准 OAuth 授权 | Approve OAuth authorization
// @Description  使用当前用户身份签发一次性 PKCE Authorization Code | Issue a one-time PKCE authorization code for the current user
// @Tags         OAuth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.OAuthAuthorizationRequest true "授权请求 | Authorization request"
// @Success      200 {object} models.OAuthAuthorizationResponse
// @Failure      400 {object} models.ErrorResponse
// @Router       /oauth/authorizations [post]
func (h *OAuthHandler) Authorize(c *gin.Context) {
	var req models.OAuthAuthorizationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	userID := c.GetUint("user_id")
	redirectURL, err := h.tokenService.CreateAuthorizationCode(userID, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.OAuthErrorCode(err)})
		return
	}
	c.JSON(http.StatusOK, models.OAuthAuthorizationResponse{RedirectURL: redirectURL})
}

// DeviceCode godoc
// @Summary      创建设备授权 | Create device authorization
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        client_id formData string true "OAuth Client ID"
// @Param        scope formData string false "OAuth Scope"
// @Success      200 {object} models.DeviceAuthorizationResponse
// @Failure      400 {object} models.ErrorResponse
// @Router       /oauth/device/code [post]
func (h *OAuthHandler) DeviceCode(c *gin.Context) {
	response, err := h.tokenService.CreateDeviceAuthorization(c.PostForm("client_id"), c.PostForm("scope"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.OAuthErrorCode(err)})
		return
	}
	c.JSON(http.StatusOK, response)
}

// ApproveDevice godoc
// @Summary      批准设备授权 | Approve device authorization
// @Tags         OAuth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.DeviceApprovalRequest true "设备授权确认 | Device authorization approval"
// @Success      204
// @Failure      400 {object} models.ErrorResponse
// @Router       /oauth/device/authorizations [post]
func (h *OAuthHandler) ApproveDevice(c *gin.Context) {
	var req models.DeviceApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if err := h.tokenService.ApproveDeviceAuthorization(c.GetUint("user_id"), req.UserCode, req.Approve); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": service.OAuthErrorCode(err)})
		return
	}
	c.Status(http.StatusNoContent)
}

// Token godoc
// @Summary      兑换 OAuth Token | Exchange OAuth token
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
// @Router       /oauth/token [post]
func (h *OAuthHandler) Token(c *gin.Context) {
	clientID := c.PostForm("client_id")
	var pair *service.IssuedTokenPair
	var err error
	switch c.PostForm("grant_type") {
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
		c.JSON(statusCode, gin.H{"error": service.OAuthErrorCode(err)})
		return
	}
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
// @Tags         OAuth
// @Accept       application/x-www-form-urlencoded
// @Produce      json
// @Param        token formData string true "Refresh Token"
// @Success      200
// @Router       /oauth/revoke [post]
func (h *OAuthHandler) Revoke(c *gin.Context) {
	_ = h.tokenService.RevokeRefreshToken(c.PostForm("token"))
	c.Status(http.StatusOK)
}
