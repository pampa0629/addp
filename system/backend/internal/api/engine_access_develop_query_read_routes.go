package api

import (
	"context"
	"errors"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type developQueryReadService interface {
	CheckDevelopQueryRead(context.Context, int64, client.DevelopQueryReadRequest) (*client.DevelopQueryReadObservation, error)
}

type DevelopQueryReadHandler struct{ service developQueryReadService }

func RegisterDevelopQueryReadRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *DevelopQueryReadHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.ServiceCredential == nil || handler == nil || handler.service == nil {
		return errors.New("Develop query read dependencies required")
	}
	tenant, err := middleware.NewIAMServiceContextGuard("tenant")
	if err != nil {
		return err
	}
	develop, err := middleware.NewIAMClientGuard("addp-develop")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard("system.develop_query_read_check.execute")
	if err != nil {
		return err
	}
	api.POST("/engine-access/read-observations/develop-query", runtime.Authentication, runtime.ServiceCredential, tenant, develop, permission, handler.Check)
	return nil
}

// Check godoc
// @Summary 核验 Develop 查询的当前来源读取授权 | Observe current Develop query source read authorization
// @Description 仅 addp-develop Tenant Service，支持 PostgreSQL、GaussDB、MySQL 和 TiDB 原生只读查询。Provider 提供同次实际执行的完整来源，System 独立读取 Meta 正式身份后最终重核原用户、功能权限及全部当前 Grant。异步查询绑定原 EA 和当前执行租约；样例试读仅接收同次同步原用户凭据，不持久化或记录凭据。不签发 Grant、不返回连接、不授予可复用 Allow | Only addp-develop Tenant Service for native, read-only PostgreSQL, GaussDB, MySQL and TiDB queries. Provider facts bind the actual execution. System resolves Meta facts before finally rechecking the original user, functions and all current Grants. Async queries require the original EA and current execution lease; sample reads use only the synchronous original user credential, never persisted or logged. No Grants, connections or reusable Allow are issued
// @Description PostgreSQL 交互 Notebook 使用原会话派生执行授权及匹配的会话 ID，复核会话、令牌族和当前全部来源 | PostgreSQL interactive Notebook reads bind the derived execution authorization and matching session ID and recheck the session, token family and all current sources
// @Tags 源数据读取检查 | Source Data Read Checks
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body client.DevelopQueryReadRequest true "完整来源和固定执行边界 | Complete sources and fixed execution boundary"
// @Success 200 {object} client.DevelopQueryReadObservation "当次观察和最晚边界，不得缓存为授权 | Point-in-time observation and upper deadline, not cacheable authority"
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.develop_query_read_check.execute"]
// @Router /engine-access/read-observations/develop-query [post]
func (h *DevelopQueryReadHandler) Check(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var request client.DevelopQueryReadRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &request) != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	_, tenant, _, err := iamTenantActor(c)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	result, err := h.service.CheckDevelopQueryRead(c.Request.Context(), int64(tenant), request)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if result == nil || result.ObservedAt.IsZero() || !result.ExpiresAt.After(result.ObservedAt) {
		respondIAMError(c, errors.New("incomplete Develop query read observation"))
		return
	}
	c.JSON(http.StatusOK, result)
}
