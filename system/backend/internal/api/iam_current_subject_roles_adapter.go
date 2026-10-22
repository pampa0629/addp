package api

import (
	"context"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	"github.com/gin-gonic/gin"
)

type currentSubjectRolesService interface {
	ResolveCurrentSubjectRoles(context.Context, string) (*authorization.CurrentSubjectRoles, error)
}
type IAMCurrentSubjectRolesHandler struct{ service currentSubjectRolesService }

// Get godoc
// @Summary 查询本人当前租户角色 | Observe own current tenant roles
// @Description 仅第一方登录用户查询本人角色，包含无功能权限的业务角色；不授予权限，不支持指定其他主体或租户 | First-party users may observe only their own tenant roles, including roles without functional permissions; grants no access and accepts no subject or tenant selector
// @Tags 认证 | Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} authorization.CurrentSubjectRoles
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @x-addp-auth-mode "self"
// @Router /auth/subject-roles [get]
func (h *IAMCurrentSubjectRolesHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	if h.service == nil {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	bearer := iamBearerToken(c.GetHeader("Authorization"))
	if bearer == "" {
		respondIAMError(c, commonapi.ErrUnauthorized)
		return
	}
	result, err := h.service.ResolveCurrentSubjectRoles(c.Request.Context(), bearer)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
