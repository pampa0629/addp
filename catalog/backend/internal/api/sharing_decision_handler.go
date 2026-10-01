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

// GetSharingDecision reads the caller's immutable confirmation without reconfirming.
// @Summary 读取本人共享确认记录 | Read own sharing confirmation record
// @Description 仅返回当前可见条目下由当前 User 确认的原记录；历史读取不要求仍为业务负责人。不重新确认、不延长到期时间、不返回 System Grant 或受理状态 | Return only the current User's original decision under a currently visible entry. Historical reads do not require current business ownership. No reconfirmation, expiry extension, System Grant or acceptance state is produced
// @Tags Catalog Sharing
// @Produce json
// @Param id path string true "条目 UUID | Entry UUID"
// @Param decision_id path string true "决定 UUID | Decision UUID"
// @Success 200 {object} service.SharingDecisionResult "不可变原记录 | Immutable original record"
// @Failure 400 {object} map[string]interface{} "参数无效 | Invalid parameters"
// @Failure 401 {object} map[string]interface{} "未认证 | Unauthorized"
// @Failure 403 {object} map[string]interface{} "缺少独立功能权限 | Missing independent functional permissions"
// @Failure 404 {object} map[string]interface{} "条目或本人记录不存在或不可见 | Entry or own record missing or invisible"
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
	if err != nil {
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
