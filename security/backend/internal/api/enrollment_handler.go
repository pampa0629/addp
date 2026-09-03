package api

import (
	"net/http"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataprotection"
	commonauth "github.com/addp/common/middleware/auth"
	"github.com/addp/security/internal/models"
	"github.com/addp/security/internal/service"
	"github.com/gin-gonic/gin"
)

type EnrollmentHandler struct{ svc *service.EnrollmentService }

func NewEnrollmentHandler(svc *service.EnrollmentService) *EnrollmentHandler {
	return &EnrollmentHandler{svc: svc}
}

// @Summary 保护纳管列表 | List protection enrollments
// @Description 分页返回当前租户的显式保护纳管、最近成功发现摘要，以及必要 Owner 的投影安装确认状态和已安装 action/effect 规则；安装确认不表示某次数据请求已执行成功 | Return explicit protection enrollments, latest successful discovery summaries, required-owner projection installation acknowledgements, and installed action/effect rules for the current tenant; installation acknowledgement does not mean a data request has executed successfully
// @Tags Protection Enrollment
// @Produce json
// @Param page query int false "页码 | Page number"
// @Param page_size query int false "每页数量，最大 100 | Page size, maximum 100"
// @Param scope query string false "生命周期视图，current 为保护中，released 为按退出完成时间倒序的已退出历史，all 为全部；默认 current | Lifecycle view: current for in-progress protection, released for exited history ordered by completion time descending, all for both; current by default" Enums(current,released,all) default(current)
// @Success 200 {object} models.ProtectionEnrollmentListResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.read"]
// @Router /protection-enrollments [get]
// @Security BearerAuth
func (h *EnrollmentHandler) List(c *gin.Context) {
	page, pageSize := commonapi.ParsePagination(c)
	scope := strings.TrimSpace(c.DefaultQuery("scope", models.EnrollmentListScopeCurrent))
	result, err := h.svc.List(c.Request.Context(), getTenantID(c), scope, int64(page), int64(pageSize))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 创建保护纳管 | Create protection enrollment
// @Description 使用 Meta 资源树返回的标准 DataItem locator 创建完整资源纳管；Security 自行计算 fingerprint，创建后固定返回 activating，只有 Manager、Transfer、Develop 和 Service 全部安装 enrolling 门禁并确认后才进入 enrolling | Create whole-resource enrollment from a standard DataItem locator returned by the Meta resource tree; Security computes the fingerprint and always returns activating until Manager, Transfer, Develop, and Service install and acknowledge the enrolling gate
// @Tags Protection Enrollment
// @Accept json
// @Produce json
// @Param request body models.CreateProtectionEnrollmentRequest true "DataItem 资源定位符 | DataItem resource locator"
// @Success 201 {object} models.ProtectionEnrollmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.create"]
// @Router /protection-enrollments [post]
// @Security BearerAuth
func (h *EnrollmentHandler) Create(c *gin.Context) {
	var request models.CreateProtectionEnrollmentRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	result, err := h.svc.Create(c.Request.Context(), getTenantID(c), getUserID(c), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 保护纳管详情 | Get protection enrollment
// @Description 返回纳管状态、最近成功发现摘要、资源版本，以及每个必要 Owner 的投影安装确认状态和已安装 action/effect 规则；具体请求仍由 Owner 按运行时能力执行或保守拒绝 | Return enrollment state, latest successful discovery summary, resource version, and each required owner's projection installation acknowledgement and installed action/effect rules; each request is still executed or conservatively denied by the owner according to runtime capability
// @Tags Protection Enrollment
// @Produce json
// @Param id path string true "纳管 ID | Enrollment ID"
// @Success 200 {object} models.ProtectionEnrollmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.read"]
// @Router /protection-enrollments/{id} [get]
// @Security BearerAuth
func (h *EnrollmentHandler) Get(c *gin.Context) {
	result, err := h.svc.Get(c.Request.Context(), getTenantID(c), c.Param("id"))
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 重新纳入数据保护 | Re-enroll data protection
// @Description 从已退出记录冻结的目标引用创建全新的 activating 纳管并重新经过四个必要 Owner 的保护激活屏障；旧记录及其退出审计保持只读，同一目标已有未退出纳管时冲突 | Create a new activating enrollment from the target reference frozen in a released record and run the four required owners through the protection activation barrier again; the old record and exit audit remain read-only, and an existing live enrollment for the same target causes a conflict
// @Tags Protection Enrollment
// @Accept json
// @Produce json
// @Param id path string true "已退出纳管 ID | Released enrollment ID"
// @Param request body models.ReEnrollProtectionEnrollmentRequest true "已退出记录版本 | Released record version"
// @Success 201 {object} models.ProtectionEnrollmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.create"]
// @Router /protection-enrollments/{id}/re-enrollments [post]
// @Security BearerAuth
func (h *EnrollmentHandler) ReEnroll(c *gin.Context) {
	var request models.ReEnrollProtectionEnrollmentRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	result, err := h.svc.ReEnroll(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 退出保护纳管 | Release protection enrollment
// @Description 沿单一 release 路径发布退出变化并等待所有必要 Owner 原子删除本地纳管索引；basis=manual 表示常规人工退出，basis=no_supported_findings 仅在最近成功发现零命中且无进行中发现时接受并冻结来源快照；等待期间继续保持保护 | Publish release changes through the single release path and wait for every required owner to atomically remove its local enrollment index; basis=manual is a regular manual release, while basis=no_supported_findings is accepted only when the latest successful discovery has zero findings and no discovery is running, and freezes the source snapshot; protection remains in force while waiting
// @Tags Protection Enrollment
// @Accept json
// @Produce json
// @Param id path string true "纳管 ID | Enrollment ID"
// @Param request body models.ReleaseProtectionEnrollmentRequest true "资源版本、退出依据与原因 | Resource version, release basis, and reason"
// @Success 200 {object} models.ProtectionEnrollmentResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.update"]
// @Router /protection-enrollments/{id}/releases [post]
// @Security BearerAuth
func (h *EnrollmentHandler) Release(c *gin.Context) {
	var request models.ReleaseProtectionEnrollmentRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	result, err := h.svc.Release(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 创建重新发现执行 | Create rediscovery execution
// @Description 为已进入 enrolling 或 active 的纳管创建一次有界重新发现；同一纳管同时至多一个 pending/running 执行 | Create one bounded rediscovery for an enrollment in enrolling or active state; at most one pending or running execution may exist for the same enrollment
// @Tags Protection Enrollment
// @Accept json
// @Produce json
// @Param id path string true "纳管 ID | Enrollment ID"
// @Param request body models.CreateProtectionDiscoveryExecutionRequest true "资源版本 | Resource version"
// @Success 201 {object} models.ProtectionDiscoveryExecutionResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 404 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.enrollment.update"]
// @Router /protection-enrollments/{id}/discovery-executions [post]
// @Security BearerAuth
func (h *EnrollmentHandler) CreateDiscoveryExecution(c *gin.Context) {
	var request models.CreateProtectionDiscoveryExecutionRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	result, err := h.svc.CreateDiscoveryExecution(c.Request.Context(), getTenantID(c), getUserID(c), c.Param("id"), request)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

// @Summary 拉取保护投影变化 | Pull protection projection changes
// @Description 按当前 Tenant Service Access Token 的固定 OAuth Client 返回对应 Owner 的 append-only 变化；调用方不能选择其他 consumer | Return append-only changes for the owner fixed by the current Tenant Service Access Token OAuth Client; callers cannot select another consumer
// @Tags Protection Projection Runtime
// @Produce json
// @Param after_cursor query string false "上次原子提交的不透明游标；空值从历史起点开始 | Opaque cursor committed atomically by the owner; empty starts from history origin"
// @Param limit query int false "批大小，默认 200，最大 500 | Batch size, default 200, maximum 500"
// @Success 200 {object} dataprotection.ProjectionChangesResponse
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_projection.read"]
// @Router /runtime/protection-projections/changes [get]
// @Security BearerAuth
func (h *EnrollmentHandler) ListChanges(c *gin.Context) {
	limit := 200
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			respondError(c, errBadRequest)
			return
		}
		limit = parsed
	}
	result, err := h.svc.ListChanges(c.Request.Context(), getTenantID(c), protectionConsumerOwner(c), c.Query("after_cursor"), limit)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// @Summary 确认保护投影游标 | Acknowledge protection projection cursor
// @Description 确认当前固定 Owner 已在本地事务中原子安装该游标；重复确认幂等，倒退或伪造游标冲突 | Confirm that the current fixed owner atomically installed the cursor in its local transaction; duplicate acknowledgement is idempotent and regressing or forged cursors conflict
// @Tags Protection Projection Runtime
// @Accept json
// @Produce json
// @Param request body dataprotection.ProjectionAcknowledgementRequest true "已应用游标 | Applied cursor"
// @Success 204
// @Failure 400 {object} map[string]string
// @Failure 401 {object} map[string]string
// @Failure 403 {object} map[string]string
// @Failure 409 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Failure 503 {object} map[string]string
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["security.protection_projection.update"]
// @Router /runtime/protection-projection-acknowledgements [post]
// @Security BearerAuth
func (h *EnrollmentHandler) Acknowledge(c *gin.Context) {
	var request dataprotection.ProjectionAcknowledgementRequest
	if c.ShouldBindJSON(&request) != nil {
		respondError(c, errBadRequest)
		return
	}
	if err := h.svc.Acknowledge(c.Request.Context(), getTenantID(c), protectionConsumerOwner(c), request.AppliedCursor); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func protectionConsumerOwner(c *gin.Context) string {
	switch commonauth.GetClientID(c) {
	case "addp-manager":
		return "manager"
	case "addp-transfer":
		return "transfer"
	case "addp-develop":
		return "develop"
	case "addp-service":
		return "service"
	default:
		return ""
	}
}
