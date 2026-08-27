package api

import (
	"net/http"

	commonapi "github.com/addp/common/api"
	commoni18n "github.com/addp/common/middleware/i18n"
	workbenchi18n "github.com/addp/workbench/i18n"
	"github.com/addp/workbench/internal/models"
	"github.com/addp/workbench/internal/service"
	"github.com/gin-gonic/gin"
)

// ListDataApplications 列出当前用户的数据应用。
// @Summary 列出数据应用 | List data applications
// @Description 只返回当前 Tenant 和当前 User 拥有的数据应用草稿摘要 | Return only data application draft summaries owned by the current User in the current Tenant
// @Tags Workbench Data Applications
// @Produce json
// @Param page query int false "页码，默认 1 | Page number, default 1"
// @Param page_size query int false "每页数量，默认 20，最大 100 | Page size, default 20 and maximum 100"
// @Success 200 {object} commonapi.PaginatedResponse "数据应用分页结果 | Paginated data applications"
// @Failure 400 {object} map[string]interface{} "请求参数无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 500 {object} map[string]interface{} "服务器内部错误 | Internal server error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.read"]
// @Router /data_applications [get]
// @Security BearerAuth
func (h *Handler) ListDataApplications(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	page, pageSize, ok := pagination(c)
	if !ok {
		respondError(c, service.ErrInvalidDataApplication)
		return
	}
	items, total, err := h.applications.List(tenantID, userID, page, pageSize)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondPaginated(c, items, total, page, pageSize)
}

// CreateDataApplication 从当前用户的 Workbench View 创建独立数据应用。
// @Summary 创建数据应用 | Create a data application
// @Description 重新校验来源 View 的 Service 契约并复制为 Component 快照；不保存 source_view_ids | Revalidate source View Service contracts and copy them into Component snapshots; source_view_ids are not persisted
// @Tags Workbench Data Applications
// @Accept json
// @Produce json
// @Param request body models.DataApplicationCreateRequest true "数据应用名称和来源 View | Data application name and source Views"
// @Success 201 {object} models.DataApplicationResponse "新建数据应用 | Created data application"
// @Failure 400 {object} map[string]interface{} "配置无效 | Invalid configuration"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "Workbench 或 Service 权限不足 | Insufficient Workbench or Service permission"
// @Failure 404 {object} map[string]interface{} "来源 View 不存在 | Source View not found"
// @Failure 503 {object} map[string]interface{} "Service 不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.create"]
// @Router /data_applications [post]
// @Security BearerAuth
func (h *Handler) CreateDataApplication(c *gin.Context) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return
	}
	var input models.DataApplicationCreateRequest
	if err := commonapi.BindOptionalJSONStrict(c, &input); err != nil {
		respondError(c, service.ErrInvalidDataApplication)
		return
	}
	application, err := h.applications.Create(c.Request.Context(), tenantID, userID, descriptorRequest(c), input)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondCreated(c, application)
}

// GetDataApplication 获取当前用户的数据应用草稿。
// @Summary 获取数据应用 | Get a data application
// @Tags Workbench Data Applications
// @Produce json
// @Param id path string true "Data Application UUID"
// @Success 200 {object} models.DataApplicationResponse "数据应用草稿 | Data application draft"
// @Failure 400 {object} map[string]interface{} "ID 无效 | Invalid ID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "数据应用不存在 | Data application not found"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.read"]
// @Router /data_applications/{id} [get]
// @Security BearerAuth
func (h *Handler) GetDataApplication(c *gin.Context) {
	h.withDataApplication(c, func(tenantID, userID int64, id string) (interface{}, error) {
		return h.applications.Get(tenantID, userID, id)
	})
}

// UpdateDataApplication 完整替换数据应用草稿。
// @Summary 更新数据应用草稿 | Update a data application draft
// @Description Component ID 和 ServiceReference 不可改变；使用正整数 version 乐观并发并重新校验所有 Service 契约 | Component IDs and ServiceReferences are immutable; use positive version optimistic concurrency and revalidate every Service contract
// @Tags Workbench Data Applications
// @Accept json
// @Produce json
// @Param id path string true "Data Application UUID"
// @Param request body models.DataApplicationUpdateRequest true "完整草稿和当前 version | Complete draft and current version"
// @Success 200 {object} models.DataApplicationResponse "更新后的数据应用 | Updated data application"
// @Failure 400 {object} map[string]interface{} "配置无效 | Invalid configuration"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "Workbench 或 Service 权限不足 | Insufficient Workbench or Service permission"
// @Failure 404 {object} map[string]interface{} "数据应用不存在 | Data application not found"
// @Failure 409 {object} map[string]interface{} "版本冲突 | Version conflict"
// @Failure 503 {object} map[string]interface{} "Service 不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.update"]
// @Router /data_applications/{id} [put]
// @Security BearerAuth
func (h *Handler) UpdateDataApplication(c *gin.Context) {
	tenantID, userID, id, ok := dataApplicationActor(c)
	if !ok {
		return
	}
	var input models.DataApplicationUpdateRequest
	if err := commonapi.BindOptionalJSONStrict(c, &input); err != nil {
		respondError(c, service.ErrInvalidDataApplication)
		return
	}
	application, err := h.applications.Update(c.Request.Context(), tenantID, userID, id, descriptorRequest(c), input)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondSuccess(c, application)
}

