package repository

import (
	"errors"
	"strings"

	commonExecution "github.com/addp/common/execution"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// IsTaskSemanticConflict identifies the constraints that protect this task type.
// Other unique failures must not be converted into canonical task reuse.
func IsTaskSemanticConflict(err error, taskType string) bool {
	indexes := map[string]string{
		commonExecution.TaskTypeVectorTileSetGeneration:       "idx_vector_tile_set_tasks_semantic_unique",
		commonExecution.TaskTypeRasterCOGGeneration:           "idx_raster_cog_tasks_source_message_unique",
		commonExecution.TaskTypeGaussianSplatKSplatGeneration: "idx_gaussian_splat_ksplat_tasks_source_unique",
		commonExecution.TaskTypePointCloudCOPCGeneration:      "idx_point_cloud_copc_tasks_source_unique",
		commonExecution.TaskTypeVectorTileCacheGeneration:     "idx_vector_tile_cache_tasks_source_profile_unique",
		commonExecution.TaskTypeModel3DTilesGeneration:        "idx_model3d_tiles_tasks_source_format_unique",
		commonExecution.TaskTypeModel3DGLBGeneration:          "idx_model_3d_glb_tasks_source_unique",
	}
	index, supported := indexes[taskType]
	if !supported {
		return false
	}
	const semanticIndex = "uq_manager_task_definition_semantic"
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.SchemaName == "manager" && pgErr.TableName == "task_definitions" &&
			(pgErr.ConstraintName == semanticIndex || pgErr.ConstraintName == index)
	}
	// MySQL 8 reports the qualified key only in the final quoted field of error 1062.
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 &&
		strings.HasSuffix(mysqlErr.Message, " 'task_definitions."+semanticIndex+"'")
}
