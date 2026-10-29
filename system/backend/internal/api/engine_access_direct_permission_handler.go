package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	engineplugin "github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	modulei18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/engineaccess"
	"github.com/gin-gonic/gin"
)

type engineAccessDirectPermissionService interface {
	SetDirectPermission(context.Context, engineaccess.SetDirectPermissionInput) (*engineaccess.DirectPermissionView, error)
	GetDirectPermission(context.Context, engineaccess.Actor, int64, string) (*engineaccess.DirectPermissionView, error)
	ListDirectPermissions(context.Context, engineaccess.Actor, int64, int, int, engineaccess.DirectPermissionFilter) ([]engineaccess.DirectPermissionIdentityView, int64, error)
}
type EngineAccessDirectPermissionHandler struct {
	service engineAccessDirectPermissionService
}
type SetEngineDirectPermissionRequest struct {
	CatalogPath          engineplugin.EngineCatalogPath `json:"catalog_path" binding:"required"`
	ExpectedBoundaryHash string                         `json:"expected_boundary_hash" binding:"required"`
	Allowed              *bool                          `json:"allowed" binding:"required"`
	ExpectedSource       string                         `json:"expected_source" enums:"explicit,deployment_default,deployment_unknown"`
	ExpectedVersion      string                         `json:"expected_version"`
	Reason               string                         `json:"reason" binding:"required"`
}

