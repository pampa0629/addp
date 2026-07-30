package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

func TestCleanupExpectedForModuleProtocolHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []string
		module   string
		want     bool
	}{
		{name: "empty means all modules", expected: nil, module: events.ModuleManager, want: true},
		{name: "includes module", expected: []string{events.ModuleMeta, events.ModuleManager}, module: events.ModuleManager, want: true},
		{name: "does not include module", expected: []string{events.ModuleMeta}, module: events.ModuleManager, want: false},
		{name: "trims stream values", expected: []string{" manager "}, module: events.ModuleManager, want: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := events.CleanupExpectedForModule(tt.expected, tt.module); got != tt.want {
				t.Fatalf("CleanupExpectedForModule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerCleanupSummaries(t *testing.T) {
	t.Parallel()

	stats := &ManagerCleanupStats{
		PreviewStates:            2,
		TileCaches:               3,
		Embeddings:               5,
		VectorMaterializedViews:  7,
		DeletedPhysicalArtifacts: 4,
		MarkedMissingSource:      6,
		SkippedExternalTargets:   1,
		DisabledTaskDefinitions:  8,
		Errors:                   []string{"one", "two"},
	}

	scanSummary := managerScanSummary(stats)
	if scanSummary.ScannedItems != 25 || scanSummary.DisabledTaskDefinitions != 8 || scanSummary.SkippedItems != 1 || scanSummary.ErrorCount != 2 {
		t.Fatalf("scan summary = %#v", scanSummary)
	}
	if scanSummary.RiskLevel != "low" {
		t.Fatalf("scan risk = %q, want low", scanSummary.RiskLevel)
	}

	executeSummary := managerExecuteSummary(stats)
	if executeSummary.AffectedRecords != 25 {
		t.Fatalf("affected_records = %d, want 25", executeSummary.AffectedRecords)
	}
	if executeSummary.DeletedPhysicalArtifacts != 4 || executeSummary.MarkedMissingSource != 6 || executeSummary.DisabledTaskDefinitions != 8 {
		t.Fatalf("execute summary = %#v", executeSummary)
	}
	if executeSummary.SkippedItems != 1 || executeSummary.ErrorCount != 2 {
		t.Fatalf("execute summary skip/error = %#v", executeSummary)
	}
}

func TestManagerCleanupContextMatching(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	locator := "addp://engine/12/path/public/roads?type=table&item_id=99"

	tests := []struct {
		name            string
		locator         string
		itemFingerprint string
		itemID          uint
		context         map[string]interface{}
		want            bool
	}{
		{
			name:            "empty context matches",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         nil,
			want:            true,
		},
		{
			name:            "engine item and fingerprint match",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"engine_id": "12", "item_id": float64(99), "item_fingerprint": "fp-1"},
			want:            true,
		},
		{
			name:            "engine mismatch",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"engine_id": uint(13)},
			want:            false,
		},
		{
			name:            "item id mismatch",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"item_id": 100},
			want:            false,
		},
		{
			name:            "fingerprint mismatch",
			locator:         locator,
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"item_fingerprint": "fp-2"},
			want:            false,
		},
		{
			name:            "invalid locator fails engine-scoped context",
			locator:         "invalid",
			itemFingerprint: "fp-1",
			itemID:          99,
			context:         map[string]interface{}{"engine_id": 12},
			want:            false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := service.matchesCleanupContext(tt.locator, tt.itemFingerprint, tt.itemID, tt.context)
			if got != tt.want {
				t.Fatalf("matchesCleanupContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagerCleanupRiskLevelForCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		count int
		want  string
	}{
		{count: 0, want: "low"},
		{count: 100, want: "low"},
		{count: 101, want: "medium"},
		{count: 1000, want: "medium"},
		{count: 1001, want: "high"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := riskLevelForCount(tt.count); got != tt.want {
				t.Fatalf("riskLevelForCount(%d) = %q, want %q", tt.count, got, tt.want)
			}
		})
	}
}

func TestCleanupTaskTargetFromConfig(t *testing.T) {
	t.Parallel()

	embeddingTarget := cleanupTaskTargetFromConfig(commonModels.JSONMap{
		"target": commonModels.JSONMap{
			"engine_id":        float64(12),
			"item_id":          float64(99),
			"item_fingerprint": "fp-1",
			"locator":          "addp://engine/12/path/public/roads?type=table&item_id=99",
		},
	})
	if embeddingTarget.EngineID != 12 || embeddingTarget.ItemID != 99 || embeddingTarget.ItemFingerprint != "fp-1" {
		t.Fatalf("embedding target = %#v", embeddingTarget)
	}

	artifactTarget := cleanupTaskTargetFromConfig(commonModels.JSONMap{
		"target": commonModels.JSONMap{
			"source_engine_id": uint(13),
			"item_fingerprint": "fp-2",
		},
	})
	if artifactTarget.EngineID != 13 || artifactTarget.ItemFingerprint != "fp-2" {
		t.Fatalf("artifact target = %#v", artifactTarget)
	}
}

func TestCleanupTaskDefinitionReason(t *testing.T) {
	t.Parallel()

	if got := cleanupTaskDefinitionReason(map[string]interface{}{"engine_id": 12}); got != "missing_engine" {
		t.Fatalf("reason = %q, want missing_engine", got)
	}
	if got := cleanupTaskDefinitionReason(map[string]interface{}{"item_id": 99}); got != "missing_source" {
		t.Fatalf("reason = %q, want missing_source", got)
	}
}

func TestCleanupTaskTargetSourceExistsTreatsDeletedEngineAsMissing(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	target := cleanupTaskTarget{EngineID: 12, ItemID: 99, Locator: "addp://engine/12/path/public/roads?type=table&item_id=99"}
	if got := service.taskTargetSourceExists(nil, 1, target, map[string]interface{}{"engine_id": 12}); got {
		t.Fatal("taskTargetSourceExists() = true, want false for engine deleted context")
	}
	if got := service.taskTargetSourceExists(nil, 1, cleanupTaskTarget{}, nil); !got {
		t.Fatal("empty task target should be treated as existing to avoid broad cleanup")
	}
}

func TestFilterMissingVectorMaterializedViewsTreatsDeletingEngineAsMissing(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	item := &models.VectorMaterializedView{
		ID:              1,
		TenantID:        7,
		ItemFingerprint: "fp-1",
		Locator:         "addp://engine/8/path/public/roads?type=table&item_id=99",
		SourceEngineID:  8,
		Status:          models.VectorMaterializedViewStatusReady,
	}
	got := service.filterMissingVectorMaterializedViews(
		context.Background(),
		7,
		[]*models.VectorMaterializedView{item},
		map[string]interface{}{"engine_id": 8},
	)
	if len(got) != 1 || got[0] != item {
		t.Fatalf("filterMissingVectorMaterializedViews() = %#v, want deleting engine candidate", got)
	}
}

func TestFilterMissingVectorMaterializedViewsSkipsAbandonedExternal(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	item := &models.VectorMaterializedView{
		ID:              1,
		TenantID:        7,
		ItemFingerprint: "fp-1",
		Locator:         "addp://engine/8/path/public/roads?type=table&item_id=99",
		SourceEngineID:  8,
		Status:          models.VectorMaterializedViewStatusAbandonedExternal,
	}
	got := service.filterMissingVectorMaterializedViews(
		context.Background(),
		7,
		[]*models.VectorMaterializedView{item},
		map[string]interface{}{"engine_id": 8},
	)
	if len(got) != 0 {
		t.Fatalf("filterMissingVectorMaterializedViews() = %#v, want abandoned result skipped", got)
	}
}

func TestExecuteCleanupAbandonsExternalMaterializedViewWithoutDroppingIt(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewVectorMaterializedViewRepository(db)
	optimizationSvc := NewVectorMaterializedViewTaskService(repo, nil)
	service := &CleanupService{optimizationSvc: optimizationSvc}
	item := cleanupTestVectorMaterializedView()
	item.ErrorMessage = "dial tcp: connection refused"
	if err := repo.CreateResult(context.Background(), item); err != nil {
		t.Fatalf("create vector materialized view: %v", err)
	}

	stats, err := service.ExecuteCleanup(context.Background(), item.TenantID, events.CleanupModePhysical, map[string]interface{}{
		"engine_id":                item.SourceEngineID,
		"external_artifact_policy": commonModels.ExternalArtifactPolicyAbandon,
		"requested_by":             uint(42),
	})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 0 || stats.AbandonedExternal != 1 || stats.VectorMaterializedViews != 1 {
		t.Fatalf("cleanup stats = %#v", stats)
	}

	stored, err := repo.GetResult(context.Background(), item.ID, item.TenantID)
	if err != nil {
		t.Fatalf("get abandoned result: %v", err)
	}
	if stored == nil || stored.Status != models.VectorMaterializedViewStatusAbandonedExternal {
		t.Fatalf("stored result = %#v, want abandoned_external", stored)
	}
	if stored.TargetSchema != item.TargetSchema || stored.TargetTable != item.TargetTable {
		t.Fatalf("external target identity changed: %#v", stored)
	}
	if stored.Metadata["external_artifact_policy"] != commonModels.ExternalArtifactPolicyAbandon {
		t.Fatalf("metadata = %#v, want abandon policy", stored.Metadata)
	}
	if stored.Metadata["abandoned_external_by"] != float64(42) && stored.Metadata["abandoned_external_by"] != uint(42) {
		t.Fatalf("abandoned_external_by = %#v, want 42", stored.Metadata["abandoned_external_by"])
	}
	if stored.Metadata["abandoned_external_at"] == nil || stored.Metadata["last_cleanup_error"] != item.ErrorMessage {
		t.Fatalf("metadata = %#v, want audit time and last cleanup error", stored.Metadata)
	}
	current, err := repo.GetCurrentResult(context.Background(), item.TenantID, item.ItemFingerprint, item.SourceGeometryColumn, item.TargetSRID)
	if err != nil {
		t.Fatalf("get current result after abandon: %v", err)
	}
	if current != nil {
		t.Fatalf("abandoned result still participates as current result: %#v", current)
	}
}

func TestExecuteCleanupDeletePolicyKeepsExternalRecordOnDropFailure(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewVectorMaterializedViewRepository(db)
	service := &CleanupService{optimizationSvc: NewVectorMaterializedViewTaskService(repo, nil)}
	item := cleanupTestVectorMaterializedView()
	if err := repo.CreateResult(context.Background(), item); err != nil {
		t.Fatalf("create vector materialized view: %v", err)
	}

	stats, err := service.ExecuteCleanup(context.Background(), item.TenantID, events.CleanupModePhysical, map[string]interface{}{
		"engine_id":                item.SourceEngineID,
		"external_artifact_policy": commonModels.ExternalArtifactPolicyDelete,
	})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(stats.Errors) != 1 || stats.AbandonedExternal != 0 || stats.DeletedPhysicalArtifacts != 0 {
		t.Fatalf("cleanup stats = %#v, want one drop error and retained record", stats)
	}
	stored, err := repo.GetResult(context.Background(), item.ID, item.TenantID)
	if err != nil || stored == nil {
		t.Fatalf("external record should remain after drop failure: result=%#v err=%v", stored, err)
	}
	if stored.Status == models.VectorMaterializedViewStatusAbandonedExternal {
		t.Fatalf("delete policy unexpectedly abandoned external target: %#v", stored)
	}
}

func cleanupTestVectorMaterializedView() *models.VectorMaterializedView {
	itemID := uint(99)
	return &models.VectorMaterializedView{
		TenantID:                  7,
		ItemFingerprint:           "fp-cleanup",
		ItemID:                    &itemID,
		Locator:                   "addp://engine/8/path/public/roads?type=table&item_id=99",
		SourceEngineID:            8,
		SourceSchema:              "public",
		SourceTable:               "roads",
		SourceGeometryColumn:      "geom",
		SourceSRID:                4326,
		TargetSRID:                3857,
		TargetKind:                models.VectorMaterializedViewTargetKindSourceSchemaMaterializedView,
		TargetSchema:              "public",
		TargetTable:               "addp_vmv_cleanup",
		TargetGeometryColumn:      models.VectorMaterializedViewTargetGeometryColumn,
		Status:                    models.VectorMaterializedViewStatusReady,
		SourceFingerprintSnapshot: commonModels.JSONMap{},
		Metadata:                  commonModels.JSONMap{"owner": "manager"},
	}
}

func TestNormalizeExportCleanupOptionsUsesInternalDefaults(t *testing.T) {
	t.Parallel()

	got := normalizeExportCleanupOptions(ExportCleanupOptions{})
	if got.SuccessRetention != 24*time.Hour {
		t.Fatalf("success retention = %v, want 24h", got.SuccessRetention)
	}
	if got.FailedRetention != 6*time.Hour {
		t.Fatalf("failed retention = %v, want 6h", got.FailedRetention)
	}
	if got.MaxRunningAge != 6*time.Hour {
		t.Fatalf("max running age = %v, want 6h", got.MaxRunningAge)
	}
	if got.Interval != 30*time.Minute {
		t.Fatalf("interval = %v, want 30m", got.Interval)
	}
}

func TestExportSessionCleanupPrefixUsesParentLocator(t *testing.T) {
	t.Parallel()

	service := &CleanupService{minioBucket: "manager"}
	session := &models.ExportSession{
		TargetParentLocator: "addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix",
		TargetLocator:       "addp-infra://minio/manager/tenant_7/export/20260620/session-1/roads.csv?type=object",
	}
	got, err := service.exportSessionCleanupPrefix(session)
	if err != nil {
		t.Fatalf("exportSessionCleanupPrefix() error = %v", err)
	}
	if got != "tenant_7/export/20260620/session-1/" {
		t.Fatalf("prefix = %q", got)
	}
}

func TestExportSessionCleanupPrefixRejectsDifferentBucket(t *testing.T) {
	t.Parallel()

	service := &CleanupService{minioBucket: "manager"}
	session := &models.ExportSession{
		TargetParentLocator: "addp-infra://minio/other/tenant_7/export/20260620/session-1?type=prefix",
	}
	if _, err := service.exportSessionCleanupPrefix(session); err == nil {
		t.Fatal("exportSessionCleanupPrefix() accepted different bucket")
	}
}
