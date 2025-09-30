package api

import (
	"net/http"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
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

	token, err := utils.GenerateToken(user.ID, user.Username, h.cfg.JWTSecret, h.cfg.TokenExpireMinutes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成令牌失败"})
		return
	}

	c.JSON(http.StatusOK, models.LoginResponse{
		AccessToken: token,
		TokenType:   "Bearer",
	})
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req models.UserCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userService.Create(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, user)
}