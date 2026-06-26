package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	commonapi "github.com/addp/common/api"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func TestQuickViewOptimizationTaskCreateNormalizesConfig(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewQuickViewOptimizationRepository(db)
	svc := NewQuickViewOptimizationTaskService(repo, nil)

	task := newQuickViewOptimizationTaskDefinition()
	target, _ := asJSONMap(task.Config["target"])
	delete(target, "locator")
	target["legacy_ref"] = "ignored"
	task.Config["target"] = target

	if err := svc.Create(context.Background(), task); err != nil {
		t.Fatalf("create quick view optimization task: %v", err)
	}
	normalizedTarget, _ := asJSONMap(task.Config["target"])
	if normalizedTarget["item_fingerprint"] != spatialItemFingerprint(11, "public", "roads") {
		t.Fatalf("item_fingerprint = %v", normalizedTarget["item_fingerprint"])
	}
	if normalizedTarget["locator"] != tableLocator(11, "public", "roads") {
		t.Fatalf("locator = %v, want standard table locator", normalizedTarget["locator"])
	}
	if _, ok := normalizedTarget["legacy_ref"]; ok {
		t.Fatalf("legacy_ref still present: %#v", normalizedTarget)
	}
	optimization, _ := asJSONMap(task.Config["optimization"])
	if optimization["target_kind"] != models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView {
		t.Fatalf("target_kind = %v", optimization["target_kind"])
	}
	storage, _ := asJSONMap(task.Config["storage"])
	if storage["target_schema"] != "public" {
		t.Fatalf("target_schema = %v, want public", storage["target_schema"])
	}
}

func TestQuickViewOptimizationTaskRejectsUnsupportedPhaseOneOptions(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewQuickViewOptimizationRepository(db)
	svc := NewQuickViewOptimizationTaskService(repo, nil)

	scheduled := newQuickViewOptimizationTaskDefinition()
	scheduled.Schedule = "* * * * *"
	if err := svc.Create(context.Background(), scheduled); err == nil || !strings.Contains(err.Error(), "does not support schedule") {
		t.Fatalf("scheduled create error = %v, want schedule rejection", err)
	}

	crossSchema := newQuickViewOptimizationTaskDefinition()
	crossSchema.Config["storage"] = commonModels.JSONMap{"target_schema": "addp_derived"}
	if err := svc.Create(context.Background(), crossSchema); err == nil || !strings.Contains(err.Error(), "target_schema to equal source schema") {
		t.Fatalf("cross schema create error = %v, want source schema rejection", err)
	}

	wrongSRID := newQuickViewOptimizationTaskDefinition()
	geometry, _ := asJSONMap(wrongSRID.Config["geometry"])
	geometry["target_srid"] = float64(4326)
	wrongSRID.Config["geometry"] = geometry
	if err := svc.Create(context.Background(), wrongSRID); err == nil || !strings.Contains(err.Error(), "target_srid=3857") {
		t.Fatalf("wrong srid create error = %v, want 3857 rejection", err)
	}

	source3857 := newQuickViewOptimizationTaskDefinition()
	geometry, _ = asJSONMap(source3857.Config["geometry"])
	geometry["source_srid"] = float64(3857)
	source3857.Config["geometry"] = geometry
	if err := svc.Create(context.Background(), source3857); err == nil || !strings.Contains(err.Error(), "already optimized by source 3857") {
		t.Fatalf("source 3857 create error = %v, want already optimized rejection", err)
	}

	sourceGeomAttribute := newQuickViewOptimizationTaskDefinition()
	optimization, _ := asJSONMap(sourceGeomAttribute.Config["optimization"])
	optimization["attributes"] = []interface{}{"name", "shape"}
	sourceGeomAttribute.Config["optimization"] = optimization
	if err := svc.Create(context.Background(), sourceGeomAttribute); err == nil || !strings.Contains(err.Error(), "must not include geometry column") {
		t.Fatalf("source geometry attribute create error = %v, want geometry attribute rejection", err)
	}

	targetGeomAttribute := newQuickViewOptimizationTaskDefinition()
	optimization, _ = asJSONMap(targetGeomAttribute.Config["optimization"])
	optimization["attributes"] = []interface{}{"name", models.QuickViewOptimizationTargetGeometryColumn}
	targetGeomAttribute.Config["optimization"] = optimization
	if err := svc.Create(context.Background(), targetGeomAttribute); err == nil || !strings.Contains(err.Error(), "must not include geometry column") {
		t.Fatalf("target geometry attribute create error = %v, want geometry attribute rejection", err)
	}

	reservedAttribute := newQuickViewOptimizationTaskDefinition()
	optimization, _ = asJSONMap(reservedAttribute.Config["optimization"])
	optimization["attributes"] = []interface{}{"name", "source_row_id"}
	reservedAttribute.Config["optimization"] = optimization
	if err := svc.Create(context.Background(), reservedAttribute); err == nil || !strings.Contains(err.Error(), "reserved column") {
		t.Fatalf("reserved attribute create error = %v, want reserved column rejection", err)
	}

	duplicateAttribute := newQuickViewOptimizationTaskDefinition()
	optimization, _ = asJSONMap(duplicateAttribute.Config["optimization"])
	optimization["attributes"] = []interface{}{"name", "NAME"}
	duplicateAttribute.Config["optimization"] = optimization
	if err := svc.Create(context.Background(), duplicateAttribute); err == nil || !strings.Contains(err.Error(), "duplicate column") {
		t.Fatalf("duplicate attribute create error = %v, want duplicate column rejection", err)
	}
}

