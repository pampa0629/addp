package api

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type managerPreviewReadCheckService interface {
	CheckManagerPreviewRead(context.Context, string, engineaccess.ManagerPreviewReadCheckRequest) (*engineaccess.SourceReadCheck, error)
}

type EngineAccessSourceReadCheckHandler struct {
	service managerPreviewReadCheckService
}

func RegisterEngineAccessSourceReadCheckRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *EngineAccessSourceReadCheckHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("source read check dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(engineaccess.ManagerPreviewReadPermission)
	if err != nil {
		return err
	}
	routes := api.Group("/engine-access/read-checks")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.POST("/manager-preview", permission, handler.CheckManagerPreview)
	return nil
}

// CheckManagerPreview godoc
// @Summary 检查 Manager 预览的当前精确源读取范围 | Check current precise source read coverage for Manager preview
// @Description 仅当前 Tenant 第一方或 OAuth User Bearer；固定检查 manager.data_item.read 和 addp.api Client 边界，身份与完整源规则共用只读快照。无 Grant 或存在 Deny 整体拒绝，不接收自报身份、execution 或 Permission | Only a current Tenant first-party or OAuth User Bearer. Fixed manager.data_item.read and addp.api client constraints are checked with complete source rules in one read-only snapshot. Missing Grants or Deny reject the entire set; caller identity, execution and Permission are not accepted
// @Description 成功仅为当次源规则观察，不是完整 Allow、访问令牌或可缓存凭据。Manager 必须提供 Provider 证明的完整读取集合、完成本地 Security 保护并执行同一 PreparedQuery；本接口不访问源端或 Catalog | Success is a point-in-time source observation, not a complete Allow, access token or cacheable credential. Manager must supply the Provider-proven complete read set, apply local Security protection and execute the same PreparedQuery. This endpoint accesses neither the source nor Catalog
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body engineaccess.ManagerPreviewReadCheckRequest true "1 至 200 个完整读取目标，不支持 query 参数 | 1 to 200 complete read targets; no query parameters"
// @Success 200 {object} engineaccess.SourceReadCheck "当次观察时刻，不授予访问权 | Observation time without granting access"
// @Failure 400,401,403,500 {object} IAMErrorResponse "请求失败 | Request failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["manager.data_item.read"]
// @Router /engine-access/read-checks/manager-preview [post]
func (h *EngineAccessSourceReadCheckHandler) CheckManagerPreview(c *gin.Context) {
	var request engineaccess.ManagerPreviewReadCheckRequest
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil || len(request.Targets) == 0 || len(request.Targets) > 200 {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.CheckManagerPreviewRead(c.Request.Context(), sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), request)
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
