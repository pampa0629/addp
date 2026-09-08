package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/exportartifact"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
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
		FreedBytes:               1024,
		MarkedMissingSource:      6,
		SkippedExternalTargets:   1,
		TaskDefinitions:          8,
		DisabledTaskDefinitions:  8,
		DeletedTaskDefinitions:   3,
		Errors:                   []string{"one", "two"},
	}

	scanSummary := managerScanSummary(stats)
	if scanSummary.ScannedItems != 25 || scanSummary.FreedBytes != 1024 || scanSummary.DisabledTaskDefinitions != 0 || scanSummary.SkippedItems != 1 || scanSummary.ErrorCount != 2 {
		t.Fatalf("scan summary = %#v", scanSummary)
	}
	if scanSummary.RiskLevel != "low" {
		t.Fatalf("scan risk = %q, want low", scanSummary.RiskLevel)
	}

	executeSummary := managerExecuteSummary(stats)
	if executeSummary.AffectedRecords != 28 {
		t.Fatalf("affected_records = %d, want 28", executeSummary.AffectedRecords)
	}
	if executeSummary.DeletedPhysicalArtifacts != 4 || executeSummary.FreedBytes != 1024 || executeSummary.MarkedMissingSource != 6 || executeSummary.DisabledTaskDefinitions != 8 {
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

	targets := cleanupTaskTargetsFromDefinition(repository.CleanupTaskDefinition{Config: commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"source_engine_id": float64(26),
			"item_locator":     "addp://engine/26/path/3d/model.glb?type=file&item_id=88",
		},
		"target": commonModels.JSONMap{
			"engine_id":        float64(12),
			"item_id":          float64(99),
			"item_fingerprint": "fp-1",
			"locator":          "addp://engine/12/path/public/roads?type=table&item_id=99",
		},
	}})
	if len(targets) != 2 || targets[0].EngineID != 26 || !targets[0].VerifySource {
		t.Fatalf("source targets = %#v", targets)
	}
	if targets[1].EngineID != 12 || targets[1].ItemID != 99 || targets[1].ItemFingerprint != "fp-1" || !targets[1].VerifySource {
		t.Fatalf("target refs = %#v", targets[1])
	}
}

func TestManagerCleanupTaskRegistryCoversTaskProviderDeclaration(t *testing.T) {
	declaration, err := ManagerTaskProviderDeclaration()
	if err != nil {
		t.Fatalf("ManagerTaskProviderDeclaration() error = %v", err)
	}
	var payload struct {
		TaskCapabilities []struct {
			Type string `json:"type"`
		} `json:"task_capabilities"`
	}
	if declaration.Capabilities == nil {
		t.Fatal("Manager task provider capabilities are required")
	}
	if err := json.Unmarshal([]byte(*declaration.Capabilities), &payload); err != nil {
		t.Fatalf("decode Manager task provider capabilities: %v", err)
	}
	registered := map[string]bool{}
	for _, spec := range repository.ManagerTaskDefinitionSpecs() {
		registered[spec.TaskType] = true
	}
	for _, capability := range payload.TaskCapabilities {
		if !registered[capability.Type] {
			t.Fatalf("TaskProvider task type %q is not registered for cleanup", capability.Type)
		}
		delete(registered, capability.Type)
	}
	if len(registered) != 0 {
		t.Fatalf("cleanup-only task types = %#v", registered)
	}
}

func TestCleanupTaskTargetSourceExistsTreatsDeletedEngineAsMissing(t *testing.T) {
	t.Parallel()

	service := &CleanupService{}
	target := cleanupTaskTarget{EngineID: 12, ItemID: 99, Locator: "addp://engine/12/path/public/roads?type=table&item_id=99", VerifySource: true}
	if got := service.taskTargetSourceExists(nil, 1, target, map[string]interface{}{"engine_id": 12}, nil); got {
		t.Fatal("taskTargetSourceExists() = true, want false for engine deleted context")
	}
	if got := service.taskTargetSourceExists(nil, 1, cleanupTaskTarget{}, nil, nil); !got {
		t.Fatal("empty task target should be treated as existing to avoid broad cleanup")
	}
	if got := service.taskTargetSourceExists(nil, 1, cleanupTaskTarget{EngineID: 26}, nil, map[uint]struct{}{12: {}}); got {
		t.Fatal("missing tenant engine should be treated as missing")
	}
}

