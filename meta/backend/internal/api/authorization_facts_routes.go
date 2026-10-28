package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/authorization"
	"github.com/gin-gonic/gin"
)

func registerDataItemAuthorizationFactsRoute(api *gin.RouterGroup, h *Handler) {
	api.POST("/runtime/data-item-identities/query", commonAuth.MustNewServiceClientGuard("addp-system"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaDataItemIdentityRead), h.QueryDataItemIdentities)
	api.GET("/runtime/data-items/:fingerprint/authorization-facts", commonAuth.MustNewServiceClientGuard("addp-system", "addp-catalog"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaAuthorizationFactsRead), h.GetDataItemAuthorizationFacts)
}
