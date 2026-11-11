package repository

import (
	"errors"

	commonExecution "github.com/addp/common/execution"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsTaskSemanticConflict identifies the constraints that protect this task type.
// Other unique failures must not be converted into canonical task reuse.
func IsTaskSemanticConflict(err error, taskType string) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23505" || postgresError.SchemaName != "manager" || postgresError.TableName != "task_definitions" {
		return false
	}
	indexes := map[string]string{
		commonExecution.TaskTypeVectorTileSetGeneration:       "idx_vector_tile_set_tasks_semantic_unique",
		commonExecution.TaskTypeRasterCOGGeneration:           "idx_raster_cog_tasks_source_unique",
		commonExecution.TaskTypeGaussianSplatKSplatGeneration: "idx_gaussian_splat_ksplat_tasks_source_unique",
		commonExecution.TaskTypePointCloudCOPCGeneration:      "idx_point_cloud_copc_tasks_source_unique",
		commonExecution.TaskTypeVectorTileCacheGeneration:     "idx_vector_tile_cache_tasks_source_profile_unique",
		commonExecution.TaskTypeModel3DTilesGeneration:        "idx_model3d_tiles_tasks_source_format_unique",
		commonExecution.TaskTypeModel3DGLBGeneration:          "idx_model_3d_glb_tasks_source_unique",
	}
	index, supported := indexes[taskType]
	return supported && (postgresError.ConstraintName == "uq_manager_task_definition_semantic" || postgresError.ConstraintName == index)
}
