package api

import (
	commonAuth "github.com/addp/common/middleware/auth"
	"github.com/addp/meta/internal/authorization"
	"github.com/gin-gonic/gin"
)

func registerScanPlanFactsRoute(api *gin.RouterGroup, h *Handler) {
	api.GET("/runtime/scan-executions/:execution_id/authorization-facts", commonAuth.MustNewServiceClientGuard("addp-system"), commonAuth.MustNewPermissionGuard(authorization.PermissionMetaTaskProviderRead), h.GetScanPlanFacts)
}
