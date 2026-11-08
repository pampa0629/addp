package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/addp/common/jsonmap"
	commoni18n "github.com/addp/common/middleware/i18n"
	systemi18n "github.com/addp/system/i18n"
	"io"
	"net/http"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/middleware/logging"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type metaMetadataCheckService interface {
	CheckMetaLineageRead(context.Context, string, authorization.SourceMetadataCheckRequest) (*authorization.SourceMetadataCheck, error)
	CheckMetaMetadataRead(context.Context, string, authorization.SourceMetadataCheckRequest) (*authorization.SourceMetadataCheck, error)
	CheckManagerSearchRead(context.Context, string, authorization.SourceSearchCheckRequest) (*authorization.SourceSearchCheck, error)
}

type EngineAccessMetadataCheckHandler struct{ service metaMetadataCheckService }

const managerSearchStartedKey = "addp.system.manager_search_started"

func RegisterEngineAccessMetadataCheckRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessMetadataCheckHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("metadata check dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(engineaccess.MetaCatalogReadPermission)
	if err != nil {
		return err
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess, middleware.IAMTokenTypeDelegatedAccess)
	if err != nil {
		return err
	}
	guards := map[string]gin.HandlerFunc{}
	for _, scope := range []string{"resource.children.list", "resource.ancestors.get"} {
		guard, err := sharedauth.NewDelegatedRouteGuard(sharedauth.DelegatedRouteGuardConfig{Audience: "meta", RequiredScopes: []string{scope}, RequiredPermissions: []string{engineaccess.MetaCatalogReadPermission}})
		if err != nil {
			return err
		}
		guards[scope] = guard
	}
	delegated := func(c *gin.Context) {
		current, ok := middleware.IAMAuthContextFromGin(c)
		if !ok {
			respondIAMError(c, commonapi.ErrUnauthorized)
			c.Abort()
			return
		}
		if current.Token.Type != middleware.IAMTokenTypeDelegatedAccess {
			c.Next()
			return
		}
		if len(current.Client.Scopes) != 1 || guards[current.Client.Scopes[0]] == nil {
			respondIAMError(c, commonapi.ErrForbidden)
			c.Abort()
			return
		}
		guards[current.Client.Scopes[0]](c)
	}
	api.POST("/engine-access/metadata-checks/meta-query", runtime.Authentication, credential, tenant, delegated, permission, handler.CheckMetaQuery)
	lineagePermission, err := middleware.NewIAMPermissionGuard(engineaccess.MetaLineageReadPermission)
	if err != nil {
		return err
	}
	api.POST("/engine-access/metadata-checks/meta-lineage", runtime.Authentication, runtime.UserAccessCredential, tenant, lineagePermission, handler.CheckMetaLineage)
	searchPermission, err := middleware.NewIAMPermissionGuard(engineaccess.ManagerSearchPermission)
	if err != nil {
		return err
	}
	searchDelegate, err := sharedauth.NewDelegatedRouteGuard(sharedauth.DelegatedRouteGuardConfig{Audience: "manager", RequiredScopes: []string{"data.search"}, RequiredPermissions: []string{engineaccess.ManagerSearchPermission}})
	if err != nil {
		return err
	}
	api.POST("/engine-access/metadata-checks/manager-search", func(c *gin.Context) {
		c.Set(managerSearchStartedKey, time.Now())
		c.Next()
	}, runtime.Authentication, credential, tenant, searchDelegate, searchPermission, handler.CheckManagerSearch)
	return nil
}

// CheckMetaQuery godoc
// @Summary 检查 Meta 查询候选的当前元数据可见性 | Check current metadata visibility for Meta query candidates
// @Description 当前 Tenant 第一方或 OAuth API User，或唯一 meta audience 且仅含 resource.children.list 或 resource.ancestors.get 一种 Scope 的真实委托凭据；固定核验 meta.catalog.read 和可靠身份的 DataItem 当前规则。拒绝机器、资源票据、自报身份或边界 | Current Tenant first-party or OAuth API User, or a real delegate with sole meta audience and exactly one resource.children.list or resource.ancestors.get scope. Checks fixed meta.catalog.read and current rules for reliably identified DataItems. Rejects Service, Resource Ticket and caller-supplied identity or boundaries
// @Description 每个候选只返回原目标及可见布尔值；不返回授权来源或拒绝原因，不授予内容读取或扫描权限，不构成可缓存许可。调用方须在分页计数及安全字段投影前应用结果 | Returns each original candidate and its visibility only, without rule sources or denial reasons. Grants neither content read nor scan authority and is not a cacheable lease. Consumers must apply results before pagination, counting and safe-field projection
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorization.SourceMetadataCheckRequest true "1 至 200 个不重复完整目标，不支持 query 参数 | 1 to 200 unique complete targets; no query parameters"
// @Success 200 {object} authorization.SourceMetadataCheck "当次可见性观察 | Current visibility observation"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "身份、边界或依赖核验失败 | Identity, boundary or dependency validation failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.catalog.read"]
// @Router /engine-access/metadata-checks/meta-query [post]
func (h *EngineAccessMetadataCheckHandler) CheckMetaQuery(c *gin.Context) {
	h.check(c, h.service.CheckMetaMetadataRead)
}

