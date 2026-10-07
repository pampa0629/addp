package api

import (
	"net/http"
	"time"

	"github.com/addp/catalog/internal/service"
	commonapi "github.com/addp/common/api"
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

type createSharingDecisionRequest struct {
	DecisionID    string     `json:"decision_id" format:"uuid"`
	Version       string     `json:"version"`
	RecipientType string     `json:"recipient_type" enums:"user,project_group"`
	RecipientID   string     `json:"recipient_id"`
	ExpiryMode    string     `json:"expiry_mode" binding:"required" enums:"at_time,until_revoked"`
	ExpiresAt     *time.Time `json:"expires_at" format:"date-time" extensions:"x-nullable"`
	Reason        string     `json:"reason" maxLength:"2000"`
}

// CreateSharingDecision records a business decision, not data authorization.
// @Summary 确认一次普通只读共享 | Confirm one ordinary read-only sharing decision
// @Description 当前业务负责人显式确认；本人或本人项目组受益不自动拒绝。完整源目标由 Meta 当前祖先链与 System 能力描述核对；不接受自报确认人、路径或动作。必须显式选择 expiry_mode：at_time 提供未来 expires_at；until_revoked 无到期时间（省略或 null），仍受当前资格、撤销与安全约束，漏填模式不默认永久。decision_id 同参重试返回原记录；决定不是受理回执或 Grant，不授予数据访问 | The current business owner explicitly confirms sharing, including self or own project-group benefit. The exact target is checked against current Meta ancestry and System capabilities. Explicit expiry_mode is required: at_time requires future expires_at; until_revoked requires an omitted or null date and remains subject to current eligibility, revocation and safety. Missing mode never defaults to indefinite. Identical parameters recover the original decision, not an acceptance receipt, Grant or data access
// @Tags Catalog Sharing
// @Accept json
// @Produce json
// @Param id path string true "条目 UUID | Entry UUID"
// @Param request body createSharingDecisionRequest true "共享确认参数 | Sharing confirmation parameters"
// @Success 201 {object} service.SharingDecisionResult "新建的不可变业务决定 | New immutable business decision"
// @Success 200 {object} service.SharingDecisionResult "同参恢复的原决定 | Original decision recovered with identical parameters"
// @Failure 400 {object} map[string]interface{} "请求或期限无效 | Invalid request or expiry"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "缺少独立权限或当前业务责任 | Missing independent permission or current business ownership"
// @Failure 404 {object} map[string]interface{} "条目不存在或不可见 | Entry missing or invisible"
// @Failure 409 {object} map[string]interface{} "版本、决定参数或当前目标冲突 | Version, decision binding or current target conflict"
// @Failure 503 {object} map[string]interface{} "Meta 或 System 核验不可用 | Meta or System validation unavailable"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.sharing_decision.create"]
// @Router /entries/{id}/sharing_decisions [post]
// @Security BearerAuth
func (h *Handler) CreateSharingDecision(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	var request createSharingDecisionRequest
	if err := commonapi.BindOptionalJSONStrict(c, &request); err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	decisionID, err := parseCanonicalUUID(request.DecisionID)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	version, err := parseCanonicalPositiveInt64(request.Version)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	recipientID, err := parseCanonicalPositiveInt64(request.RecipientID)
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	result, created, err := h.entries.CreateSharingDecision(c.Request.Context(), tenantID, entryAccess(c), id, service.SharingDecisionInput{
		DecisionID: decisionID, Version: version, RecipientType: request.RecipientType, RecipientID: recipientID, ExpiryMode: request.ExpiryMode, ExpiresAt: request.ExpiresAt, Reason: request.Reason,
	}, auth)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, result)
}

