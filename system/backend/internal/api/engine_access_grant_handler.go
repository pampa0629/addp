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
	ListSourceGrants(context.Context, engineaccess.Actor, int64, int, int, engineaccess.SourceGrantFilter) ([]engineaccess.SourceGrantView, int64, error)
	ListSourceGrantRelations(context.Context, engineaccess.Actor, int64, int, int, engineaccess.SourceGrantFilter) ([]engineaccess.SourceGrantRelation, int64, error)
	InspectSourceGrants(context.Context, engineaccess.Actor, int64, int64, engineplugin.EngineCatalogPath) (*engineaccess.SourceGrantInspection, error)
}

type InspectSourceGrantsRequest struct {
	AccountID   string                         `json:"account_id"`
	CatalogPath engineplugin.EngineCatalogPath `json:"catalog_path"`
}

// Inspect godoc
// @Summary 核查账号的源数据授权来源 | Inspect an account's source-data grant sources
// @Description 当前租户用户须授权读取权限及引擎管理资格；选择同租户账号和精确普通表，使用实际读取的唯一源规则展开当前有效个人、部门及项目组授权。拒绝仅返回固定命中结论，不返回规则正文或编号 | Current Tenant User needs grant-read permission and engine management qualification. Uses the actual-read rule query to inspect current personal, department and project-group sources for a same-tenant account and exact ordinary table. Deny is exposed only as a fixed reason, never its body or ID
// @Description 只读观察不连接源库、不授权、不核验接收方实际会话、功能或 Security；rule_covered 不是实际访问许可，响应不可缓存 | Read-only observation never connects to the source or issues access, and does not validate the recipient's real session, function permissions or Security. rule_covered is not actual access; response is not cacheable
// @Tags 源数据授权 | Source Data Grants
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param request body InspectSourceGrantsRequest true "账号与精确表 | Account and exact table"
// @Success 200 {object} engineaccess.SourceGrantInspection "当次源规则观察 | Current source-rule observation"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/access_grants/inspection [post]
func (h *EngineAccessGrantHandler) Inspect(c *gin.Context) {
	actor, engineID, ok := approvalRequirementActor(c)
	if !ok {
		return
	}
	var request InspectSourceGrantsRequest
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	accountID, err := parseIAMDecimalID(request.AccountID)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	result, err := h.service.InspectSourceGrants(c.Request.Context(), actor, engineID, accountID, request.CatalogPath)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

type EngineAccessGrantHandler struct{ service engineAccessGrantService }

type RevokeEngineAccessGrantRequest struct {
	Reason string `json:"reason" binding:"required"`
}

type CreateIndependentEngineAccessGrantRequest struct {
	RequestID          string                         `json:"request_id"`
	CatalogPath        engineplugin.EngineCatalogPath `json:"catalog_path"`
	RequirementVersion string                         `json:"requirement_version"`
	InitializeApproval bool                           `json:"initialize_approval"`
	RecipientType      string                         `json:"recipient_type" enums:"user,department,project_group"`
	RecipientID        string                         `json:"recipient_id"`
	Action             string                         `json:"action" enums:"read"`
	ExpiryMode         string                         `json:"expiry_mode" enums:"at_time,until_revoked"`
	ExpiresAt          *time.Time                     `json:"expires_at"`
	Reason             string                         `json:"reason"`
}

// Create godoc
// @Summary 显式签发独立只读授权 | Issue explicit independent read access
// @Description 当前 Tenant User 需独立授予权限及管理资格（当前租户授权管理员或本引擎受托办理人）；精确普通表的批准要求必须为 independent 且与原版本匹配。不要求企业 Catalog 或 Meta 编目，结构核验只读且不读取样本 | Current Tenant User needs creation permission and management qualification (current-tenant authorization administrator or delegated engine handler). Exact ordinary table approval must be independent at the expected version. No enterprise Catalog or Meta cataloging; structure verification is read-only without sampling
// @Description 同命令同操作者同参数重试仅恢复原签发及撤销事实，不续期或恢复读取。不同参数 409；成功不替代消费侧功能权限、Deny、当前主体和安全策略 | Identical command, operator and parameters recover issuance and revocation history without renewal or restored access. Changed parameters return 409; success does not replace consumer permissions, Deny, current identity or security policy
// @Description 同目标、接收方和动作已有有效授权时，新命令返回 409 engine_access_grant_relation_exists，不重复发放或静默变更期限；包括业务批准来源 | A new command returns 409 engine_access_grant_relation_exists when the same target, recipient and action already have active access, including business-approved Grants. No duplicate issuance or silent validity change
// @Description 授权管理员不需委托自己，受托办理员需有效引擎委托。initialize_approval=true 须额外具有首次配置权限且版本为 1；首次配置与 Grant 原子提交，不覆盖已有要求 | Authorization administrators need no self-delegation; handlers need an effective engine delegation. initialize_approval=true additionally requires initialization permission and version 1; configuration and Grant commit atomically without overwriting an existing basis
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
// @x-addp-conditional-permissions ["system.engine_access_approval_requirement.initialize"]
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
		Actor: actor, EngineID: engineID, RequestID: id, CatalogPath: request.CatalogPath, RequirementVersion: version, InitializeApproval: request.InitializeApproval,
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
// @Summary 查看当前源数据授权关系 | List current source-data authorization relations
// @Description 表名和账号条件取交集，完整结果筛选后计数及分页；账号只匹配个人授权，不展开组织来源 | Table and account filters intersect before counting and pagination; account matches personal Grants only, not organization sources
// @Description 按精确目标、接收方和动作聚合未到期、未撤销的记录；长期有效优先，否则显示最晚到期。包含存量重复数量；不是实际访问裁决，不读取源端。需读取权限及当前管理资格 | Groups unexpired, unrevoked Grants by exact target, recipient and action. Until-revoked dominates; otherwise latest expiry. Includes legacy duplicate count; not an effective access verdict and no source IO. Requires read permission and current management qualification
// @Tags 源数据授权 | Source Data Grants
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页条数，默认 20，最多 100 | Page size, default 20, maximum 100"
// @Param table_search query string false "路径名称的字面量子串，不区分大小写，去掉首尾空白，最多 200 字 | Case-insensitive literal path-name substring, trimmed, up to 200 characters"
// @Param account_id query string false "接收账号 Principal ID，规范正整数十进制字符串 | Recipient account Principal ID, canonical positive decimal string"
// @Success 200 {object} object{data=[]engineaccess.SourceGrantRelation,total=int64,page=int,page_size=int,total_pages=int} "当前关系 | Current relations"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/access_grants [get]
func (h *EngineAccessGrantHandler) List(c *gin.Context) {
	h.list(c, false)
}

// History godoc
// @Summary 查看源数据授权历史 | List source-data Grant history
// @Description 表名和账号条件取交集，完整结果筛选后计数及分页；包含已停用账号的历史个人授权，不展开组织来源 | Table and account filters intersect before counting and pagination; includes inactive-account personal history, not organization sources
// @Description 当前租户用户需读取权限及管理资格；保留所有原签发、到期与撤销事实，包含直接和业务批准，不代表当前可访问 | Current tenant user needs read permission and management qualification. Preserves original issuance, expiry and revocation for direct and business approvals; not current access Allow
// @Tags 源数据授权 | Source Data Grants
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码，默认 1 | Page, default 1"
// @Param page_size query int false "每页条数，默认 20，最多 100 | Page size, default 20, maximum 100"
// @Param table_search query string false "路径名称的字面量子串，不区分大小写，去掉首尾空白，最多 200 字 | Case-insensitive literal path-name substring, trimmed, up to 200 characters"
// @Param account_id query string false "接收账号 Principal ID，规范正整数十进制字符串 | Recipient account Principal ID, canonical positive decimal string"
// @Success 200 {object} object{data=[]engineaccess.SourceGrantView,total=int64,page=int,page_size=int,total_pages=int} "签发及撤销历史 | Issuance and revocation history"
// @Failure 400,401,403,404,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/access_grants/history [get]
func (h *EngineAccessGrantHandler) History(c *gin.Context) { h.list(c, true) }

func (h *EngineAccessGrantHandler) list(c *gin.Context, history bool) {
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
	filter := engineaccess.SourceGrantFilter{}
	for key, values := range query {
		if len(values) != 1 {
			err = commonapi.ErrBadRequest
			break
		}
		if key == "table_search" {
			filter.TableSearch = strings.TrimSpace(values[0])
			if filter.TableSearch == "" {
				err = commonapi.ErrBadRequest
				break
			}
			continue
		}
		if key == "account_id" {
			filter.AccountID, err = parseIAMDecimalID(values[0])
			if err != nil {
				break
			}
			continue
		}
		if key != "page" && key != "page_size" {
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
	if validationErr := filter.Validate(); validationErr != nil {
		err = validationErr
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	var rows any
	var total int64
	if history {
		rows, total, err = h.service.ListSourceGrants(c.Request.Context(), actor, engineID, page, size, filter)
	} else {
		rows, total, err = h.service.ListSourceGrantRelations(c.Request.Context(), actor, engineID, page, size, filter)
	}
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
		{engineaccess.ErrGrantRelationExists, "engine_access_grant_relation_exists", modulei18n.MsgGrantRelationExists, 409},
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
// @Summary 收回整条源数据授权关系 | Withdraw a complete source-data authorization relation
// @Description 当前租户用户需管理资格及撤销权限；原子收回定位记录的同目标、同接收方、同动作全部有效 Grant，保留历史与编号集合。同参重试只恢复原结果，不影响后来新授予；个人撤销不删除组织授权、不建立 Deny | Current tenant user needs management qualification and revocation permission. Atomically withdraws all active Grants for the anchor's exact target, recipient and action, preserving history and withdrawn IDs. Identical retry recovers only the original result, not later Grants. Personal withdrawal never deletes organization Grants or creates Deny
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