// CheckMetaLineage godoc
// @Summary 检查血缘端点可见性 | Check lineage endpoint visibility
// @Description 仅当前 Tenant 第一方或 OAuth User，固定核验 meta.lineage.read 与当前元数据权限；拒绝委托、机器和资源票据。不返回服务可见性或可缓存许可 | Current Tenant first-party or OAuth User only; checks fixed meta.lineage.read and current metadata access. Rejects delegates, Service and resource tickets. Returns neither service visibility nor cacheable authority
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorization.SourceMetadataCheckRequest true "1 至 200 个完整目标 | 1 to 200 complete targets"
// @Success 200 {object} authorization.SourceMetadataCheck "当次可见性 | Current visibility"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "核验失败 | Check failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.lineage.read"]
// @Router /engine-access/metadata-checks/meta-lineage [post]
func (h *EngineAccessMetadataCheckHandler) CheckMetaLineage(c *gin.Context) {
	h.check(c, h.service.CheckMetaLineageRead)
}

// CheckManagerSearch godoc
// @Summary 检查 Manager 检索候选的当前可见性 | Check current visibility for Manager search candidates
// @Description 接受当前 Tenant User 或唯一 manager audience 和 data.search Scope 的委托，固定核验 manager.search.execute。逐项返回元数据及内容读取裁决，仅用于当前检索 | Accepts a current Tenant User or a delegate with sole manager audience and data.search scope. Checks fixed manager.search.execute and returns per-target metadata and content decisions for the current search only
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorization.SourceSearchCheckRequest true "0 至 200 个目标、查询随机量及显式可空的期望观察标记 | 0 to 200 targets, query nonce and explicitly nullable expected observation tag"
// @Success 200 {object} authorization.SourceSearchCheck "当次检索授权观察 | Current search access observation"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "核验失败 | Check failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.search.execute"]
// @Router /engine-access/metadata-checks/manager-search [post]
func (h *EngineAccessMetadataCheckHandler) CheckManagerSearch(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	started := time.Now()
	request, ok := bindSearchCheck(c)
	if !ok {
		return
	}
	requestBinding := time.Since(started)
	ctx, timing := engineaccess.ObserveManagerSearchTiming(c.Request.Context())
	result, err := h.service.CheckManagerSearchRead(ctx, sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), request)
	if errors.Is(err, engineaccess.ErrSearchContextChanged) {
		code := "search_context_changed"
		c.JSON(http.StatusConflict, IAMErrorResponse{Error: commoni18n.T(c, systemi18n.MsgSearchContextChanged), ErrorCode: &code})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	responseStarted := time.Now()
	if !result.Matches(request) {
		respondIAMError(c, errors.New("incomplete search observation"))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
	if routeStarted := c.GetTime(managerSearchStartedKey); !routeStarted.IsZero() {
		logging.SetCompletionDiagnostics(c, timing.LogGroup(started.Sub(routeStarted), requestBinding, time.Since(responseStarted)))
	}
}

func (h *EngineAccessMetadataCheckHandler) check(c *gin.Context, observe func(context.Context, string, authorization.SourceMetadataCheckRequest) (*authorization.SourceMetadataCheck, error)) {
	request, ok := bindMetadataCheck(c)
	if !ok {
		return
	}
	result, err := observe(c.Request.Context(), sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), request)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if !result.Matches(request) {
		respondIAMError(c, errors.New("incomplete metadata observation"))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func bindMetadataCheck(c *gin.Context) (authorization.SourceMetadataCheckRequest, bool) {
	var request authorization.SourceMetadataCheckRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil || !request.Valid() {
		respondIAMError(c, commonapi.ErrBadRequest)
		return request, false
	}
	return request, true
}

func bindSearchCheck(c *gin.Context) (authorization.SourceSearchCheckRequest, bool) {
	var request authorization.SourceSearchCheckRequest
	encoded, err := io.ReadAll(io.LimitReader(c.Request.Body, 512<<10+1))
	shape, shapeErr := jsonmap.DecodeObjectStrict(encoded, 512<<10, 24)
	_, targets := shape["targets"]
	_, nonce := shape["query_nonce"]
	_, expected := shape["expected_observation_tag"]
	if c.Request.URL.RawQuery != "" || err != nil || shapeErr != nil || len(shape) != 3 || !targets || !nonce || !expected || json.Unmarshal(encoded, &request) != nil || !request.Valid() {
		respondIAMError(c, commonapi.ErrBadRequest)
		return request, false
	}
	return request, true
}
