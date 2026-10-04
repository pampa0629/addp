package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type engineAccessDenyService interface {
	CreateDeny(context.Context, engineaccess.CreateDenyInput) (*engineaccess.SourceDeny, error)
	ReleaseDeny(context.Context, engineaccess.ReleaseDenyInput) (*engineaccess.DenyRelease, error)
}

type ReleaseEngineAccessDenyRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// Release godoc
// @Summary 解除指定源读取拒绝规则 | Release a specific source read Deny
// @Description 当前租户用户须同时具备独立解除权限和有效引擎管理委派，不要求为原建立人；停用引擎及失效接收主体不阻断解除，不访问源端或 Catalog。仅追加不可变解除记录，不创建、恢复或续期 Grant | Current tenant user needs independent release Permission and effective engine management delegation, not original creator identity. Disabled engines and unavailable recipients permit release without source or Catalog IO. Adds immutable release history without creating, restoring or renewing Grants
// @Description 自然到期后首次解除返回 409 engine_access_deny_expired，无新记录或成功审计；到期前已解除的同操作者、同成员关系及同规范化原因重试返回原历史，当前资格仍须有效。异参返回 409 engine_access_deny_release_conflict，跨租户或引擎隐藏为 404 | First release after natural expiry returns 409 engine_access_deny_expired with no history or success audit; identical operator, membership and normalized reason retries recover earlier history subject to current qualification. Changed parameters return 409 engine_access_deny_release_conflict; other tenant or engine history stays hidden as 404
// @Tags 源数据显式拒绝 | Source Data Explicit Deny
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param deny_id path string true "原拒绝 UUID | Original Deny UUID"
// @Param request body ReleaseEngineAccessDenyRequest true "解除原因 | Release reason"
// @Success 200 {object} engineaccess.DenyRelease "不可变解除事实，不代表允许访问 | Immutable release fact, not an access verdict"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_deny.release"]
// @Router /engines/{id}/access_denies/{deny_id}/release [post]
func (h *EngineAccessDenyHandler) Release(c *gin.Context) {
	if rejectTenantIDQuery(c) {
		return
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	id, err := uuid.Parse(c.Param("deny_id"))
	var request ReleaseEngineAccessDenyRequest
	if err != nil || id == uuid.Nil || id.String() != c.Param("deny_id") || commonapi.BindOptionalJSONStrict(c, &request) != nil ||
		strings.TrimSpace(request.Reason) == "" || utf8.RuneCountInString(request.Reason) > 2000 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.ReleaseDeny(c.Request.Context(), engineaccess.ReleaseDenyInput{Actor: actor, EngineID: engineID,
		DenyID: id, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if errors.Is(err, engineaccess.ErrDenyReleaseExpired) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgDenyReleaseExpired), "error_code": "engine_access_deny_expired"})
		return
	}
	if errors.Is(err, engineaccess.ErrDenyReleaseConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgDenyReleaseConflict), "error_code": "engine_access_deny_release_conflict"})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

type EngineAccessDenyHandler struct{ service engineAccessDenyService }

type CreateEngineAccessDenyRequest struct {
	DenyID        string                         `json:"deny_id"`
	CatalogPath   engineplugin.EngineCatalogPath `json:"catalog_path"`
	RecipientType string                         `json:"recipient_type" enums:"user,department,project_group"`
	RecipientID   string                         `json:"recipient_id"`
	Action        string                         `json:"action" enums:"read"`
	ExpiryMode    string                         `json:"expiry_mode" enums:"at_time,until_revoked"`
	ExpiresAt     *time.Time                     `json:"expires_at"`
	Reason        string                         `json:"reason"`
}

// Create godoc
// @Summary 建立精确源读取拒绝规则 | Establish a precise source read Deny
// @Description 当前租户用户须同时具备独立创建权限和有效引擎管理委派；接收主体须有效，路径必须是精确叶子。停用引擎仍可建立拒绝，不访问源端或 Catalog | Current tenant user needs independent creation Permission and effective engine management delegation; recipient must be current and path a precise leaf. Disabled engines permit Deny establishment without source or Catalog IO
// @Description 必须显式选择未来到期时间或长期有效；同编号同参数重试恢复原不可变记录，不延长期限或重复审计。记录不证明执行侧已经拒绝数据读取 | Explicit future expiry or until-revoked mode is required; identical command retries recover immutable history without renewal or duplicate audit. History is not proof of execution-side access denial
// @Tags 源数据显式拒绝 | Source Data Explicit Deny
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request body CreateEngineAccessDenyRequest true "拒绝编号、精确路径、主体、动作、期限与原因 | Deny ID, precise path, subject, action, expiry and reason"
// @Success 200 {object} engineaccess.SourceDeny "原有或新建的不可变拒绝事实 | Original or newly established immutable Deny"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_deny.create"]
// @Router /engines/{id}/access_denies [post]
func (h *EngineAccessDenyHandler) Create(c *gin.Context) {
	if rejectTenantIDQuery(c) {
		return
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var request CreateEngineAccessDenyRequest
	if commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	id, err := uuid.Parse(request.DenyID)
	if err != nil || id == uuid.Nil || id.String() != request.DenyID {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	recipientID, err := parseIAMDecimalID(request.RecipientID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	row, err := h.service.CreateDeny(c.Request.Context(), engineaccess.CreateDenyInput{Actor: actor, EngineID: engineID,
		DenyID: id, CatalogPath: request.CatalogPath, RecipientType: request.RecipientType, RecipientID: recipientID,
		Action: request.Action, ExpiryMode: request.ExpiryMode, ExpiresAt: request.ExpiresAt, Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if errors.Is(err, engineaccess.ErrDenyConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgDenyConflict), "error_code": "engine_access_deny_conflict"})
		return
	}
	if errors.Is(err, engineaccess.ErrDenyExpiry) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgDenyExpiry), "error_code": "engine_access_deny_expiry"})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
