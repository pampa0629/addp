package repository

import (
	"errors"
	"fmt"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTaskSemanticConflict(t *testing.T) {
	for taskType, index := range map[string]string{
		commonExecution.TaskTypeVectorTileCacheGeneration:     "idx_vector_tile_cache_tasks_source_profile_unique",
		commonExecution.TaskTypeRasterCOGGeneration:           "idx_raster_cog_tasks_source_unique",
		commonExecution.TaskTypeVectorTileSetGeneration:       "idx_vector_tile_set_tasks_semantic_unique",
		commonExecution.TaskTypeModel3DGLBGeneration:          "idx_model_3d_glb_tasks_source_unique",
		commonExecution.TaskTypeModel3DTilesGeneration:        "idx_model3d_tiles_tasks_source_format_unique",
		commonExecution.TaskTypeGaussianSplatKSplatGeneration: "idx_gaussian_splat_ksplat_tasks_source_unique",
		commonExecution.TaskTypePointCloudCOPCGeneration:      "idx_point_cloud_copc_tasks_source_unique",
	} {
		t.Run(taskType, func(t *testing.T) {
			for _, constraint := range []string{index, "uq_manager_task_definition_semantic"} {
				err := &pgconn.PgError{Code: "23505", SchemaName: "manager", TableName: "task_definitions", ConstraintName: constraint}
				if !IsTaskSemanticConflict(fmt.Errorf("create: %w", err), taskType) {
					t.Fatalf("semantic constraint %s was not recognized", constraint)
				}
			}
			for _, err := range []error{
				nil,
				errors.New("23505 uq_manager_task_definition_semantic"),
				&pgconn.PgError{Code: "23503", SchemaName: "manager", TableName: "task_definitions", ConstraintName: index},
				&pgconn.PgError{Code: "23505", SchemaName: "manager", TableName: "task_definitions", ConstraintName: "task_definitions_pkey"},
				&pgconn.PgError{Code: "23505", SchemaName: "other", TableName: "task_definitions", ConstraintName: index},
				&pgconn.PgError{Code: "23505", SchemaName: "manager", TableName: "other", ConstraintName: index},
				&pgconn.PgError{Code: "23505", SchemaName: "manager", TableName: "task_definitions", ConstraintName: "idx_other_tasks_semantic_unique"},
			} {
				if IsTaskSemanticConflict(err, taskType) {
					t.Fatalf("unrelated error recognized as semantic conflict: %v", err)
				}
			}
		})
	}
	if IsTaskSemanticConflict(&pgconn.PgError{Code: "23505", SchemaName: "manager", TableName: "task_definitions", ConstraintName: "uq_manager_task_definition_semantic"}, "unknown") {
		t.Fatal("unknown task type recognized")
	}
}
