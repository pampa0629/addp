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
// @Summary      创建用户 | Create user
// @Description  创建新用户（租户管理员权限）| Create new user (tenant admin permission required)
// @Tags         用户管理 | User Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        request body models.UserCreateRequest true "用户信息 | User info"
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
// @Summary      获取用户列表 | List users
// @Description  分页获取用户列表（自动过滤租户）| Get paginated user list (auto-filtered by tenant)
// @Tags         用户管理 | User Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        page query int false "页码 | Page number" default(1)
// @Param        page_size query int false "每页数量 | Page size" default(10)
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

// GetByID godoc
// @Summary      获取用户详情 | Get user detail
// @Tags         用户管理 | User Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "用户ID | User ID"
// @Success      200 {object} models.User
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /users/{id} [get]
func (h *UserHandler) GetByID(c *gin.Context) {
	id, err := commonapi.BindIDParam(c, "id")
	if err != nil {
		return // 错误已经在 BindIDParam 中处理
	}

	userID, _ := commonapi.GetCurrentUserID(c)
	user, err := h.userService.GetByID(id, userID)
	commonapi.RespondOrError(c, user, err)
}

// Update godoc
// @Summary      更新用户 | Update user
// @Tags         用户管理 | User Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "用户ID | User ID"
// @Param        request body models.UserUpdateRequest true "用户更新信息 | User update info"
// @Success      200 {object} models.User
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /users/{id} [put]
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

// Delete godoc
// @Summary      删除用户 | Delete user
// @Tags         用户管理 | User Management
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "用户ID | User ID"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /users/{id} [delete]
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
// @Summary      获取当前用户信息 | Get current user info
// @Description  获取当前登录用户的详细信息 | Get detailed info of the currently logged-in user
// @Tags         用户管理 | User Management
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

// ChangePassword godoc
// @Summary      修改用户密码 | Change user password
// @Tags         用户管理 | User Management
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id path int true "用户ID | User ID"
// @Param        request body models.ChangePasswordRequest true "密码修改请求 | Change password request"
// @Success      200 {object} models.SuccessResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Router       /users/{id}/change-password [put]
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