// Set godoc
// @Summary 设置精确数据项的直接授权许可 | Set direct authorization permission for an exact DataItem
// @Description 需独立许可管理权限和当前引擎授权管理员资格；普通办理委托不能代替。提交实际观察的来源与版本，显式设置覆盖部署默认。仅控制后续直接签发，不修改已有Grant，不阻断Catalog业务办理，不授予内容读取 | Requires dedicated manage permission and current engine authorization administrator qualification; delegated handlers are insufficient. Submit the observed source and version; explicit settings override deployment defaults. Controls future direct issuance only, without changing Grants, blocking Catalog fulfillment or granting content access
// @Tags 直接授权许可 | Direct Authorization Permissions
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎ID | Engine ID"
// @Param fingerprint path string true "数据项指纹 | DataItem fingerprint"
// @Param request body SetEngineDirectPermissionRequest true "目标、边界、许可值、原观察及原因 | Target, boundary, desired value, prior observation and reason"
// @Success 200 {object} engineaccess.DirectPermissionView "显式许可观察 | Explicit permission observation"
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_direct_permission.update"]
// @Router /engines/{id}/data-items/{fingerprint}/direct-permission [put]
func (h *EngineAccessDirectPermissionHandler) Set(c *gin.Context) {
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	var request SetEngineDirectPermissionRequest
	if c.Request.URL.RawQuery != "" || !authorizationfacts.ValidItemFingerprint(c.Param("fingerprint")) || commonapi.BindOptionalJSONStrict(c, &request) != nil || request.Allowed == nil || strings.TrimSpace(request.Reason) == "" {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	version, err := strconv.ParseInt(request.ExpectedVersion, 10, 64)
	if err != nil || version < 0 || strconv.FormatInt(version, 10) != request.ExpectedVersion {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.SetDirectPermission(c.Request.Context(), engineaccess.SetDirectPermissionInput{Actor: actor, EngineID: engineID,
		CatalogPath: request.CatalogPath, ItemFingerprint: c.Param("fingerprint"), ExpectedBoundaryHash: request.ExpectedBoundaryHash,
		Allowed: *request.Allowed, ExpectedSource: request.ExpectedSource, ExpectedVersion: version, Reason: request.Reason, Audit: iamAuditMetadataWithStatus(c, http.StatusOK)})
	if err != nil {
		respondDirectPermissionError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// Get godoc
// @Summary 查询数据项有效直接授权许可 | Read effective direct permission for a DataItem
// @Description 仅查询Meta已识别身份；显式设置优先，否则使用Catalog持久启用状态，未登记为未知。运行健康不改变默认。不授予读取权，不初始化许可 | Queries a Meta-identified item. Explicit settings win; otherwise persistent Catalog enablement determines the default, with undeclared state unknown. Runtime health never changes this default. No content grant or implicit initialization
// @Tags 直接授权许可 | Direct Authorization Permissions
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎ID | Engine ID"
// @Param fingerprint path string true "数据项指纹 | DataItem fingerprint"
// @Success 200 {object} engineaccess.DirectPermissionView "当次观察，不是凭据 | Current observation, not a credential"
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_direct_permission.read"]
// @Router /engines/{id}/data-items/{fingerprint}/direct-permission [get]
func (h *EngineAccessDirectPermissionHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || !authorizationfacts.ValidItemFingerprint(c.Param("fingerprint")) {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	row, err := h.service.GetDirectPermission(c.Request.Context(), actor, engineID, c.Param("fingerprint"))
	if err != nil {
		respondDirectPermissionError(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

// List godoc
// @Summary 按有效状态查询数据项直接授权许可 | List DataItems by effective direct permission
// @Description 包含未显式配置的数据项；由Meta按身份筛选后计数分页，System组合许可来源及Meta权威父节点链；树仅作导航，不建立节点许可。跨服务查询不持有IAM锁；配置变化返回冲突，故障不返回空清单 | Includes unconfigured items. Meta filters identities before counting and pagination; System combines permission provenance and Meta-owned ancestry for navigation, never node-level permission. No IAM lock crosses the owner request; configuration changes return conflict, and failures never become empty lists
// @Tags 直接授权许可 | Direct Authorization Permissions
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎ID | Engine ID"
// @Param search query string false "名称或路径搜索 | Name or path search"
// @Param item_fingerprint query string false "精确数据项身份 | Exact DataItem identity"
// @Param state query string false "有效许可状态 | Effective permission state" Enums(allowed,denied,unknown)
// @Param page query int false "页码，默认1 | Page, default 1"
// @Param page_size query int false "每页数量，默认20，最多100 | Page size, default 20, maximum 100"
// @Success 200 {object} object{data=[]engineaccess.DirectPermissionIdentityView,total=int64,page=int,page_size=int,total_pages=int} "有效许可清单 | Effective permission list"
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_direct_permission.read"]
// @Router /engines/{id}/direct-permissions [get]
func (h *EngineAccessDirectPermissionHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.ContentLength != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	page, size := 1, 20
	filter := engineaccess.DirectPermissionFilter{}
	for key, values := range query {
		if len(values) != 1 {
			respondIAMError(c, commonapi.ErrBadRequest)
			return
		}
		value := values[0]
		switch key {
		case "search":
			filter.Search = value
		case "item_fingerprint":
			filter.ItemFingerprint = value
		case "state":
			filter.State = value
		case "page", "page_size":
			parsed, e := strconv.Atoi(value)
			if e != nil || parsed <= 0 || strconv.Itoa(parsed) != value {
				respondIAMError(c, commonapi.ErrBadRequest)
				return
			}
			if key == "page" {
				page = parsed
			} else {
				size = parsed
			}
		default:
			respondIAMError(c, commonapi.ErrBadRequest)
			return
		}
	}
	rows, total, err := h.service.ListDirectPermissions(c.Request.Context(), actor, engineID, page, size, filter)
	if err != nil {
		respondDirectPermissionError(c, err)
		return
	}
	commonapi.RespondPaginated(c, rows, total, page, size)
}
func engineAccessActor(c *gin.Context) (engineaccess.Actor, int64, bool) {
	if rejectTenantIDQuery(c) {
		return engineaccess.Actor{}, 0, false
	}
	actor, err := engineDelegationActor(c)
	if err != nil {
		respondIAMError(c, err)
		return actor, 0, false
	}
	engineID, err := parseIAMDecimalID(c.Param("id"))
	if err != nil {
		respondIAMError(c, err)
		return actor, 0, false
	}
	return actor, engineID, true
}
func respondDirectPermissionError(c *gin.Context, err error) {
	if errors.Is(err, engineaccess.ErrDirectPermissionVersion) {
		c.JSON(http.StatusConflict, gin.H{"error": commoni18n.T(c, "system.engine_access_direct_permission.version_conflict"), "error_code": "resource_version_conflict"})
		return
	}
	if errors.Is(err, engineaccess.ErrAuthorizationFactsUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, modulei18n.MsgAuthorizationFactsUnavailable), "error_code": "authorization_facts_unavailable"})
		return
	}
	respondIAMError(c, err)
}
