package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	commonapi "github.com/addp/common/api"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/common/middleware/logging"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type sourceReadCheckService interface {
	CheckManagerPreviewRead(context.Context, string, engineaccess.SourceReadCheckRequest) (*engineaccess.SourceReadCheck, error)
	CheckManagerProfileResultRead(context.Context, string, engineaccess.SourceReadCheckRequest) (*engineaccess.SourceReadCheck, error)
	CheckDevelopWorkflowResultRead(context.Context, string, engineaccess.SourceReadCheckRequest) (*engineaccess.SourceReadCheck, error)
}

type EngineAccessSourceReadCheckHandler struct {
	service sourceReadCheckService
}

func RegisterEngineAccessSourceReadCheckRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessSourceReadCheckHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("source read check dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	credential, err := middleware.NewIAMCredentialGuard(middleware.IAMTokenTypeFirstPartyAccess, middleware.IAMTokenTypeOAuthAccess, middleware.IAMTokenTypeDelegatedAccess)
	if err != nil {
		return err
	}
	delegated, err := sharedauth.NewDelegatedRouteGuard(sharedauth.DelegatedRouteGuardConfig{
		Audience: "manager", RequiredScopes: []string{"data.preview"}, RequiredPermissions: []string{engineaccess.ManagerPreviewReadPermission},
	})
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(engineaccess.ManagerPreviewReadPermission)
	if err != nil {
		return err
	}
	routes := api.Group("/engine-access/read-checks")
	routes.POST("/manager-preview", observeManagerPreviewTimings, runtime.Authentication, credential, tenant, delegated, permission, handler.CheckManagerPreview)
	// The result operation has no Tool delegation route. Membership or preview
	// capability does not broaden a delegated credential to derived results.
	api.POST("/engine-access/read-checks/manager-profile-result", runtime.Authentication, runtime.UserAccessCredential, tenant, permission, handler.CheckManagerProfileResult)
	developPermission, err := middleware.NewIAMPermissionGuard("develop.task.read", "develop.data_read.execute")
	if err != nil {
		return err
	}
	api.POST("/engine-access/read-checks/develop-workflow-result", runtime.Authentication, runtime.UserAccessCredential, tenant, developPermission, handler.CheckDevelopWorkflowResult)
	return nil
}

func observeManagerPreviewTimings(c *gin.Context) {
	ctx, rule := engineaccess.ObserveHostedSourceRuleTiming(c.Request.Context())
	if rule == nil {
		c.Next()
		return
	}

	// Authentication and the final rule snapshot each
	// resolve once and synchronously. Extra or missing resolutions fail evidence.
	var resolutions [2]slog.Value
	count := 0
	ctx = iam.WithAuthContextTiming(ctx, func(timing slog.Value) {
		if count < len(resolutions) {
			resolutions[count] = timing
		}
		count++
	})
	c.Request = c.Request.WithContext(ctx)
	c.Next()
	var identity slog.Attr
	if count == len(resolutions) {
		identity = slog.Group("auth_context_ns",
			slog.Attr{Key: "route", Value: resolutions[0]},
			slog.Attr{Key: "rule_snapshot", Value: resolutions[1]})
	}
	logging.SetCompletionDiagnostics(c, rule.LogGroup(), identity)
}

// CheckManagerPreview godoc
// @Summary 检查 Manager 预览的当前精确源读取范围 | Check current precise source read coverage for Manager preview
// @Description 当前 Tenant 第一方或 OAuth API User，以及精确 manager audience 和唯一 data.preview Scope 的真实委托凭据；固定检查 manager.data_item.read，身份与完整源规则共用只读快照。不开放 Service、Resource Ticket 或其它 System API，无 Grant 或存在 Deny 整体拒绝，不接收自报身份、execution 或 Permission | Current Tenant first-party or OAuth API User, or a real delegated credential with exact manager audience and sole data.preview scope. Fixed manager.data_item.read and complete source rules share one read-only snapshot. Service, Resource Ticket and other System APIs remain unavailable; missing Grants or Deny reject the entire set; caller identity, execution and Permission are not accepted
// @Description 成功仅为当次源规则观察，不是完整 Allow、访问令牌或可缓存凭据。Manager 必须提供 Provider 证明的完整读取集合、完成本地 Security 保护并执行同一 PreparedQuery；本接口不访问源端或 Catalog | Success is a point-in-time source observation, not a complete Allow, access token or cacheable credential. Manager must supply the Provider-proven complete read set, apply local Security protection and execute the same PreparedQuery. This endpoint accesses neither the source nor Catalog
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body engineaccess.SourceReadCheckRequest true "1 至 200 个完整读取目标；受管 FTP/NFS 模型结果须提交同次 expected_source 且仅限普通 User，不支持 query 参数 | 1 to 200 complete read targets; managed FTP/NFS model results require same-decision expected_source and ordinary User; no query parameters"
// @Success 200 {object} engineaccess.SourceReadCheck "当次观察时刻，不授予访问权 | Observation time without granting access"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "身份、授权边界或依赖核验失败 | Identity, authorization boundary or dependency validation failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
// @Router /engine-access/read-checks/manager-preview [post]
func (h *EngineAccessSourceReadCheckHandler) CheckManagerPreview(c *gin.Context) {
	h.check(c, h.service.CheckManagerPreviewRead)
}

