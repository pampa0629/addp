package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
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

type engineAccessGrantService interface {
	RevokeGrant(context.Context, engineaccess.RevokeGrantInput) (*engineaccess.GrantRevocation, error)
	CreateIndependentGrant(context.Context, engineaccess.CreateIndependentGrantInput) (*engineaccess.SourceGrantView, error)
	ListSourceGrants(context.Context, engineaccess.Actor, int64, int, int) ([]engineaccess.SourceGrantView, int64, error)
}

type EngineAccessGrantHandler struct{ service engineAccessGrantService }

type RevokeEngineAccessGrantRequest struct {
	Reason string `json:"reason" binding:"required"`
}

type CreateIndependentEngineAccessGrantRequest struct {
	RequestID          string                         `json:"request_id"`
	CatalogPath        engineplugin.EngineCatalogPath `json:"catalog_path"`
	RequirementVersion string                         `json:"requirement_version"`
	RecipientType      string                         `json:"recipient_type" enums:"user,department,project_group"`
	RecipientID        string                         `json:"recipient_id"`
	Action             string                         `json:"action" enums:"read"`
	ExpiryMode         string                         `json:"expiry_mode" enums:"at_time,until_revoked"`
	ExpiresAt          *time.Time                     `json:"expires_at"`
	Reason             string                         `json:"reason"`
}