// GetSharingDecision reviews an immutable confirmation without reconfirming.
// @Summary 读取共享确认记录 | Read a sharing confirmation record
// @Description 当前可见条目下，原确认人读取本人历史，当前业务负责人可复核全部历史；均须独立共享确认权限。读取不重新确认、不要求办理权、不延长期限、不返回源数据 | Under a currently visible entry, the original confirmer may read own history and the current business owner may review all history, both with independent confirmation permission. No reconfirmation, fulfillment permission requirement, expiry extension or source data
// @Tags Catalog Sharing
// @Produce json
// @Param id path string true "条目 UUID | Entry UUID"
// @Param decision_id path string true "决定 UUID | Decision UUID"
// @Success 200 {object} service.SharingDecisionResult "不可变原记录 | Immutable original record"
// @Failure 400 {object} map[string]interface{} "参数无效 | Invalid parameters"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "缺少独立功能权限 | Missing independent functional permissions"
// @Failure 404 {object} map[string]interface{} "条目或范围内记录不存在或不可见 | Entry or in-scope record missing or invisible"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.sharing_decision.create"]
// @Router /entries/{id}/sharing_decisions/{decision_id} [get]
// @Security BearerAuth
func (h *Handler) GetSharingDecision(c *gin.Context) {
	tenantID, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	if err != nil || c.Request.URL.RawQuery != "" {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	decisionID, err := parseCanonicalUUID(c.Param("decision_id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	result, err := h.entries.GetSharingDecision(c.Request.Context(), tenantID, entryAccess(c), id, decisionID, auth)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListSharingDecisions godoc
// @Summary 查找共享确认历史 | Find sharing confirmation history
// @Description 条目可见且具有独立确认权限；原确认人读本人记录，当前业务负责人复核全部历史，先过滤后分页。不发起办理或授予数据访问 | Requires entry visibility and independent confirmation permission. Original confirmers read own records; the current business owner reviews all history, filtered before pagination. Does not initiate fulfillment or grant data access
// @Tags Catalog Sharing
// @Produce json
// @Security BearerAuth
// @Param id path string true "条目 UUID | Entry UUID"
// @Param page query int false "页码，默认 1 | Page, default 1" default(1)
// @Param page_size query int false "每页数量，默认 20，最多 100 | Page size, default 20, maximum 100" default(20)
// @Success 200 {object} object{data=[]service.SharingDecisionResult,total=int64,page=int,page_size=int,total_pages=int} "可复核的确认历史 | Reviewable confirmation history"
// @Failure 400,401,403,404,500 {object} map[string]interface{} "参数、身份或读取范围错误 | Invalid parameters, identity or read scope"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.sharing_decision.create"]
// @Router /entries/{id}/sharing_decisions [get]
func (h *Handler) ListSharingDecisions(c *gin.Context) {
	tenant, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	page, size, err := sharingPagination(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	rows, total, err := h.entries.ListSharingDecisions(c.Request.Context(), tenant, entryAccess(c), id, auth, page, size)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}

// ListSharingDecisionFulfillments godoc
// @Summary 复核共享确认的办理及签发历史 | Review fulfillment and issuance history for a sharing decision
// @Description 原确认人或当前业务负责人在条目可见及独立确认权限内，只读查询 System 原受理及签发历史。granted_at 不表达当前访问 Allow；依赖失败返回错误，不伪装为 pending。查询不提交、关闭、核清或签发 | Original confirmer or current business owner, with entry visibility and independent confirmation permission, reads System's original acceptance and issuance history. Granted_at is not a current access Allow; dependency failures never become pending. Does not submit, close, settle or issue
// @Tags Catalog Sharing
// @Produce json
// @Security BearerAuth
// @Param id path string true "条目 UUID | Entry UUID"
// @Param decision_id path string true "确认 UUID | Decision UUID"
// @Param page query int false "页码，默认 1 | Page, default 1" default(1)
// @Param page_size query int false "每页数量，默认 20，最多 100 | Page size, default 20, maximum 100" default(20)
// @Success 200 {object} object{data=[]service.SharingFulfillmentHistory,total=int64,page=int,page_size=int,total_pages=int} "只读历史，不代表当前数据访问权 | Read-only history, not current data access"
// @Failure 400,401,403,404,409,500,503 {object} map[string]interface{} "请求、范围、绑定或依赖错误 | Request, scope, binding or dependency error"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["catalog.entry.read","catalog.sharing_decision.create"]
// @Router /entries/{id}/sharing_decisions/{decision_id}/fulfillments [get]
func (h *Handler) ListSharingDecisionFulfillments(c *gin.Context) {
	tenant, ok := commonAuth.TenantIDFromGin(c)
	auth, authenticated := commonAuth.AuthContextFromGin(c)
	if !ok || !authenticated {
		respondError(c, http.StatusUnauthorized, service.ErrUserPrincipalRequired)
		return
	}
	id, err := parseCanonicalUUID(c.Param("id"))
	decisionID, decisionErr := parseCanonicalUUID(c.Param("decision_id"))
	if err != nil || decisionErr != nil {
		respondError(c, http.StatusBadRequest, service.ErrInvalidEntryUpdate)
		return
	}
	page, size, err := sharingPagination(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	rows, total, err := h.entries.ListSharingDecisionFulfillments(c.Request.Context(), tenant, entryAccess(c), id, decisionID, auth, page, size)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}