func TestBuildQuickViewOptimizationPlanUsesStagingMaterializedViewAndIndex(t *testing.T) {
	task := newQuickViewOptimizationTaskDefinition()
	execCfg, err := normalizeQuickViewOptimizationTaskConfig(task.Config)
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	plan := buildQuickViewOptimizationPlan(execCfg, "addp_qvo_abcd", "1234567890")
	createSQLWithPK := buildQuickViewOptimizationCreateSQL(execCfg, plan.StagingTable, []string{"id"})

	for _, fragment := range []string{
		`CREATE MATERIALIZED VIEW "public"."addp_qvo_abcd_staging_1234567890" AS`,
		`("id")::text AS source_row_id`,
		`"name"`,
		`ST_Transform("shape", 3857) AS "geom_3857"`,
		`FROM "public"."roads"`,
		`WHERE "shape" IS NOT NULL`,
	} {
		if !strings.Contains(createSQLWithPK, fragment) {
			t.Fatalf("create SQL missing %q:\n%s", fragment, createSQLWithPK)
		}
	}
	if !strings.Contains(plan.CreateIndexSQL, `CREATE INDEX "idx_addp_qvo_abcd_staging_1234567890_geom_3857_gist"`) {
		t.Fatalf("create index SQL = %s", plan.CreateIndexSQL)
	}
	if strings.Contains(plan.CreateIndexSQL, "CONCURRENTLY") {
		t.Fatalf("create index SQL should not use concurrently during staging build: %s", plan.CreateIndexSQL)
	}
	if plan.AnalyzeSQL != `ANALYZE "public"."addp_qvo_abcd_staging_1234567890"` {
		t.Fatalf("analyze SQL = %s", plan.AnalyzeSQL)
	}
}

