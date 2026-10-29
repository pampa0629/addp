package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type authorizationFactsService interface {
	ListAuthorizationItems(context.Context, engineaccess.Actor, int64, int, int) (*authorizationfacts.IdentityPage, error)
	GetAuthorizationFacts(context.Context, engineaccess.Actor, int64, string) (*authorizationfacts.AuthorizationFacts, error)
}

// This route registration belongs to the canonical *_routes.go discovery.
type EngineAccessAuthorizationFactsHandler struct{ service authorizationFactsService }

func RegisterEngineAccessAuthorizationFactsRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *EngineAccessAuthorizationFactsHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || h == nil || h.service == nil {
		return errors.New("authorization facts route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessGrantRead)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/data-items")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.GET("", permission, h.List)
	routes.GET("/:fingerprint/authorization-facts", permission, h.Get)
	return nil
}

// Get godoc
// @Summary 核对数据项授权边界 | Inspect a DataItem authorization boundary
// @Description 当前 Tenant User 需源授权读取功能及本引擎管理资格。System 用独立 addp-system 身份读取 Meta 后再次核验用户资格；识别完整不代表获权，不扫描或签发 Grant | Requires current tenant User, source-grant read permission and engine management qualification. System reads Meta as addp-system then rechecks the user; complete facts are not an Allow. No scan or Grant issuance
// @Tags 源数据授权 | Source Data Grants
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param fingerprint path string true "数据项指纹 | DataItem fingerprint"
// @Success 200 {object} authorizationfacts.AuthorizationFacts "授权边界事实 | Authorization boundary facts"
// @Failure 404 {object} IAMErrorResponse "authorization_target_not_found：目标不存在或不属于当前引擎 | Target is missing or belongs to another engine"
// @Failure 400,401,403,500,503 {object} IAMErrorResponse "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/data-items/{fingerprint}/authorization-facts [get]
func (h *EngineAccessAuthorizationFactsHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.ContentLength != 0 || !authorizationfacts.ValidItemFingerprint(c.Param("fingerprint")) {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	facts, err := h.service.GetAuthorizationFacts(c.Request.Context(), actor, engineID, c.Param("fingerprint"))
	if errors.Is(err, engineaccess.ErrAuthorizationFactsUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": commoni18n.T(c, sysi18n.MsgAuthorizationFactsUnavailable), "error_code": "authorization_facts_unavailable"})
		return
	}
	if errors.Is(err, commonapi.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": commoni18n.T(c, sysi18n.MsgAuthorizationTargetNotFound), "error_code": "authorization_target_not_found"})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, facts)
}

// List godoc
// @Summary 查询授权候选数据项 | List authorization DataItem identities
// @Description 仅返回 Meta 已识别的数据项及祖先节点。需当前租户用户、源授权读取功能及引擎管理资格；读取前后复核。无原生文件枚举、内容读取或签发 | Returns only Meta identities and ancestor nodes. Rechecks current tenant user and engine management qualification before and after the owner query. No native file enumeration, content reads or issuance.
// @Tags 源数据授权 | Source Data Grants
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎 ID | Engine ID"
// @Param page query int false "页码 | Page number" default(1) minimum(1)
// @Param page_size query int false "每页数量 | Page size" default(100) minimum(1) maximum(100)
// @Success 200 {object} authorizationfacts.IdentityPage "数据项身份清单 | DataItem identities"
// @Failure 400,401,403,500,503 {object} IAMErrorResponse "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/data-items [get]
func (h *EngineAccessAuthorizationFactsHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	if c.Request.ContentLength != 0 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	page, size := 1, 100
	for key, values := range c.Request.URL.Query() {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			respondIAMError(c, commonapi.ErrBadRequest)
			return
		}
		value, err := strconv.Atoi(values[0])
		if err != nil || value <= 0 || strconv.Itoa(value) != values[0] {
			respondIAMError(c, commonapi.ErrBadRequest)
			return
		}
		if key == "page" {
			page = value
		} else {
			size = value
		}
	}
	result, err := h.service.ListAuthorizationItems(c.Request.Context(), actor, engineID, page, size)
	if err != nil {
		respondDirectPermissionError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
