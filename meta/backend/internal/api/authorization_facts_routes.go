package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/authorization"
	"github.com/gin-gonic/gin"
)

func registerDataItemAuthorizationFactsRoute(api *gin.RouterGroup, h *Handler) {
	api.GET("/runtime/data-items/authorization-facts/revision", commonAuth.MustNewServiceClientGuard("addp-system"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaAuthorizationFactsRead), h.GetAuthorizationBasisRevision)
	api.POST("/runtime/node-identities/query", commonAuth.MustNewServiceClientGuard("addp-system"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaNodeIdentityRead), h.QueryNodeIdentities)
	api.POST("/runtime/node-identities/resolve", commonAuth.MustNewServiceClientGuard("addp-system"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaNodeIdentityRead), h.ResolveNodeIdentity)
	api.POST("/runtime/data-item-identities/query", commonAuth.MustNewServiceClientGuard("addp-system"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaDataItemIdentityRead), h.QueryDataItemIdentities)
	api.POST("/runtime/data-items/authorization-facts/query", commonAuth.MustNewServiceClientGuard("addp-system", "addp-catalog", "addp-manager"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaAuthorizationFactsRead), h.QueryDataItemAuthorizationFacts)
}