// CheckManagerProfileResult godoc
// @Summary 检查 Manager 剖析结果的当前源读取权限 | Check current source read coverage for Manager profile results
// @Description 仅当前 Tenant 普通第一方或 OAuth API User；固定核验 manager.data_item.read 及完整历史采样来源的现行规则，无 Grant 或命中 Deny 整体拒绝。不支持委托、Service、Resource Ticket 或自报身份、execution、Permission；不授予后台采样权限 | Current Tenant ordinary first-party or OAuth API User only. Fixed manager.data_item.read and current rules for all historical sampling sources; missing Grants or Deny reject the entire set. Delegation, Service, Resource Ticket and caller-supplied identity, execution or Permission are not supported; no background sampling authority is granted
// @Description Manager 必须提供实际结果冻结的完整 ReadSet，不能用当前视图依赖补造。只返回不可缓存的当次观察时刻，不访问源端，不创建 Grant 或执行授权 | Manager must supply the complete ReadSet frozen from the actual result, not reconstruct it from current view dependencies. Returns only a non-cacheable observation time; no source access, Grant or execution authorization
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body engineaccess.SourceReadCheckRequest true "1 至 200 个完整历史读取目标，不支持 query 参数 | 1 to 200 complete historical read targets; no query parameters"
// @Success 200 {object} engineaccess.SourceReadCheck "当次观察时刻 | Observation time"
// @Failure 400,401,403,409,500,503 {object} IAMErrorResponse "身份、授权边界或依赖核验失败 | Identity, authorization boundary or dependency validation failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
// @Router /engine-access/read-checks/manager-profile-result [post]
func (h *EngineAccessSourceReadCheckHandler) CheckManagerProfileResult(c *gin.Context) {
	h.check(c, h.service.CheckManagerProfileResultRead)
}

func (h *EngineAccessSourceReadCheckHandler) check(c *gin.Context, check func(context.Context, string, engineaccess.SourceReadCheckRequest) (*engineaccess.SourceReadCheck, error)) {
	var request engineaccess.SourceReadCheckRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil || len(request.Targets) == 0 || len(request.Targets) > 200 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	if request.ExpectedSource != nil {
		current, ok := middleware.IAMAuthContextFromGin(c)
		if !ok || current.Principal.Type != "user" || (current.Token.Type != middleware.IAMTokenTypeFirstPartyAccess && current.Token.Type != middleware.IAMTokenTypeOAuthAccess) {
			respondIAMError(c, commonapi.ErrForbidden)
			return
		}
	}
	result, err := check(c.Request.Context(), sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), request)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if result == nil || result.ObservedAt.IsZero() {
		respondIAMError(c, errors.New("incomplete source read observation"))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

// CheckDevelopWorkflowResult godoc
// @Summary 检查 Develop 本人工作流结果的当前来源读取权 | Check current source read access for own Develop workflow results
// @Description 仅当前普通 Tenant User，核验全部实际来源的当前 read Grant；不签发执行授权，不接受 Service、委托凭据或 expected_source。 | Current ordinary Tenant User only; recheck all actual source read grants without issuing execution authorization or accepting service/delegated credentials or expected_source.
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Param request body engineaccess.SourceReadCheckRequest true "实际来源 ReadSet | Actual source ReadSet"
// @Success 200 {object} engineaccess.SourceReadCheck
// @Failure 400 {object} IAMErrorResponse
// @Failure 401 {object} IAMErrorResponse
// @Failure 403 {object} IAMErrorResponse
// @Security BearerAuth
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["develop.task.read","develop.data_read.execute"]
// @Router /engine-access/read-checks/develop-workflow-result [post]
func (h *EngineAccessSourceReadCheckHandler) CheckDevelopWorkflowResult(c *gin.Context) {
	h.check(c, h.service.CheckDevelopWorkflowResultRead)
}
