package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

type metadataRefreshScopeService interface {
	GetMetadataRefreshScope(context.Context, string, authorization.MetadataRefreshQuery) (*authorization.MetadataRefreshScope, error)
}

type MetadataRefreshScopeHandler struct{ service metadataRefreshScopeService }

func RegisterMetadataRefreshScopeRoutes(api *gin.RouterGroup, runtime *IAMRuntime, handler *MetadataRefreshScopeHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || handler == nil || handler.service == nil {
		return errors.New("metadata refresh dependencies required")
	}
	api.POST("/engine-access/metadata-refresh-scopes/query", runtime.Authentication, runtime.UserAccessCredential,
		sharedauth.MustNewContextGuard("tenant"), sharedauth.MustNewPermissionGuard("meta.scan_task.execute"), handler.Query)
	return nil
}

// Query godoc
// @Summary 查询当前授权范围内的元数据刷新范围 | Query metadata refresh scope within current grants
// @Description 仅 PG 与 NFS 的普通 User；node／item 核验扫描功能及当前个人与组织元数据／读取 Grant，并独立验证 Meta 身份；node 不接受引擎根。显式 engine 集中扫描必须具有引擎管理范围，可用于首次基础识别。精确项不扩大到父目录。结果不授予读取或可缓存扫描许可 | Ordinary User for PG and NFS only. Node/item checks scan function, current personal/organization metadata/read Grants and independent Meta identity; node rejects engine roots. Explicit centralized engine scans require engine management authority and may bootstrap basic identification. Exact items never expand to parents. The result grants neither read access nor a cacheable scan permission
// @Tags 元数据集中采集 | Centralized Metadata Collection
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body authorization.MetadataRefreshQuery true "目标种类与类型化路径 | Target kind and typed path"
// @Success 200 {object} authorization.MetadataRefreshScope "当前最大刷新范围 | Current maximum refresh scope"
// @Failure 400,401,403,404,409,500,503 {object} IAMErrorResponse "核验失败 | Validation failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["meta.scan_task.execute"]
// @Router /engine-access/metadata-refresh-scopes/query [post]
func (h *MetadataRefreshScopeHandler) Query(c *gin.Context) {
	var query authorization.MetadataRefreshQuery
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	if c.Request.URL.RawQuery != "" || commonapi.BindOptionalJSONStrict(c, &query) != nil || query.Validate() != nil {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.GetMetadataRefreshScope(c.Request.Context(), sharedauth.CanonicalBearerToken(c.GetHeader("Authorization")), query)
	if err != nil {
		respondIAMError(c, err)
		return
	}
	if result == nil || result.Validate() != nil || !reflect.DeepEqual(result.Query, query) {
		respondIAMError(c, commonapi.ErrInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}
