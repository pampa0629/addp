package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	workbenchi18n "github.com/addp/workbench/i18n"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct{ views *service.ViewService }

func NewHandler(views *service.ViewService) *Handler { return &Handler{views: views} }

// ListViews 列出当前用户私有的工作台视图。
// @Summary 列出工作台视图 | List Workbench views
// @Description 只返回当前 Tenant 和当前 User 拥有的工作台视图；不访问 Service | Return only Workbench views owned by the current User in the current Tenant; Service is not accessed
// @Tags Workbench Views
// @Produce json
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 100 | Page size, default 20 and maximum 100"
// @Success 200 {object} commonapi.PaginatedResponse "工作台视图分页结果 | Paginated Workbench views"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.view.read"]
// @Router /views [get]
// @Security BearerAuth
func (h *Handler) ListViews(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	page, pageSize, ok := pagination(c)
	if !ok {
		respondError(c, service.ErrInvalidView)
		return
	}
	views, total, err := h.views.List(tenantID, userID, page, pageSize)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondPaginated(c, views, total, page, pageSize)
}

// CreateView 创建当前用户私有的工作台视图。
// @Summary 创建工作台视图 | Create a Workbench view
// @Description 转发当前已验证的 User Bearer 读取 Service Consumer Descriptor，校验完整配置并保存当前契约指纹；请求不得提交 Tenant、owner、指纹或 Token | Forward the validated User Bearer to read the Service Consumer Descriptor, validate the complete configuration, and save the current contract fingerprint; Tenant, owner, fingerprint, and tokens are not accepted
// @Tags Workbench Views
// @Accept json
// @Produce json
// @Param request body models.ViewWriteRequest true "工作台视图配置 | Workbench view configuration"
// @Success 201 {object} models.ViewResponse "新建工作台视图 | Created Workbench view"
// @Failure 400 {object} map[string]interface{} "视图配置无效 | Invalid view configuration"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "Workbench 或 Service 权限不足 | Insufficient Workbench or Service permission"
// @Failure 503 {object} map[string]interface{} "Service 不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.view.create"]
// @Router /views [post]
// @Security BearerAuth
func (h *Handler) CreateView(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	var input models.ViewWriteRequest
	if err := commonapi.BindOptionalJSONStrict(c, &input); err != nil {
		respondError(c, service.ErrInvalidView)
		return
	}
	view, err := h.views.Create(c.Request.Context(), tenantID, userID, descriptorRequest(c), input)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondCreated(c, view)
}

