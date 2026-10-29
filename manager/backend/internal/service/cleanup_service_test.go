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
	"github.com/addp/common/execution/executiontest"
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
		{name: "empty grants no module", expected: nil, module: events.ModuleManager, want: false},
		{name: "includes module", expected: []string{events.ModuleMeta, events.ModuleManager}, module: events.ModuleManager, want: true},
		{name: "does not include module", expected: []string{events.ModuleMeta}, module: events.ModuleManager, want: false},
		{name: "rejects old stream value", expected: []string{" manager "}, module: events.ModuleManager, want: false},
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
	if scanSummary.ScannedItems != 25 || scanSummary.FreedBytes != 0 || scanSummary.DisabledTaskDefinitions != 0 || scanSummary.SkippedItems != 1 || scanSummary.ErrorCount != 2 {
		t.Fatalf("scan summary = %#v", scanSummary)
	}
	if scanSummary.RiskLevel != "low" {
		t.Fatalf("scan risk = %q, want low", scanSummary.RiskLevel)
	}

	executeSummary := managerExecuteSummary(stats)
	if executeSummary.AffectedRecords != 28 {
		t.Fatalf("affected_records = %d, want 28", executeSummary.AffectedRecords)
	}
	if executeSummary.DeletedPhysicalArtifacts != 4 || executeSummary.FreedBytes != 1024 || executeSummary.MarkedMissingSource != 6 || executeSummary.DisabledTaskDefinitions != 8 || executeSummary.DeletedTaskDefinitions != 3 {
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

	targets := cleanupTaskTargetsFromDefinition(repository.CleanupTaskDefinition{TaskType: commonExecution.TaskTypeEmbedding, Config: commonModels.JSONMap{
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

func TestCleanupTaskTargetReasonUsesExactEngineEvidence(t *testing.T) {
	svc := &CleanupService{systemClient: newManagerCleanupEmptySystemClient(t)}
	for _, tc := range []struct {
		target cleanupTaskTarget
		scope  map[string]interface{}
		want   string
	}{
		{cleanupTaskTarget{EngineID: 12, VerifySource: true}, map[string]interface{}{"engine_id": 12}, models.TaskBindingIssueMissingEngine},
		{cleanupTaskTarget{}, nil, ""},
		{cleanupTaskTarget{EngineID: 26}, nil, models.TaskBindingIssueMissingEngine},
	} {
		got, err := svc.cleanupTargetReason(t.Context(), 1, tc.target, tc.scope, map[uint]bool{})
		if err != nil || got != tc.want {
			t.Fatalf("reason=%s err=%v", got, err)
		}
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
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	plan, err := svc.buildCleanupPlan(t.Context(), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return repo.ApplyFrozen(t.Context(), tx, plan.Tasks[0], false) }); err != nil {
		t.Fatal(err)
	}
	var marked models.TaskDefinition
	if err := db.Table("manager.task_definitions").Where("id = ?", 1).Take(&marked).Error; err != nil {
		t.Fatalf("load logically cleaned task: %v", err)
	}
	if marked.Enabled || marked.BindingStatus != models.TaskBindingStatusMissing || marked.BindingIssue != models.TaskBindingIssueMissingEngine {
		t.Fatalf("logically cleaned task = %#v", marked)
	}
	plan, err = svc.buildCleanupPlan(t.Context(), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return repo.ApplyFrozen(t.Context(), tx, plan.Tasks[0], true) }); err != nil {
		t.Fatal(err)
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
	// Scan is read-only: even a missing Engine must not mark or delete an artifact.
	for _, spec := range specs {
		var count int64
		if err := db.Table(spec.Table).Where("status='ready'").Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("scan mutated %s: %d %v", spec.Table, count, err)
		}
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

func TestCleanupPlanIncludesDeletingEngineMaterializedView(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewVectorMaterializedViewRepository(db)
	item := cleanupTestVectorMaterializedView()
	if err := repo.CreateResult(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	svc := &CleanupService{optimizationSvc: NewVectorMaterializedViewTaskService(repo, nil)}
	plan, err := svc.buildCleanupPlan(t.Context(), 7, map[string]interface{}{"engine_id": uint(8)})
	if err != nil || len(plan.Vectors) != 1 || plan.Vectors[0].ID != item.ID {
		t.Fatal("exact deleting Engine not selected", plan, err)
	}
}

func TestCleanupPlanExcludesAbandonedExternalMaterializedView(t *testing.T) {
	db := newTileCacheTaskServiceTestDB(t)
	repo := repository.NewVectorMaterializedViewRepository(db)
	item := cleanupTestVectorMaterializedView()
	item.Status = models.VectorMaterializedViewStatusAbandonedExternal
	if err := repo.CreateResult(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	svc := &CleanupService{optimizationSvc: NewVectorMaterializedViewTaskService(repo, nil)}
	plan, err := svc.buildCleanupPlan(t.Context(), 7, map[string]interface{}{"engine_id": uint(8)})
	if err != nil || len(plan.Vectors) != 0 {
		t.Fatal("abandoned target selected again", plan, err)
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
