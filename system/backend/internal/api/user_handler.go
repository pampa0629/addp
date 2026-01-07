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
