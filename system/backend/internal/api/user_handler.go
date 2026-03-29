package api

import (
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/service"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

// Create godoc
// @Summary      创建用户
// @Description  创建新用户（租户管理员权限）
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.UserCreateRequest true "用户信息"
// @Success      200 {object} models.User
// @Failure      400 {object} models.ErrorResponse
// @Router       /users [post]
func (h *UserHandler) Create(c *gin.Context) {
	var req models.UserCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, 400, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	user, err := h.userService.Create(&req, userID)
	commonapi.RespondOrError(c, user, err)
}

// List godoc
// @Summary      获取用户列表
// @Description  分页获取用户列表（自动过滤租户）
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码" default(1)
// @Param        page_size query int false "每页数量" default(10)
// @Success      200 {object} object{data=[]models.User,total=int,page=int,page_size=int}
// @Failure      500 {object} models.ErrorResponse
// @Router       /users [get]
func (h *UserHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	userID, _ := commonapi.GetCurrentUserID(c)

	users, total, err := h.userService.List(page, pageSize, userID)
	if err != nil {
		commonapi.RespondError(c, 500, err.Error())
		return
	}
	commonapi.RespondPaginated(c, users, total, page, pageSize)
}

func (h *UserHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return // 错误已经在 BindIDParam 中处理
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	user, err := h.userService.GetByID(id, userID)
	commonapi.RespondOrError(c, user, err)
}

func (h *UserHandler) Update(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.UserUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, 400, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	user, err := h.userService.Update(id, &req, userID)
	commonapi.RespondOrError(c, user, err)
}

func (h *UserHandler) Delete(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	err = h.userService.Delete(id, userID)
	if err != nil {
		commonapi.RespondOrError(c, nil, err)
		return
	}
	commonapi.RespondSuccess(c, gin.H{"message": "删除成功"})
}

// Me godoc
// @Summary      获取当前用户信息
// @Description  获取当前登录用户的详细信息
// @Tags         用户管理
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200 {object} models.User
// @Failure      401 {object} models.ErrorResponse
// @Router       /users/me [get]
func (h *UserHandler) Me(c *gin.Context) {
	userID, _ := commonapi.GetCurrentUserID(c)
	user, err := h.userService.GetByID(userID, userID)
	commonapi.RespondOrError(c, user, err)
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return
	}

	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		commonapi.RespondError(c, 400, err.Error())
		return
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	err = h.userService.ChangePassword(id, &req, userID)
	if err != nil {
		commonapi.RespondOrError(c, nil, err)
		return
	}
	commonapi.RespondSuccess(c, gin.H{"message": "密码修改成功"})
}
