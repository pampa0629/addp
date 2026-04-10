package api

import (
	"net/http"

	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/pkg/utils"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	userService *service.UserService
	cfg         *config.Config
}

func NewAuthHandler(userService *service.UserService, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		userService: userService,
		cfg:         cfg,
	}
}

// Login godoc
// @Summary      用户登录
// @Description  使用用户名和密码登录，返回 JWT token
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request body models.LoginRequest true "登录信息"
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

	var tenantID uint
	if user.TenantID != nil {
		tenantID = *user.TenantID
	}
	token, err := utils.GenerateToken(user.ID, user.Username, tenantID, h.cfg.JWTSecret, h.cfg.TokenExpireMinutes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, sysi18n.MsgTokenGenFailed)})
		return
	}

	c.JSON(http.StatusOK, models.LoginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
	})
}

// Register godoc
// @Summary      用户注册
// @Description  注册新用户（需开启公开注册）
// @Tags         认证
// @Accept       json
// @Produce      json
// @Param        request body models.UserCreateRequest true "注册信息"
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
// @Summary      刷新 Token
// @Description  使用即将过期的 token 获取新的 token
// @Tags         认证
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.LoginResponse
// @Failure      401 {object} models.ErrorResponse
// @Router       /refresh [post]
// Refresh 刷新 JWT Token
// 接受即将过期或已过期的 token，返回新的 token
func (h *AuthHandler) Refresh(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgMissingAuthHeader)})
		return
	}

	const bearerPrefix = "Bearer "
	if len(authHeader) < len(bearerPrefix) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.T(c, sysi18n.MsgInvalidAuthFormat)})
		return
	}

	tokenString := authHeader[len(bearerPrefix):]

	claims, err := utils.ParseTokenAllowExpired(tokenString, h.cfg.JWTSecret)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": commoni18n.TWithDetail(c, sysi18n.MsgInvalidToken, err.Error())})
		return
	}

	newToken, err := utils.GenerateToken(claims.UserID, claims.Username, claims.TenantID, h.cfg.JWTSecret, h.cfg.TokenExpireMinutes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": commoni18n.T(c, sysi18n.MsgTokenRefreshFailed)})
		return
	}

	c.JSON(http.StatusOK, models.LoginResponse{
		AccessToken: newToken,
		TokenType:   "Bearer",
	})
}
