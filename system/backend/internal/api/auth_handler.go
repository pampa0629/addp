package api

import (
	"errors"
	"net/http"
	"strings"

	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/middleware"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
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
// @Description  使用用户名和密码登录，返回 opaque Access Token，并设置 HttpOnly Refresh Token Cookie 与 Owner Path 限定的 Browser Resource Access Ticket Cookie | Login with username and password, return an opaque access token, and set an HttpOnly refresh token cookie plus owner-path browser resource access ticket cookies
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
	h.setWebSessionCookies(c, pair)

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
// @Description  使用 HttpOnly Cookie 中的 Refresh Token 轮换 Access Token、Refresh Token 和 Browser Resource Access Ticket | Rotate the access token, refresh token, and browser resource access tickets using the refresh token from the HttpOnly cookie
// @Tags         认证 | Auth
// @Accept       json
// @Produce      json
// @Success      200 {object} models.LoginResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /refresh [post]
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(refreshCookieName)
	if err != nil {
		h.clearResourceAccessTicketCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
		return
	}
	pair, err := h.tokenService.RotateWebRefreshToken(refreshToken)
	if err != nil {
		h.clearRefreshCookie(c)
		h.clearResourceAccessTicketCookies(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
		return
	}
	h.setWebSessionCookies(c, pair)
	c.JSON(http.StatusOK, models.LoginResponse{
		AccessToken: pair.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   pair.AccessExpiresIn,
	})
}

// Logout godoc
// @Summary      退出登录 | Logout
// @Description  撤销当前 Refresh Token Family，并清除 Refresh Token 与 Browser Resource Access Ticket Cookie | Revoke the current refresh token family and clear the refresh-token and browser-resource-ticket cookies
// @Tags         认证 | Auth
// @Produce      json
// @Success      204
// @Router       /logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	if refreshToken, err := c.Cookie(refreshCookieName); err == nil {
		_ = h.tokenService.RevokeRefreshToken(refreshToken)
	}
	h.clearRefreshCookie(c)
	h.clearResourceAccessTicketCookies(c)
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

// CreateDelegation godoc
// @Summary      签发受委托访问令牌 | Issue delegated access token
// @Description  为当前用户的一次 ADDP Tool 调用签发绑定 owner audience、稳定 Tool Scope、AgentRun 和 ToolCall 的短期 opaque Token | Issue a short-lived opaque token for one ADDP Tool call, bound to the owner audience, stable Tool scope, AgentRun and ToolCall
// @Tags         认证 | Auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.DelegatedAccessTokenRequest true "委托边界 | Delegation boundary"
// @Success      201 {object} models.DelegatedAccessTokenResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Router       /auth/delegations [post]
func (h *AuthHandler) CreateDelegation(c *gin.Context) {
	var req models.DelegatedAccessTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return
	}
	sourceToken := bearerToken(c.GetHeader("Authorization"))
	if sourceToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidToken)})
		return
	}
	issued, err := h.tokenService.IssueDelegatedAccessToken(sourceToken, &req)
	if err != nil {
		statusCode := http.StatusInternalServerError
		messageID := sysi18n.MsgTokenGenFailed
		if errors.Is(err, service.ErrInvalidAccessToken) {
			statusCode = http.StatusUnauthorized
			messageID = sysi18n.MsgInvalidToken
		} else if errors.Is(err, service.ErrInvalidDelegation) || errors.Is(err, service.ErrInvalidScope) {
			statusCode = http.StatusBadRequest
			messageID = commoni18n.MsgInvalidParams
		}
		c.JSON(statusCode, gin.H{"error": commoni18n.T(c, messageID)})
		return
	}
	c.JSON(http.StatusCreated, issued)
}

func bearerToken(header string) string {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

const refreshCookieName = "addp_refresh_token"

func (h *AuthHandler) setRefreshCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, token, h.cfg.RefreshTokenExpireDays*24*60*60, "/api/v1/system", "", h.cfg.Env == "production", true)
}

func (h *AuthHandler) setWebSessionCookies(c *gin.Context, pair *service.IssuedTokenPair) {
	h.setRefreshCookie(c, pair.RefreshToken)
	maxAge := pair.ResourceAccessTicketExpiresIn
	if maxAge <= 0 {
		maxAge = pair.AccessExpiresIn
	}
	for _, owner := range models.BrowserResourceAccessOwners {
		token := pair.ResourceAccessTickets[owner]
		if token == "" {
			continue
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(models.BrowserResourceAccessTicketCookieName, token, maxAge, "/api/v1/"+owner, "", h.cfg.Env == "production", true)
	}
}

func (h *AuthHandler) clearRefreshCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(refreshCookieName, "", -1, "/api/v1/system", "", h.cfg.Env == "production", true)
}

func (h *AuthHandler) clearResourceAccessTicketCookies(c *gin.Context) {
	for _, owner := range models.BrowserResourceAccessOwners {
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(models.BrowserResourceAccessTicketCookieName, "", -1, "/api/v1/"+owner, "", h.cfg.Env == "production", true)
	}
}
