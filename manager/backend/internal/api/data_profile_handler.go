package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DataProfileHandler struct {
	service *service.DataProfileService
}

func NewDataProfileHandler(profileService *service.DataProfileService) *DataProfileHandler {
	return &DataProfileHandler{service: profileService}
}

// GetCurrent godoc
// @Summary 获取当前数据剖析结果 | Get current data profile
// @Description 查询指定表格型 data item、内容选择上下文和可选剖析配置哈希的当前成功结果、活跃执行及新鲜度，不会隐式触发执行。| Query the current successful profile, active execution, and freshness for a tabular data item, content selection, and optional profile config hash without implicitly starting an execution.
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符 | Resource locator"
// @Param child_name query string false "容器内表格 child 名称 | Tabular child name in a container"
// @Param ref_path query string false "multi child 内 ref 路径 | Ref path within a multi child"
// @Param nested_child_path query string false "嵌套容器 child 路径 | Nested container child path"
// @Param profile_config_hash query string false "服务端返回的条件剖析配置哈希；省略时查询全范围剖析 | Server-issued conditional profile config hash; omit for the all-data profile"
// @Success 200 {object} service.DataProfileCurrentResponse "当前剖析状态 | Current profiling state"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 422 {object} map[string]interface{} "资源不支持剖析 | Resource is not profileable"
// @Failure 503 {object} map[string]interface{} "剖析服务不可用 | Profiling unavailable"
// @Failure 500 {object} map[string]interface{} "查询剖析结果失败 | Failed to query profile"
// @x-ai-hint "查询指定 locator 已保存的数据剖析结果；该接口不会触发源数据读取。"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
// @Router /data-profiles/current [get]
// @Security BearerAuth
func (h *DataProfileHandler) GetCurrent(c *gin.Context) {
	if h == nil || h.service == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgDataProfileUnavailable)
		return
	}
	req := service.DataProfileCurrentRequest{
		Locator:           strings.TrimSpace(c.Query("locator")),
		ProfileConfigHash: strings.TrimSpace(c.Query("profile_config_hash")),
		DataProfileSelection: service.DataProfileSelection{
			ChildName:       c.Query("child_name"),
			RefPath:         c.Query("ref_path"),
			NestedChildPath: c.Query("nested_child_path"),
		},
	}
	if req.Locator == "" {
		missingLocator(c)
		return
	}
	response, err := h.service.GetCurrent(c.Request.Context(), tenantIDValue(c), req)
	if err != nil {
		handleDataProfileError(c, err, manageri18n.MsgDataProfileQueryFailed)
		return
	}
	c.JSON(http.StatusOK, response)
}

// CreateExecution godoc
// @Summary 创建数据剖析执行 | Create data profiling execution
// @Description 为指定表格型 data item 创建或复用一次全范围或结构化条件范围的采样剖析 ad-hoc execution；不会创建任务定义，也不接受 SQL。| Create or reuse an all-data or structured-condition sample profiling ad-hoc execution for a tabular data item without creating a task definition or accepting SQL.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body service.DataProfileExecutionRequest true "剖析执行请求 | Profiling execution request"
// @Success 202 {object} service.DataProfileExecutionResponse "执行已受理 | Execution accepted"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 422 {object} map[string]interface{} "资源不支持剖析 | Resource is not profileable"
// @Failure 503 {object} map[string]interface{} "剖析服务不可用 | Profiling unavailable"
// @Failure 500 {object} map[string]interface{} "创建剖析执行失败 | Failed to create profiling execution"
// @x-ai-hint "为指定 locator 发起有界采样剖析；mode 首期只允许 sample，data_scope 只允许 all 或单层 and/or 结构化条件，重复的活动执行会被复用。"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_profile.execute"]
// @Router /data-profile-executions [post]
// @Security BearerAuth
func (h *DataProfileHandler) CreateExecution(c *gin.Context) {
	if h == nil || h.service == nil {
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgDataProfileUnavailable)
		return
	}
	var req service.DataProfileExecutionRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		managerError(c, http.StatusBadRequest, manageri18n.MsgInvalidRequestBody)
		return
	}
	if strings.TrimSpace(req.Locator) == "" {
		missingLocator(c)
		return
	}
	response, err := h.service.CreateExecution(c.Request.Context(), tenantIDValue(c), userIDValue(c), req)
	if err != nil {
		handleDataProfileError(c, err, manageri18n.MsgDataProfileCreateFailed)
		return
	}
	c.JSON(http.StatusAccepted, response)
}

func handleDataProfileError(c *gin.Context, err error, fallbackMessage string) {
	switch {
	case errors.Is(err, service.ErrDataProfileUnsupported):
		managerError(c, http.StatusUnprocessableEntity, manageri18n.MsgDataProfileUnsupported)
	case errors.Is(err, service.ErrDataProfileUnavailable):
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgDataProfileUnavailable)
	case errors.Is(err, service.ErrDataProfileInvalidRequest):
		managerError(c, http.StatusBadRequest, fallbackMessage)
	default:
		managerError(c, http.StatusInternalServerError, fallbackMessage)
	}
}