// Create godoc
// @Summary 显式签发独立只读授权 | Issue explicit independent read access
// @Description 当前 Tenant User 需独立授予权限及有效引擎管理委派；精确普通表的批准要求必须为 independent 且与原版本匹配。不要求企业 Catalog 或 Meta 编目，结构核验只读且不读取样本 | Current Tenant User needs creation permission and effective engine delegation. Exact ordinary table approval must be independent at the expected version. No enterprise Catalog or Meta cataloging; structure verification is read-only without sampling
// @Description 同命令同操作者同参数重试仅恢复原签发及撤销事实，不续期或恢复读取。不同参数 409；成功不替代消费侧功能权限、Deny、当前主体和安全策略 | Identical command, operator and parameters recover issuance and revocation history without renewal or restored access. Changed parameters return 409; success does not replace consumer permissions, Deny, current identity or security policy
// @Tags 源数据授权 | Source Data Grants
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request body CreateIndependentEngineAccessGrantRequest true "显式命令、精确表、批准版本、接收方、只读动作、期限与原因 | Explicit command, exact table, approval version, recipient, read action, expiry and reason"
// @Success 201 {object} engineaccess.SourceGrantView "已提交的签发历史，不是访问凭据 | Committed issuance history, not an access credential"
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.create"]
// @Router /engines/{id}/access_grants [post]
func (h *EngineAccessGrantHandler) Create(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	var request CreateIndependentEngineAccessGrantRequest
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	id, err := uuid.Parse(request.RequestID)
	recipientID, recipientErr := parseIAMDecimalID(request.RecipientID)
	version, versionErr := parseIAMDecimalID(request.RequirementVersion)
	if err != nil || id == uuid.Nil || id.String() != request.RequestID || recipientErr != nil || versionErr != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.CreateIndependentGrant(c.Request.Context(), engineaccess.CreateIndependentGrantInput{
		Actor: actor, EngineID: engineID, RequestID: id, CatalogPath: request.CatalogPath, RequirementVersion: version,
		RecipientType: request.RecipientType, RecipientID: recipientID, Action: request.Action,
		ExpiryMode: request.ExpiryMode, ExpiresAt: request.ExpiresAt, Reason: request.Reason,
		Audit: iamAuditMetadataWithStatus(c, http.StatusCreated)})
	if err != nil {
		respondIndependentGrantError(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

// List godoc
// @Summary 查看源数据授权历史 | List source-data Grant history
// @Description 当前租户用户需独立读取权限及有效引擎管理委派；同时显示独立批准和 Catalog 来源、期限与撤销历史，停用引擎仍可查看。不是当前访问裁决，不读取源端 | Current Tenant User needs history-read permission and effective engine delegation. Lists independent and Catalog issuance, expiry and revocation history, including disabled engines. Not a current access verdict; no source IO
// @Tags 源数据授权 | Source Data Grants
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页条数，默认 20，最多 100 | Page size, default 20, maximum 100"
// @Success 200 {object} object{data=[]engineaccess.SourceGrantView,total=int64,page=int,page_size=int,total_pages=int} "签发及撤销历史 | Issuance and revocation history"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/access_grants [get]
func (h *EngineAccessGrantHandler) List(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	page, size := 1, 20
	for key, values := range query {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			err = commonapi.ErrBadRequest
			break
		}
		value, parseErr := strconv.Atoi(values[0])
		if parseErr != nil || value <= 0 || strconv.Itoa(value) != values[0] {
			err = commonapi.ErrBadRequest
			break
		}
		if key == "page" {
			page = value
		} else {
			size = value
		}
	}
	if size > 100 || page > int(^uint(0)>>1)/size {
		err = commonapi.ErrBadRequest
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	rows, total, err := h.service.ListSourceGrants(c.Request.Context(), actor, engineID, page, size)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}

func respondIndependentGrantError(c *gin.Context, err error) {
	for _, value := range []struct {
		domain  error
		code    string
		message string
		status  int
	}{
		{engineaccess.ErrIndependentGrantConflict, "engine_access_grant_command_conflict", modulei18n.MsgIndependentGrantConflict, 409},
		{engineaccess.ErrIndependentGrantBasis, "engine_access_grant_approval_changed", modulei18n.MsgIndependentGrantBasis, 409},
		{engineaccess.ErrIndependentGrantExpiry, "engine_access_grant_expiry_elapsed", modulei18n.MsgIndependentGrantExpiry, 409},
		{engineaccess.ErrIndependentGrantSourceUnavailable, "engine_access_grant_source_unavailable", modulei18n.MsgIndependentGrantSourceUnavailable, 503},
	} {
		if errors.Is(err, value.domain) {
			c.JSON(value.status, gin.H{"error": commoni18n.T(c, value.message), "error_code": value.code})
			return
		}
	}
	respondIAMError(c, err)
}

// Revoke godoc
// @Summary 撤销指定源数据 Grant | Revoke a specific source-data Grant
// @Description 当前租户用户须同时具备有效引擎管理委派和独立撤销权限；停用引擎仍可撤销。只收回此 Grant，同参重试恢复原撤销记录，不影响其他独立授权 | Current tenant user needs an effective engine management delegation and independent revocation Permission, even for disabled engines. Withdraws only this Grant; identical retries return original history without affecting independent Grants
// @Tags 源数据授权撤销 | Source Data Grant Revocation
// @Description 自然到期后首次撤销返回 409 engine_access_grant_expired，不写撤销事实；到期前已撤销的同参重试仍须当前资格有效并返回原记录。长期有效仍可撤销，五分钟办理窗口不限制撤销 | First revocation after natural expiry returns 409 engine_access_grant_expired without writing history; retries of an earlier revocation require current qualification and return the original record. Until-revoked Grants remain revocable; the five-minute fulfillment window does not limit revocation
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request_id path string true "授权签发命令 UUID | Grant issuance command UUID"
// @Param request body RevokeEngineAccessGrantRequest true "撤销原因 | Revocation reason"
// @Success 200 {object} engineaccess.GrantRevocation "不可变撤销事实 | Immutable revocation fact"
// @Failure 400,401,403,404,409,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.revoke"]
// @Router /engines/{id}/access_grants/{request_id}/revoke [post]
func (h *EngineAccessGrantHandler) Revoke(c *gin.Context) {
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
	id, err := uuid.Parse(c.Param("request_id"))
	var request RevokeEngineAccessGrantRequest
	if err != nil || id == uuid.Nil || id.String() != c.Param("request_id") || commonapi.BindOptionalJSONStrict(c, &request) != nil ||
		strings.TrimSpace(request.Reason) == "" || utf8.RuneCountInString(request.Reason) > 2000 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.RevokeGrant(c.Request.Context(), engineaccess.RevokeGrantInput{Actor: actor, EngineID: engineID,
		RequestID: id, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if errors.Is(err, engineaccess.ErrGrantRevocationExpired) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgGrantRevocationExpired), "error_code": "engine_access_grant_expired"})
		return
	}
	if errors.Is(err, engineaccess.ErrGrantRevocationConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, modulei18n.MsgGrantRevocationConflict), "error_code": "engine_access_grant_revocation_conflict"})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}