func TestQuickViewOptimizationSourceRowIDExpression(t *testing.T) {
	tests := []struct {
		name    string
		columns []string
		want    string
	}{
		{
			name:    "none",
			columns: nil,
			want:    `(row_number() OVER ())::text AS source_row_id`,
		},
		{
			name:    "single",
			columns: []string{"id"},
			want:    `("id")::text AS source_row_id`,
		},
		{
			name:    "composite",
			columns: []string{"tenant_id", "feature_id"},
			want:    `jsonb_build_object('tenant_id', "tenant_id", 'feature_id', "feature_id")::text AS source_row_id`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quickViewOptimizationSourceRowIDExpression(tt.columns); got != tt.want {
				t.Fatalf("source row id expression = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestQuickViewOptimizationDeleteResultReturnsNotFound(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewQuickViewOptimizationRepository(db)
	svc := NewQuickViewOptimizationTaskService(repo, nil)

	err := svc.DeleteResult(context.Background(), 999, 1)
	if !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("delete missing result error = %v, want ErrNotFound", err)
	}
}

func TestQuickViewOptimizationDeleteResultRecordsCleanupFailure(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewQuickViewOptimizationRepository(db)
	svc := NewQuickViewOptimizationTaskService(repo, nil)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite sql db: %v", err)
	}
	svc.SetDBProvider(quickViewOptimizationTestDBProvider{db: sqlDB})

	result := newQuickViewOptimizationResult()
	if err := repo.CreateResult(context.Background(), result); err != nil {
		t.Fatalf("create quick view optimization result: %v", err)
	}

	err = svc.DeleteResult(context.Background(), result.ID, result.TenantID)
	if err == nil || !strings.Contains(err.Error(), "drop quick view optimization target") {
		t.Fatalf("delete result error = %v, want drop failure", err)
	}

	stored, err := repo.GetResult(context.Background(), result.ID, result.TenantID)
	if err != nil {
		t.Fatalf("get result after cleanup failure: %v", err)
	}
	if stored == nil {
		t.Fatalf("result was deleted after cleanup failure")
	}
	if stored.Status != models.QuickViewOptimizationStatusFailed {
		t.Fatalf("status = %s, want failed", stored.Status)
	}
	if !strings.Contains(stored.ErrorMessage, "cleanup quick view optimization target failed") {
		t.Fatalf("error_message = %q, want cleanup failure", stored.ErrorMessage)
	}
	if stored.Metadata["cleanup_error"] == nil {
		t.Fatalf("metadata cleanup_error missing: %#v", stored.Metadata)
	}
}

func TestQuickViewOptimizationDeleteResultsForSourceTableDeletesPreference(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewQuickViewOptimizationRepository(db)
	quickViewRepo := repository.NewQuickViewRepository(db)
	svc := NewQuickViewOptimizationTaskService(repo, nil)
	svc.SetQuickViewRepository(quickViewRepo)

	result := newQuickViewOptimizationResult()
	result.Status = models.QuickViewOptimizationStatusDeleted
	if err := repo.CreateResult(context.Background(), result); err != nil {
		t.Fatalf("create quick view optimization result: %v", err)
	}
	if err := quickViewRepo.UpdatePreferredMode(result.TenantID, result.ItemFingerprint, result.Locator, models.QuickViewPreferredModeMapQuickView); err != nil {
		t.Fatalf("create quick view preference: %v", err)
	}

	if err := svc.DeleteResultsForSourceTable(context.Background(), result.TenantID, result.SourceEngineID, result.SourceSchema, result.SourceTable); err != nil {
		t.Fatalf("delete results for source table: %v", err)
	}

	if _, err := quickViewRepo.GetByIdentity(result.TenantID, result.ItemFingerprint, result.Locator); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("quick view preference error = %v, want ErrNotFound", err)
	}
}

func TestQuickViewOptimizationListResultsBySourceTableFiltersPrecisely(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewQuickViewOptimizationRepository(db)

	target := newQuickViewOptimizationResult()
	target.Status = models.QuickViewOptimizationStatusDeleted
	if err := repo.CreateResult(context.Background(), target); err != nil {
		t.Fatalf("create target result: %v", err)
	}
	other := newQuickViewOptimizationResult()
	other.SourceTable = "buildings"
	other.TargetTable = "addp_qvo_other"
	other.ItemFingerprint = spatialItemFingerprint(other.SourceEngineID, other.SourceSchema, other.SourceTable)
	other.Locator = tableLocator(other.SourceEngineID, other.SourceSchema, other.SourceTable)
	if err := repo.CreateResult(context.Background(), other); err != nil {
		t.Fatalf("create other result: %v", err)
	}

	results, err := repo.ListResultsBySourceTable(context.Background(), target.TenantID, target.SourceEngineID, target.SourceSchema, target.SourceTable)
	if err != nil {
		t.Fatalf("list by source table: %v", err)
	}
	if len(results) != 1 || results[0].SourceTable != target.SourceTable {
		t.Fatalf("results = %#v, want only %s", results, target.SourceTable)
	}
}

type quickViewOptimizationTestDBProvider struct {
	db *sql.DB
}

func (p quickViewOptimizationTestDBProvider) GetPostGISDB(context.Context, *uint, uint) (*sql.DB, error) {
	return p.db, nil
}

func newQuickViewOptimizationTaskDefinition() *models.QuickViewOptimizationTask {
	return &models.QuickViewOptimizationTask{
		TenantID: 1,
		Name:     "optimize roads",
		Enabled:  true,
		Config: commonModels.JSONMap{
			"target": commonModels.JSONMap{
				"source_engine_id": float64(11),
				"schema":           "public",
				"table":            "roads",
				"item_id":          float64(42),
			},
			"geometry": commonModels.JSONMap{
				"geometry_column": "shape",
				"source_srid":     float64(4326),
				"target_srid":     float64(3857),
			},
			"optimization": commonModels.JSONMap{
				"target_kind":         models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView,
				"include_source_key":  true,
				"attributes":          []interface{}{"name"},
				"analyze_after_build": true,
			},
			"storage": commonModels.JSONMap{
				"target_schema": "public",
			},
		},
	}
}

func newQuickViewOptimizationResult() *models.QuickViewOptimization {
	executionID := "execution-1"
	taskID := uint(1)
	itemID := uint(42)
	return &models.QuickViewOptimization{
		TenantID:                  1,
		ItemFingerprint:           spatialItemFingerprint(11, "public", "roads"),
		ItemID:                    &itemID,
		Locator:                   tableLocator(11, "public", "roads"),
		TaskID:                    &taskID,
		LastExecutionID:           &executionID,
		SourceEngineID:            11,
		SourceSchema:              "public",
		SourceTable:               "roads",
		SourceGeometryColumn:      "shape",
		SourceSRID:                4326,
		TargetSRID:                3857,
		TargetKind:                models.QuickViewOptimizationTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_qvo_test",
		TargetGeometryColumn:      models.QuickViewOptimizationTargetGeometryColumn,
		Status:                    models.QuickViewOptimizationStatusReady,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{"index_name": "idx_addp_qvo_test_geom_3857_gist"},
	}
}