// DeleteDataApplication 删除从未发布的数据应用。
// @Summary 删除未发布数据应用 | Delete an unpublished data application
// @Description 只有从未产生 Application Revision 的应用可删除；请求必须携带当前 version | Only applications that never produced an Application Revision can be deleted; the current version is required
// @Tags Workbench Data Applications
// @Accept json
// @Produce json
// @Param id path string true "Data Application UUID"
// @Param request body models.DataApplicationVersionRequest true "当前 version | Current version"
// @Success 200 {object} map[string]string "删除成功 | Deleted"
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "数据应用不存在 | Data application not found"
// @Failure 409 {object} map[string]interface{} "版本冲突或应用已发布 | Version conflict or application already published"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.delete"]
// @Router /data_applications/{id} [delete]
// @Security BearerAuth
func (h *Handler) DeleteDataApplication(c *gin.Context) {
	tenantID, userID, id, ok := dataApplicationActor(c)
	if !ok {
		return
	}
	input, ok := bindDataApplicationVersion(c)
	if !ok {
		return
	}
	if err := h.applications.Delete(tenantID, userID, id, input.Version); err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": commoni18n.T(c, workbenchi18n.MsgDataApplicationDeleteSucceeded)})
}

// PublishDataApplication 发布新的不可变 Application Revision。
// @Summary 发布数据应用 | Publish a data application
// @Tags Workbench Data Applications
// @Accept json
// @Produce json
// @Param id path string true "Data Application UUID"
// @Param request body models.DataApplicationVersionRequest true "当前 version | Current version"
// @Success 200 {object} models.DataApplicationResponse "已发布数据应用 | Published data application"
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "Workbench 或 Service 权限不足 | Insufficient Workbench or Service permission"
// @Failure 404 {object} map[string]interface{} "数据应用不存在 | Data application not found"
// @Failure 409 {object} map[string]interface{} "版本冲突 | Version conflict"
// @Failure 503 {object} map[string]interface{} "Service 不可用 | Service unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.publish"]
// @Router /data_applications/{id}/publish [post]
// @Security BearerAuth
func (h *Handler) PublishDataApplication(c *gin.Context) {
	tenantID, userID, id, ok := dataApplicationActor(c)
	if !ok {
		return
	}
	input, ok := bindDataApplicationVersion(c)
	if !ok {
		return
	}
	application, err := h.applications.Publish(c.Request.Context(), tenantID, userID, id, descriptorRequest(c), input.Version)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondSuccess(c, application)
}

// OfflineDataApplication 下线当前发布修订。
// @Summary 下线数据应用 | Take a data application offline
// @Tags Workbench Data Applications
// @Accept json
// @Produce json
// @Param id path string true "Data Application UUID"
// @Param request body models.DataApplicationVersionRequest true "当前 version | Current version"
// @Success 200 {object} models.DataApplicationResponse "已下线数据应用 | Offline data application"
// @Failure 400 {object} map[string]interface{} "请求无效 | Invalid request"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 404 {object} map[string]interface{} "数据应用不存在 | Data application not found"
// @Failure 409 {object} map[string]interface{} "版本冲突或尚未发布 | Version conflict or unpublished application"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.publish"]
// @Router /data_applications/{id}/offline [post]
// @Security BearerAuth
func (h *Handler) OfflineDataApplication(c *gin.Context) {
	tenantID, userID, id, ok := dataApplicationActor(c)
	if !ok {
		return
	}
	input, ok := bindDataApplicationVersion(c)
	if !ok {
		return
	}
	application, err := h.applications.Offline(tenantID, userID, id, input.Version)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondSuccess(c, application)
}

// GetDataApplicationRuntime 获取当前用户可运行的发布修订。
// @Summary 获取数据应用运行快照 | Get a data application runtime snapshot
// @Description 创建者或持有生效 Resource Grant 的 User 可读取当前 published Revision；真实数据仍由浏览器以当前 User Bearer 调用各 Service | The owner or a User with an effective Resource Grant may read the current published Revision; the browser still calls every Service with the current User Bearer
// @Tags Workbench Data Applications
// @Produce json
// @Param id path string true "Data Application UUID"
// @Success 200 {object} models.DataApplicationRuntimeResponse "不可变运行快照 | Immutable runtime snapshot"
// @Failure 400 {object} map[string]interface{} "ID 无效 | Invalid ID"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "权限不足 | Forbidden"
// @Failure 409 {object} map[string]interface{} "尚未发布或已下线 | Unpublished or offline"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["workbench.data_application.execute"]
// @Router /data_applications/{id}/runtime [get]
// @Security BearerAuth
func (h *Handler) GetDataApplicationRuntime(c *gin.Context) {
	h.withDataApplication(c, func(tenantID, userID int64, id string) (interface{}, error) {
		return h.applications.Runtime(tenantID, userID, id)
	})
}

func (h *Handler) withDataApplication(c *gin.Context, load func(tenantID, userID int64, id string) (interface{}, error)) {
	tenantID, userID, id, ok := dataApplicationActor(c)
	if !ok {
		return
	}
	response, err := load(tenantID, userID, id)
	if err != nil {
		respondError(c, err)
		return
	}
	commonapi.RespondSuccess(c, response)
}

func dataApplicationActor(c *gin.Context) (int64, int64, string, bool) {
	tenantID, userID, ok := actor(c)
	if !ok {
		return 0, 0, "", false
	}
	id, ok := dataApplicationID(c)
	return tenantID, userID, id, ok
}

func bindDataApplicationVersion(c *gin.Context) (models.DataApplicationVersionRequest, bool) {
	var input models.DataApplicationVersionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &input); err != nil || input.Version <= 0 {
		respondError(c, service.ErrInvalidDataApplication)
		return models.DataApplicationVersionRequest{}, false
	}
	return input, true
}
