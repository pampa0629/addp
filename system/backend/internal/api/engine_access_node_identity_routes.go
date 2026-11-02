package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/jsonmap"
	commoni18n "github.com/addp/common/middleware/i18n"
	sysi18n "github.com/addp/system/i18n"
	"github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/engineaccess"
	"github.com/addp/system/internal/middleware"
	"github.com/gin-gonic/gin"
)

type authorizationNodeService interface {
	ListAuthorizationNodes(context.Context, engineaccess.Actor, int64, authorizationfacts.NodeIdentityPageQuery) (*authorizationfacts.NodeIdentityPage, error)
}
type EngineAccessNodeIdentityHandler struct{ service authorizationNodeService }

func RegisterEngineAccessNodeIdentityRoutes(api *gin.RouterGroup, runtime *IAMRuntime, h *EngineAccessNodeIdentityHandler) error {
	if api == nil || runtime == nil || runtime.Authentication == nil || runtime.UserAccessCredential == nil || h == nil || h.service == nil {
		return errors.New("node identity route dependencies required")
	}
	tenant, err := middleware.NewIAMContextGuard("tenant")
	if err != nil {
		return err
	}
	permission, err := middleware.NewIAMPermissionGuard(authorization.PermissionSystemEngineAccessGrantRead)
	if err != nil {
		return err
	}
	routes := api.Group("/engines/:id/authorization-nodes")
	routes.Use(runtime.Authentication, runtime.UserAccessCredential, tenant)
	routes.POST("/query", permission, h.Query)
	return nil
}

// Query godoc
// @Summary 浏览授权候选节点 | Browse authorization candidate nodes
// @Description 当前Tenant User须具有源授权读取功能和目标引擎管理范围。仅查询Meta已识别PG server/schema的直接子节点身份，不枚举数据项、源目录、内容、属性或统计；查询前后重核当前人员资格。结果不是Grant，签发仍独立核对真实节点 | Current Tenant User needs source-grant read permission and target engine management scope, rechecked before and after Meta IO. Lists only immediate Meta-identified PG server/schema node identities. No DataItem enumeration, native source browsing, content, attributes or statistics. Not a Grant; issuance independently verifies the exact node
// @Tags 源数据授权 | Source Data Grants
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "引擎ID | Engine ID"
// @Param request body authorizationfacts.NodeIdentityPageQuery true "节点路径及分页 | Node path and pagination"
// @Success 200 {object} authorizationfacts.NodeIdentityPage
// @Failure 400,401,403,404,413,500,503 {object} IAMErrorResponse "查询失败 | Query failed"
// @x-addp-auth-mode "permission"
// @x-addp-required-permissions ["system.engine_access_grant.read"]
// @Router /engines/{id}/authorization-nodes/query [post]
func (h *EngineAccessNodeIdentityHandler) Query(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actor, engineID, ok := engineAccessActor(c)
	if !ok {
		return
	}
	body, readErr := io.ReadAll(io.LimitReader(c.Request.Body, authorizationfacts.MaxNodeIdentityBytes+1))
	if len(body) > authorizationfacts.MaxNodeIdentityBytes {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error_code": "invalid_request", "error": commoni18n.T(c, commoni18n.MsgInvalidParams)})
		return
	}
	_, shapeErr := jsonmap.DecodeObjectStrict(body, authorizationfacts.MaxNodeIdentityBytes, 8)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var query authorizationfacts.NodeIdentityPageQuery
	bindErr := commonapi.BindOptionalJSONStrict(c, &query)
	if c.Request.URL.RawQuery != "" || readErr != nil || shapeErr != nil || bindErr != nil || query.Validate() != nil || int64(query.Target.EngineID) != engineID {
		respondIAMError(c, commonapi.ErrBadRequest)
		return
	}
	result, err := h.service.ListAuthorizationNodes(c.Request.Context(), actor, engineID, query)
	if errors.Is(err, engineaccess.ErrAuthorizationFactsUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error_code": "authorization_facts_unavailable", "error": commoni18n.T(c, sysi18n.MsgAuthorizationFactsUnavailable)})
		return
	}
	if err != nil {
		respondIAMError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