func TestManagerCleanupFindsAndPhysicallyDeletesPreviouslyDisabledUnknownEngineTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	ensureDerivedTaskDefinitionTestTables(t, db)
	if err := db.Exec(`INSERT INTO manager.task_definitions
		(tenant_id, task_type, version, name, enabled, semantic_key, config)
		VALUES (1, 'model_3d_glb_generation', 1, 'unknown engine task', TRUE, 'fp-model', '{}')`).Error; err != nil {
		t.Fatalf("insert task: %v", err)
	}
	if err := db.Exec(`INSERT INTO manager.task_resource_bindings
		(task_definition_id, tenant_id, role, engine_id, locator, item_fingerprint, ordinal)
		VALUES (1, 1, 'source', 26, 'addp://engine/26/path/model.glb?type=file', 'fp-model', 0)`).Error; err != nil {
		t.Fatalf("insert task resource binding: %v", err)
	}
	repo := repository.NewCleanupTaskDefinitionRepository(db, []repository.CleanupTaskDefinitionSpec{{
		TaskType: "model_3d_glb_generation", Table: "manager.task_definitions",
	}})
	svc := &CleanupService{taskDefinitionRepo: repo, systemClient: newManagerCleanupEmptySystemClient(t)}

	scan, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if scan.TaskDefinitions != 1 {
		t.Fatalf("task definitions = %d, want 1", scan.TaskDefinitions)
	}
	logical, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModeLogical, nil)
	if err != nil || logical.DisabledTaskDefinitions != 1 {
		t.Fatalf("logical cleanup = %#v, error = %v", logical, err)
	}
	physical, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, nil)
	if err != nil || physical.DeletedTaskDefinitions != 1 {
		t.Fatalf("physical cleanup = %#v, error = %v", physical, err)
	}
	remaining, err := repo.List(context.Background(), 1)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining tasks = %#v, error = %v", remaining, err)
	}
	var bindingCount int64
	if err := db.Table("manager.task_resource_bindings").Count(&bindingCount).Error; err != nil || bindingCount != 0 {
		t.Fatalf("remaining task bindings = %d, error = %v", bindingCount, err)
	}
}

func TestManagerCleanupFindsUnknownEngineManagedQuickViewArtifacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`ATTACH DATABASE ':memory:' AS manager`).Error; err != nil {
		t.Fatalf("attach manager schema: %v", err)
	}
	specs := []repository.CleanupManagedArtifactSpec{
		{TaskType: commonExecution.TaskTypeModel3DGLBGeneration, Table: "manager.model_3d_glb"},
		{TaskType: commonExecution.TaskTypeModel3DTilesGeneration, Table: "manager.model3d_tiles"},
		{TaskType: commonExecution.TaskTypeGaussianSplatKSplatGeneration, Table: "manager.gaussian_splat_ksplat"},
		{TaskType: commonExecution.TaskTypePointCloudCOPCGeneration, Table: "manager.point_cloud_copc"},
	}
	for _, spec := range specs {
		if err := db.Exec(`CREATE TABLE ` + spec.Table + ` (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_engine_id INTEGER NOT NULL,
			item_id INTEGER, item_fingerprint TEXT, locator TEXT, status TEXT, error_message TEXT,
			size_bytes INTEGER, source_size_bytes INTEGER,
			updated_at DATETIME, deleted_at DATETIME
		)`).Error; err != nil {
			t.Fatalf("create %s: %v", spec.Table, err)
		}
		if err := db.Exec(`INSERT INTO ` + spec.Table + ` (tenant_id, source_engine_id, item_id, item_fingerprint, locator, status, size_bytes)
			VALUES (1, 26, 88, 'fp-88', 'addp://engine/26/path/source.bin?type=file&item_id=88', 'ready', 2048)`).Error; err != nil {
			t.Fatalf("insert %s: %v", spec.Table, err)
		}
	}
	repo := repository.NewCleanupManagedArtifactRepository(db, specs)
	svc := &CleanupService{managedArtifactRepo: repo, systemClient: newManagerCleanupEmptySystemClient(t)}

	scan, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if scan.ManagedArtifacts != 4 || scan.FreedBytes != 8192 {
		t.Fatalf("managed artifact scan = %#v, want 4 artifacts and 8192 bytes", scan)
	}
	logical, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModeLogical, nil)
	if err != nil || logical.ManagedArtifacts != 4 || logical.MarkedMissingSource != 4 {
		t.Fatalf("logical cleanup = %#v, error = %v", logical, err)
	}
}

func newManagerCleanupEmptySystemClient(t *testing.T) *commonClient.SystemServiceClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/system/oauth/token":
			_ = json.NewEncoder(response).Encode(map[string]interface{}{
				"access_token": "addp_at_manager", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
		case "/api/v1/system/engines":
			_ = json.NewEncoder(response).Encode([]interface{}{})
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		server.URL, "addp-manager", "manager-cleanup-test-client-secret-32-bytes", server.Client(),
	)
	if err != nil {
		t.Fatalf("create service token source: %v", err)
	}
	return commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client())
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

	got := exportartifact.NormalizeCleanupOptions(ExportCleanupOptions{})
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

	session := &models.ExportSession{
		TargetParentLocator: "addp-infra://minio/manager/tenant_7/export/20260620/session-1?type=prefix",
		TargetLocator:       "addp-infra://minio/manager/tenant_7/export/20260620/session-1/roads.csv?type=object",
	}
	got, err := exportartifact.SessionObjectPrefix(session, "manager")
	if err != nil {
		t.Fatalf("exportSessionCleanupPrefix() error = %v", err)
	}
	if got != "tenant_7/export/20260620/session-1" {
		t.Fatalf("prefix = %q", got)
	}
}

func TestExportSessionCleanupPrefixRejectsDifferentBucket(t *testing.T) {
	t.Parallel()

	session := &models.ExportSession{
		TargetParentLocator: "addp-infra://minio/other/tenant_7/export/20260620/session-1?type=prefix",
	}
	if _, err := exportartifact.SessionObjectPrefix(session, "manager"); err == nil {
		t.Fatal("exportSessionCleanupPrefix() accepted different bucket")
	}
}
