package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	commonClient "github.com/addp/common/client"
	sharedauth "github.com/addp/common/middleware/auth"
	commoni18n "github.com/addp/common/middleware/i18n"
	manageri18n "github.com/addp/manager/i18n"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

type DataProfileHandler struct {
	service                 *service.DataProfileService
	notifyExecutionEnqueued func()
}

func NewDataProfileHandler(profileService *service.DataProfileService) *DataProfileHandler {
	return &DataProfileHandler{service: profileService}
}

func (h *DataProfileHandler) SetExecutionEnqueueNotifier(notify func()) {
	h.notifyExecutionEnqueued = notify
}

// GetCurrent godoc
// @Summary 获取当前数据剖析结果 | Get current data profile
// @Description 查询当前剖析状态，不隐式触发执行；已有结果须以实际采样冻结的完整来源复核当前用户的源规则及本地 Security。缺失来源证据的历史结果保留但拒绝返回，不从当前视图依赖补造。| Query current profiling state without starting an execution. Stored results require current User source rules for the complete frozen actual sampling sources and local Security; historical results without provenance are retained but not returned, never reconstructed from current view dependencies.
// @Tags Manager
// @Produce json
// @Param locator query string true "资源定位符 | Resource locator"
// @Param child_name query string false "容器内表格 child 名称 | Tabular child name in a container"
// @Param ref_path query string false "multi child 内 ref 路径 | Ref path within a multi child"
// @Param nested_child_path query string false "嵌套容器 child 路径 | Nested container child path"
// @Param profile_config_hash query string false "服务端返回的条件剖析配置哈希；省略时查询全范围剖析 | Server-issued conditional profile config hash; omit for the all-data profile"
// @Success 200 {object} service.DataProfileCurrentResponse "当前剖析状态 | Current profiling state"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "缺少可信认证上下文或认证已过期 | Missing trusted authentication context or expired authentication"
// @Failure 403 {object} map[string]interface{} "主体不支持此入口、来源证据缺失或当前无权读取全部来源 | Principal unsupported, source provenance missing or current access to all sources denied"
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
	authContext, exists := sharedauth.AuthContextFromGin(c)
	if !exists {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}
	response, err := h.service.GetCurrent(c.Request.Context(), authContext, sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), req)
	if err != nil {
		handleDataProfileError(c, err, manageri18n.MsgDataProfileQueryFailed)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, response)
}

// CreateExecution godoc
// @Summary 创建数据剖析执行 | Create data profiling execution
// @Description 首版仅支持已扫描 PostgreSQL 单表的有界采样，视图及多来源计划返回 422。冻结完整分页计划，由当前用户请求签发并原子绑定源授权后受理；后台逐页及提交前复核现行源授权、保护和租约。不接受身份、Token 或 SQL 请求字段；不保证准备后源端并发 DDL 的事务内依赖冻结。| The first release supports bounded sampling of scanned PostgreSQL single tables only; views and multi-source plans return 422. Freeze the full paging plan and accept after current-user issuance and atomic source authorization binding; revalidate current source authorization, protection and lease before each read and result commit. Identity, Token and SQL request fields are not accepted. Transactional dependency freezing against concurrent source DDL after preparation is not guaranteed.
// @Tags Manager
// @Accept json
// @Produce json
// @Param body body service.DataProfileExecutionRequest true "剖析执行请求 | Profiling execution request"
// @Success 202 {object} service.DataProfileExecutionResponse "执行已受理 | Execution accepted"
// @Failure 400 {object} map[string]interface{} "请求参数错误 | Bad request"
// @Failure 401 {object} map[string]interface{} "缺少可信认证上下文或认证已过期 | Missing trusted authentication context or expired authentication"
// @Failure 403 {object} map[string]interface{} "主体不支持此入口、来源授权被拒绝或保护规则禁止剖析 | Unsupported principal, denied source authorization, or protection rules prohibit profiling"
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
	decoder.UseNumber()
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
	authContext, exists := sharedauth.AuthContextFromGin(c)
	if !exists {
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
		return
	}
	response, err := h.service.CreateExecution(c.Request.Context(), authContext, sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), req)
	if err != nil {
		handleDataProfileError(c, err, manageri18n.MsgDataProfileCreateFailed)
		return
	}
	if h.notifyExecutionEnqueued != nil {
		h.notifyExecutionEnqueued()
	}
	c.JSON(http.StatusAccepted, response)
}

func handleDataProfileError(c *gin.Context, err error, fallbackMessage string) {
	switch {
	case errors.Is(err, service.ErrDataProfileActorExpired), errors.Is(err, commonClient.ErrManagerPreviewCredentialRejected):
		managerError(c, http.StatusUnauthorized, manageri18n.MsgUnauthorized)
	case errors.Is(err, service.ErrDataProfileSourceAuthorizationRequired), errors.Is(err, commonClient.ErrManagerPreviewReadDenied):
		c.JSON(http.StatusForbidden, gin.H{"error": commoni18n.T(c, manageri18n.MsgDataProfileSourceReadRequired), "error_code": "source_authorization_required"})
	case errors.Is(err, commonClient.ErrManagerPreviewReadUnavailable):
		managerError(c, http.StatusServiceUnavailable, manageri18n.MsgDataProfileUnavailable)
	case errors.Is(err, service.ErrDataProfileActorRequired):
		managerError(c, http.StatusForbidden, manageri18n.MsgUnauthorized)
	case errors.Is(err, service.ErrDataProfileProtectionRequired):
		protectionRequired(c)
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
