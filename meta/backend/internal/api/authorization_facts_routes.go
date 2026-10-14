package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/authorization"
	"github.com/gin-gonic/gin"
)

func registerDataItemAuthorizationFactsRoute(api *gin.RouterGroup, h *Handler) {
	api.GET("/runtime/data-items/:fingerprint/authorization-facts", commonAuth.MustNewServiceClientGuard("addp-system", "addp-catalog"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaAuthorizationFactsRead), h.GetDataItemAuthorizationFacts)
}
