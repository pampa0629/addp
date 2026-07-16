package api

import (
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

type AuthHandler struct {
	userService  *service.UserService
	tokenService *service.TokenService
	cfg          *config.Config
}

func NewAuthHandler(
	userService *service.UserService,
	tokenService *service.TokenService,
	cfg *config.Config,
) *AuthHandler {
	return &AuthHandler{
		userService:  userService,
		tokenService: tokenService,
		cfg:          cfg,
	}
}

// Login godoc
// @Summary      用户登录 | User login
// @Description  使用用户名和密码登录，返回 JWT token | Login with username and password, returns JWT token
// @Tags         认证 | Auth
// @Accept       json
// @Produce      json
// @Param        request body models.LoginRequest true "登录信息 | Login credentials"
// @Success      200 {object} models.LoginResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userService.Authenticate(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	pair, err := h.tokenService.IssueFirstParty(user)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgAccountUnavailable)})
		return
	}
	h.setRefreshCookie(c, pair.RefreshToken)

	c.JSON(http.StatusOK, models.LoginResponse{
		AccessToken: pair.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   pair.AccessExpiresIn,
	})
}

// Register godoc
// @Summary      用户注册 | User registration
// @Description  注册新用户（需开启公开注册）| Register new user (requires public registration enabled)
// @Tags         认证 | Auth
// @Accept       json
// @Produce      json
// @Param        request body models.UserCreateRequest true "注册信息 | Registration info"
// @Success      201 {object} models.User
// @Failure      400 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Router       /register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	if !h.cfg.AllowPublicRegistration {
		c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, sysi18n.MsgRegisterDisabled)})
		return
	}

	var req models.UserCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userService.Register(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, user)
}

// Refresh godoc
// @Summary      刷新 Token | Refresh token
// @Description  使用 HttpOnly Cookie 中的 Refresh Token 轮换并获取新的 Access Token | Rotate the refresh token from the HttpOnly cookie and issue a new access token
// @Tags         认证 | Auth
// @Accept       json
// @Produce      json
// @Success      200 {object} models.LoginResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(refreshCookieName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
		return
	}
	pair, err := h.tokenService.RotateWebRefreshToken(refreshToken)
	if err != nil {
		h.clearRefreshCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
		return
	}
	h.setRefreshCookie(c, pair.RefreshToken)
	c.JSON(http.StatusOK, models.LoginResponse{
		AccessToken: pair.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   pair.AccessExpiresIn,
	})
}

// Logout godoc
// @Summary      退出登录 | Logout
// @Description  撤销当前 Refresh Token Family 并清除 Cookie | Revoke the current refresh token family and clear the cookie
// @Tags         认证 | Auth
// @Produce      json
// @Success      204
// @Router       /logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	if refreshToken, err := c.Cookie(refreshCookieName); err == nil {
		_ = h.tokenService.RevokeRefreshToken(refreshToken)
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

// Context godoc
// @Summary      获取授权上下文 | Get authorization context
// @Description  将当前 Bearer Token 解析为权威用户、租户和授权上下文 | Resolve the current Bearer token into the authoritative user, tenant and authorization context
// @Tags         认证 | Auth
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.AuthorizationContext
// @Failure      401 {object} models.ErrorResponse
// @Router       /auth/context [get]
func (h *AuthHandler) Context(c *gin.Context) {
	value, exists := c.Get(middleware.AuthorizationContextKey)
	authorizationContext, ok := value.(*models.AuthorizationContext)
	if !exists || !ok || authorizationContext == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
		return
	}
	c.JSON(http.StatusOK, authorizationContext)
}

const refreshCookieName = "addp_refresh_token"

func (h *AuthHandler) setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, token, h.cfg.RefreshTokenExpireDays*24*60*60, "/api/v1/system", "", h.cfg.Env == "production", true)
}

func (h *AuthHandler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, "", -1, "/api/v1/system", "", h.cfg.Env == "production", true)
}
