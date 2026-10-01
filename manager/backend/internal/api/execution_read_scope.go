package api

import (
	"github.com/addp/common/execution"
	ownerauthorization "github.com/addp/manager/internal/authorization"
	"github.com/gin-gonic/gin"
)

// GetExecutionReadScope returns the current User's Owner-defined execution scope.
// @Summary 获取任务执行读取范围 | Get execution read scope
// @Description 仅第一方租户用户；范围由 Owner 的现有读取 Permission 裁决，任务历史与本人一次性执行使用不同条件。| First-party tenant users only; Owner permissions decide task history and own ad-hoc visibility.
// @Tags Execution diagnostics
// @Produce json
// @Success 200 {object} execution.ReadScope
// @Failure 403 {object} map[string]string
// @x-addp-auth-mode "authenticated"
// @Router /execution-read-scope [get]
// @Security BearerAuth
func GetExecutionReadScope(c *gin.Context) {
	execution.OwnerReadScopeHandler("manager", map[string]string{
		"vector_tile_cache_generation":        ownerauthorization.PermissionManagerDerivedArtifactRead,
		"vector_tile_set_generation":          ownerauthorization.PermissionManagerDerivedArtifactRead,
		"vector_materialized_view_generation": ownerauthorization.PermissionManagerDerivedArtifactRead,
		"raster_cog_generation":               ownerauthorization.PermissionManagerDerivedArtifactRead,
		"raster_mosaic_generation":            ownerauthorization.PermissionManagerDerivedArtifactRead,
		"model_3d_glb_generation":             ownerauthorization.PermissionManagerDerivedArtifactRead,
		"model3d_tiles_generation":            ownerauthorization.PermissionManagerDerivedArtifactRead,
		"gaussian_splat_ksplat_generation":    ownerauthorization.PermissionManagerDerivedArtifactRead,
		"point_cloud_copc_generation":         ownerauthorization.PermissionManagerDerivedArtifactRead,
		"pptx_pdf_generation":                 ownerauthorization.PermissionManagerDerivedArtifactRead,
		"embedding":                           ownerauthorization.PermissionManagerDerivedArtifactRead,
		"data_profiling":                      ownerauthorization.PermissionManagerDataItemRead,
		"cleanup_executor":                    "system.cleanup.read",
	})(c)
}