// GetView 获取当前用户拥有的工作台视图。
// @Summary 获取工作台视图 | Get a Workbench view
// @Description 只读取 Workbench 自身保存的配置，不访问 Service；其他用户的视图按不存在处理 | Read only the configuration stored by Workbench without accessing Service; views owned by another user are treated as not found
// @Tags Workbench Views
// @Produce json
// @Param id path string true "Workbench View UUID"
// @Success 200 {object} models.ViewResponse "工作台视图 | Workbench view"
// @Failure 400 {object} map[string]interface{} "ID 无效 | Invalid ID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "视图不存在 | View not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.view.read"]
// @Router /views/{id} [get]
// @Security BearerAuth
func (h *Handler) GetView(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	id, ok := viewID(c)
	if !ok {
		return
	}
	view, err := h.views.Get(tenantID, userID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondSuccess(c, view)
}

// UpdateView 完整更新当前用户拥有的工作台视图。
// @Summary 更新工作台视图 | Update a Workbench view
// @Description service_ref 不可改变；使用正整数 version 乐观并发并重新读取 Descriptor 校验配置和契约指纹 | service_ref is immutable; use a positive version for optimistic concurrency and re-read the Descriptor to validate configuration and contract fingerprint
// @Tags Workbench Views
// @Accept json
// @Produce json
// @Param id path string true "Workbench View UUID"
// @Param request body models.ViewWriteRequest true "完整工作台视图配置和当前 version | Complete Workbench view configuration and current version"
// @Success 200 {object} models.ViewResponse "更新后的工作台视图 | Updated Workbench view"
// @Failure 400 {object} map[string]interface{} "视图配置无效 | Invalid view configuration"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "Workbench 或 Service 权限不足 | Insufficient Workbench or Service permission"
// @Failure 404 {object} map[string]interface{} "视图不存在 | View not found"
// @Failure 409 {object} map[string]interface{} "版本冲突 | Version conflict"
// @Failure 503 {object} map[string]interface{} "Service 不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.view.update"]
// @Router /views/{id} [put]
// @Security BearerAuth
func (h *Handler) UpdateView(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	id, ok := viewID(c)
	if !ok {
		return
	}
	var input models.ViewWriteRequest
	if err := commonapi.BindOptionalJSONStrict(c, &input); err != nil {
		respondError(c, service.ErrInvalidView)
		return
	}
	view, err := h.views.Update(c.Request.Context(), tenantID, userID, id, descriptorRequest(c), input)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondSuccess(c, view)
}

// DeleteView 删除当前用户拥有的工作台视图。
// @Summary 删除工作台视图 | Delete a Workbench view
// @Description 删除只匹配当前 Tenant 与当前 User 的视图，不访问 Service | Delete a view only when it matches the current Tenant and User, without accessing Service
// @Tags Workbench Views
// @Produce json
// @Param id path string true "Workbench View UUID"
// @Success 200 {object} map[string]string "删除成功 | Deleted"
// @Failure 400 {object} map[string]interface{} "ID 无效 | Invalid ID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "视图不存在 | View not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.view.delete"]
// @Router /views/{id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteView(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	id, ok := viewID(c)
	if !ok {
		return
	}
	if err := h.views.Delete(tenantID, userID, id); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, workbenchi18n.MsgDeleteSucceeded)})
}

func actor(c *gin.Context) (int64, int64, bool) {
	tenantID, tenantOK := commonAuth.TenantIDFromGin(c)
	userID := int64(commonAuth.GetUserID(c))
	if !tenantOK || userID <= 0 {
		commonapi.RespondError(c, http.StatusForbidden, commoni18n.T(c, workbenchi18n.MsgInvalidRequest))
		return 0, 0, false
	}
	return tenantID, userID, true
}

func pagination(c *gin.Context) (int, int, bool) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, pageSizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	return page, pageSize, pageErr == nil && pageSizeErr == nil && page > 0 && pageSize > 0 && pageSize <= 100
}

func viewID(c *gin.Context) (string, bool) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil || id == uuid.Nil {
		respondError(c, service.ErrInvalidView)
		return "", false
	}
	return id.String(), true
}

func descriptorRequest(c *gin.Context) service.DescriptorRequest {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	bearer := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	return service.DescriptorRequest{
		BearerToken: bearer, AcceptLanguage: c.GetHeader("Accept-Language"),
		RequestID: requestidmiddleware.FromGinContext(c),
	}
}

func respondError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	messageKey := workbenchi18n.MsgOperationFailed
	errorCode := ""
	switch {
	case errors.Is(err, service.ErrInvalidView):
		status, messageKey = http.StatusBadRequest, workbenchi18n.MsgInvalidRequest
	case errors.Is(err, service.ErrViewNotFound):
		status, messageKey = http.StatusNotFound, workbenchi18n.MsgViewNotFound
	case errors.Is(err, service.ErrViewVersionConflict):
		status, messageKey, errorCode = http.StatusConflict, workbenchi18n.MsgVersionConflict, "workbench_view_version_conflict"
	case errors.Is(err, service.ErrServiceAccessDenied):
		status, messageKey = http.StatusForbidden, workbenchi18n.MsgServiceAccessDenied
	case errors.Is(err, service.ErrServiceUnavailable):
		status, messageKey = http.StatusServiceUnavailable, workbenchi18n.MsgServiceUnavailable
	}
	body := gin.H{"error": commoni18n.T(c, messageKey)}
	if errorCode != "" {
		body["error_code"] = errorCode
	}
	c.JSON(status, body)
}
