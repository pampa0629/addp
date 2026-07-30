package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

func TestCreateUnscannedRunsSubmitsUnscannedEngines(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	capJSON := commonModels.JSONString(capabilities)
	tenantID := uint(1)
	engine := commonModels.Engine{
		ID:             9,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
		Capabilities:   &capJSON,
	}
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.Form.Get("tenant_id") != "1" {
				t.Errorf("token tenant_id = %q, want 1", r.Form.Get("tenant_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.Header.Get("Authorization") != "Bearer addp_at_meta" ||
			r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Errorf("unexpected System authentication headers: %#v", r.Header)
		}
		switch r.URL.Path {
		case "/api/v1/system/engines":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []commonModels.Engine{engine}, "total": 1, "page": 1, "page_size": 100,
			})
		case "/api/v1/system/engines/9":
			_ = json.NewEncoder(w).Encode(engine)
		default:
			t.Errorf("unexpected System path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer systemServer.Close()
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		systemServer.URL, "addp-meta", "meta-unscanned-test-secret-32-bytes", systemServer.Client(),
	)
	if err != nil {
		t.Fatalf("create Meta service token source: %v", err)
	}
	engineSvc := NewEngineService(
		db,
		commonClient.NewSystemServiceClient(systemServer.URL, tokenSource, systemServer.Client()),
	)

	repo := NewScanService(db, engineSvc)
	execSvc := NewScanExecutionService(db, repo, engineSvc, nil)

	runs, err := execSvc.CreateUnscannedRuns(context.Background(), tenantID, 7)
	if err != nil {
		t.Fatalf("CreateUnscannedRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs len = %d, want 1", len(runs))
	}
	if runs[0].ExecutionID == "" || runs[0].Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("run = %#v", runs[0])
	}
	if got := jsonMapUint(runs[0].ExecutionConfig, "engine_id"); got != 9 {
		t.Fatalf("execution engine_id = %d, want 9", got)
	}
	if _, exists := runs[0].ExecutionConfig["token"]; exists {
		t.Fatal("execution_config must not persist a user token")
	}
}

func TestCreateManualRunResolvesNodeTargetToCatalogPaths(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	capJSON := commonModels.JSONString(capabilities)
	tenantID := uint(1)
	engineSvc := NewEngineService(db, nil)
	engineSvc.cacheEngine(tenantID, &commonModels.Engine{
		ID:             9,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
		Capabilities:   &capJSON,
	})

	root := models.MetaNode{
		TenantID:   tenantID,
		EngineID:   9,
		NodeType:   "service",
		Name:       "Business MinIO",
		FullName:   "",
		Depth:      1,
		Path:       "1",
		ItemCount:  1,
		ScanStatus: "completed",
	}
	if err := db.Create(&root).Error; err != nil {
		t.Fatalf("create root node: %v", err)
	}
	bucket := models.MetaNode{
		TenantID:     tenantID,
		EngineID:     9,
		ParentNodeID: &root.ID,
		NodeType:     "bucket",
		Name:         "manager",
		FullName:     "manager",
		Depth:        2,
		Path:         "1/2",
		ItemCount:    7,
		ScanStatus:   "completed",
	}
	if err := db.Create(&bucket).Error; err != nil {
		t.Fatalf("create bucket node: %v", err)
	}

	repo := NewScanService(db, engineSvc)
	execSvc := NewScanExecutionService(db, repo, engineSvc, nil)

	run, err := execSvc.CreateManualRun(context.Background(), tenantID, 7, &models.ScanRequest{
		EngineID:    9,
		NodeID:      bucket.ID,
		ScanDepth:   "deep",
		Force:       true,
		TriggerType: "manual",
		Source:      commonExecution.ModuleTransfer,
		RefGroups: []models.ScanRefGroup{
			{
				Primary: "manager/a5.shp",
				Refs: []models.ScanRef{
					{Path: "manager/a5.shp", Role: "main", Required: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManualRun() error = %v", err)
	}

	if got := jsonMapStringSlice(run.ExecutionConfig, "catalog_paths"); !reflect.DeepEqual(got, []string{"manager"}) {
		t.Fatalf("catalog_paths = %#v, want manager", got)
	}
	if got := jsonMapUint(run.ExecutionConfig, "engine_id"); got != 9 {
		t.Fatalf("execution engine_id = %d, want 9", got)
	}
	if got := run.ExecutionConfig["source"]; got != "transfer" {
		t.Fatalf("source = %#v, want transfer", got)
	}
	if _, exists := run.ExecutionConfig["token"]; exists {
		t.Fatal("execution_config must not persist a user token")
	}
	refGroups, ok := run.ExecutionConfig["ref_groups"].([]models.ScanRefGroup)
	if !ok || len(refGroups) != 1 || refGroups[0].Primary != "manager/a5.shp" {
		t.Fatalf("ref_groups = %#v", run.ExecutionConfig["ref_groups"])
	}
}

func TestCreateManualRunPreservesItemID(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)

	tenantID := uint(1)
	engineSvc := NewEngineService(db, nil)
	engineSvc.cacheEngine(tenantID, &commonModels.Engine{
		ID:             9,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	})
	item := models.MetaItem{
		TenantID:    tenantID,
		EngineID:    9,
		NodeID:      1,
		ItemType:    "object",
		Name:        "a.shp",
		FullName:    "manager/a.shp",
		Fingerprint: "fp-item-refresh",
		Attributes: models.JSONMap{
			"storage": map[string]interface{}{
				"bucket": "manager",
				"path":   "a.shp",
			},
		},
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}

	execSvc := NewScanExecutionService(db, NewScanService(db, engineSvc), engineSvc, nil)
	run, err := execSvc.CreateManualRun(context.Background(), tenantID, 7, &models.ScanRequest{
		EngineID: 9,
		ItemID:   item.ID,
		Source:   commonExecution.ModuleManager,
		Force:    true,
	})
	if err != nil {
		t.Fatalf("CreateManualRun() error = %v", err)
	}
	if got := jsonMapUint(run.ExecutionConfig, "item_id"); got != item.ID {
		t.Fatalf("execution item_id = %d, want %d; config=%#v", got, item.ID, run.ExecutionConfig)
	}
	if got := jsonMapStringSlice(run.ExecutionConfig, "catalog_paths"); len(got) != 0 {
		t.Fatalf("item refresh catalog_paths = %#v, want empty", got)
	}
	if got, ok := run.ExecutionConfig["ref_groups"].([]models.ScanRefGroup); ok && len(got) != 0 {
		t.Fatalf("item refresh ref_groups = %#v, want empty", got)
	}
}

func TestCreateManualRunRejectsUnsupportedTriggerType(t *testing.T) {
	t.Parallel()

	execSvc := NewScanExecutionService(nil, nil, nil, nil)
	_, err := execSvc.CreateManualRun(context.Background(), 1, 7, &models.ScanRequest{
		EngineID:    9,
		TriggerType: "bad",
	})
	if err == nil {
		t.Fatal("CreateManualRun() should reject unsupported trigger_type")
	}
}

func TestCreateManualRunRejectsScheduledTriggerType(t *testing.T) {
	t.Parallel()

	execSvc := NewScanExecutionService(nil, nil, nil, nil)
	_, err := execSvc.CreateManualRun(context.Background(), 1, 7, &models.ScanRequest{
		EngineID:    9,
		TriggerType: "scheduled",
	})
	if err == nil {
		t.Fatal("CreateManualRun() should reject scheduled trigger_type")
	}
}

func TestUpsertEngineScanTaskFromPolicyCreatesTask(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	taskSvc := NewScanTaskService(db)
	task, err := taskSvc.UpsertEngineScanTaskFromPolicy(
		tenantID,
		7,
		9,
		"Business MinIO",
		&commonModels.ScanPolicy{
			Enabled:       true,
			ScheduledScan: true,
			ScheduleMode:  "daily",
			ScheduleTime:  "03:15",
			ScanDepth:     "deep",
		},
	)
	if err != nil {
		t.Fatalf("UpsertEngineScanTaskFromPolicy() error = %v", err)
	}
	if task == nil {
		t.Fatal("task should not be nil")
	}

	var persisted models.ScanTask
	if err := db.Where("owner_module = ? AND owner_ref = ?", "system", scantask.AutomaticTaskOwnerRef(9)).First(&persisted).Error; err != nil {
		t.Fatalf("find automatic task: %v", err)
	}
	if persisted.Schedule != "15 3 * * *" || !persisted.Enabled || persisted.NextRunAt == nil {
		t.Fatalf("automatic task schedule fields = %#v", persisted)
	}
	if persisted.Scope["type"] != "engine" || persisted.Parameters["scan_depth"] != "deep" {
		t.Fatalf("automatic task scope/parameters = %#v %#v", persisted.Scope, persisted.Parameters)
	}
}

func TestUpsertEngineScanTaskFromPolicyDeletesDisabledSchedule(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	now := time.Now()
	existing := scantask.NewEngineScanTask(tenantID, 7, 9, "Business MinIO", "15 3 * * *", "deep", now, nil)
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create existing automatic task: %v", err)
	}
	otherTenantTask := scantask.NewEngineScanTask(tenantID+1, 8, 9, "Other MinIO", "30 4 * * *", "deep", now, nil)
	if err := db.Create(otherTenantTask).Error; err != nil {
		t.Fatalf("create other tenant automatic task: %v", err)
	}

	taskSvc := NewScanTaskService(db)
	task, err := taskSvc.UpsertEngineScanTaskFromPolicy(
		tenantID,
		7,
		9,
		"Business MinIO",
		&commonModels.ScanPolicy{
			Enabled:       false,
			ScheduledScan: true,
			ScheduleMode:  "daily",
		},
	)
	if err != nil {
		t.Fatalf("UpsertEngineScanTaskFromPolicy() error = %v", err)
	}
	if task != nil {
		t.Fatalf("task = %#v, want nil", task)
	}

	var count int64
	if err := db.Model(&models.ScanTask{}).Where("tenant_id = ? AND owner_module = ? AND owner_ref = ?", tenantID, "system", scantask.AutomaticTaskOwnerRef(9)).Count(&count).Error; err != nil {
		t.Fatalf("count automatic task: %v", err)
	}
	if count != 0 {
		t.Fatalf("automatic task count = %d, want 0", count)
	}
	if err := db.Model(&models.ScanTask{}).Where("tenant_id = ? AND owner_module = ? AND owner_ref = ?", tenantID+1, "system", scantask.AutomaticTaskOwnerRef(9)).Count(&count).Error; err != nil {
		t.Fatalf("count other tenant automatic task: %v", err)
	}
	if count != 1 {
		t.Fatalf("other tenant automatic task count = %d, want 1", count)
	}
}

func TestScanResponseFromExecution(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	duration := int64(123)
	step := "执行完成"
	exec := &commonExecution.TaskExecution{
		Status:          commonExecution.ExecutionStatusSuccess,
		CurrentStep:     &step,
		StartedAt:       &started,
		ExecutionTimeMs: &duration,
		Metadata: commonModels.JSONMap{
			"catalog_nodes_scanned": 0,
			"items_scanned":         1,
			"fields_scanned":        5,
			"extraction": commonModels.JSONMap{
				"documents":    1,
				"extracted":    1,
				"unsupported":  0,
				"failed":       0,
				"indexed":      1,
				"index_failed": 0,
			},
		},
	}

	resp, err := scanflow.ScanResponseFromExecution(exec)
	if err != nil {
		t.Fatalf("ScanResponseFromExecution() error = %v", err)
	}
	if resp.Status != "success" || resp.ItemsScanned != 1 || resp.FieldsScanned != 5 || resp.DurationMs != duration {
		t.Fatalf("response = %#v", resp)
	}
	if resp.Extraction == nil || resp.Extraction.Documents != 1 || resp.Extraction.Indexed != 1 {
		t.Fatalf("extraction = %#v", resp.Extraction)
	}
}

func TestComputeInheritedTargetsIgnoresManualTasks(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	taskSvc := NewScanTaskService(db)
	parent := &models.ScanTask{
		ID:       100,
		TenantID: 1,
		EngineID: 9,
		Scope: models.JSONMap{
			"type":          "catalog_path",
			"catalog_paths": []interface{}{"bucket/a", "bucket/b", "bucket/c"},
		},
	}
	if err := db.Create(&models.ScanTask{
		TenantID: 1,
		EngineID: 9,
		Name:     "manual",
		Enabled:  true,
		Scope: models.JSONMap{
			"type":          "catalog_path",
			"catalog_paths": []interface{}{"bucket/b"},
		},
	}).Error; err != nil {
		t.Fatalf("create manual task: %v", err)
	}
	if err := db.Create(&models.ScanTask{
		TenantID: 1,
		EngineID: 9,
		Name:     "scheduled",
		Enabled:  true,
		Schedule: "15 3 * * *",
		Scope: models.JSONMap{
			"type":          "catalog_path",
			"catalog_paths": []interface{}{"bucket/c"},
		},
	}).Error; err != nil {
		t.Fatalf("create scheduled task: %v", err)
	}

	execSvc := NewScanExecutionService(db, nil, nil, nil)
	scheduler := NewScanTaskScheduler(taskSvc, execSvc)
	got := scheduler.computeInheritedTargets(parent)
	if !reflect.DeepEqual(got.CatalogPaths, []string{"bucket/a", "bucket/b"}) {
		t.Fatalf("catalog paths = %#v", got.CatalogPaths)
	}
}

func TestCreateTaskRejectsDuplicateEnabledScheduleScope(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	taskSvc := NewScanTaskService(db)
	ctx := context.Background()
	req := &models.ScanTaskUpsertRequest{
		Name:         "bucket c",
		EngineID:     9,
		CatalogPaths: []string{"bucket/c"},
		Schedule:     "15 3 * * *",
		Enabled:      true,
		ScanDepth:    "deep",
	}
	if _, err := taskSvc.CreateTask(ctx, 1, 7, req); err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}

	_, err := taskSvc.CreateTask(ctx, 1, 7, &models.ScanTaskUpsertRequest{
		Name:         "same bucket c",
		EngineID:     9,
		CatalogPaths: []string{"bucket/c"},
		Schedule:     "30 4 * * *",
		Enabled:      true,
		ScanDepth:    "deep",
	})
	if err == nil {
		t.Fatal("CreateTask() should reject duplicate enabled scheduled scope")
	}
}

func TestCreateTaskAllowsDuplicateScopeWhenScheduleDisabled(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	taskSvc := NewScanTaskService(db)
	ctx := context.Background()
	if _, err := taskSvc.CreateTask(ctx, 1, 7, &models.ScanTaskUpsertRequest{
		Name:         "scheduled bucket",
		EngineID:     9,
		CatalogPaths: []string{"bucket/c"},
		Schedule:     "15 3 * * *",
		Enabled:      true,
		ScanDepth:    "deep",
	}); err != nil {
		t.Fatalf("CreateTask() scheduled error = %v", err)
	}
	if _, err := taskSvc.CreateTask(ctx, 1, 7, &models.ScanTaskUpsertRequest{
		Name:         "manual bucket",
		EngineID:     9,
		CatalogPaths: []string{"bucket/c"},
		ScanDepth:    "deep",
	}); err != nil {
		t.Fatalf("CreateTask() manual duplicate scope error = %v", err)
	}
	if _, err := taskSvc.CreateTask(ctx, 1, 7, &models.ScanTaskUpsertRequest{
		Name:         "disabled bucket",
		EngineID:     9,
		CatalogPaths: []string{"bucket/c"},
		Schedule:     "30 4 * * *",
		Enabled:      false,
		ScanDepth:    "deep",
	}); err != nil {
		t.Fatalf("CreateTask() disabled duplicate scope error = %v", err)
	}
}

func TestDeleteTaskRestoresParentScheduleInheritance(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	taskSvc := NewScanTaskService(db)
	ctx := context.Background()
	parent, err := taskSvc.CreateTask(ctx, 1, 7, &models.ScanTaskUpsertRequest{
		Name:         "parent",
		EngineID:     9,
		CatalogPaths: []string{"bucket/a", "bucket/b", "bucket/c"},
		Schedule:     "15 3 * * *",
		Enabled:      true,
		ScanDepth:    "deep",
	})
	if err != nil {
		t.Fatalf("CreateTask() parent error = %v", err)
	}
	child, err := taskSvc.CreateTask(ctx, 1, 7, &models.ScanTaskUpsertRequest{
		Name:         "child",
		EngineID:     9,
		CatalogPaths: []string{"bucket/c"},
		Schedule:     "*/15 * * * *",
		Enabled:      true,
		ScanDepth:    "deep",
	})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}

	execSvc := NewScanExecutionService(db, nil, nil, nil)
	scheduler := NewScanTaskScheduler(taskSvc, execSvc)
	beforeDelete := scheduler.computeInheritedTargets(parent)
	if !reflect.DeepEqual(beforeDelete.CatalogPaths, []string{"bucket/a", "bucket/b"}) {
		t.Fatalf("before delete catalog paths = %#v, want a/b", beforeDelete.CatalogPaths)
	}

	if err := taskSvc.DeleteTask(ctx, 1, child.ID); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
	afterDelete := scheduler.computeInheritedTargets(parent)
	if !reflect.DeepEqual(afterDelete.CatalogPaths, []string{"bucket/a", "bucket/b", "bucket/c"}) {
		t.Fatalf("after delete catalog paths = %#v, want a/b/c", afterDelete.CatalogPaths)
	}
}

func TestUpsertEngineScanTaskRejectsDuplicateEnabledEngineSchedule(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	taskSvc := NewScanTaskService(db)
	if _, err := taskSvc.CreateTask(context.Background(), 1, 7, &models.ScanTaskUpsertRequest{
		Name:      "manual engine plan",
		EngineID:  9,
		Schedule:  "15 3 * * *",
		Enabled:   true,
		ScanDepth: "deep",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_, err := taskSvc.UpsertEngineScanTaskFromPolicy(
		1,
		7,
		9,
		"Business MinIO",
		&commonModels.ScanPolicy{
			Enabled:       true,
			ScheduledScan: true,
			ScheduleMode:  "daily",
			ScheduleTime:  "03:15",
			ScanDepth:     "deep",
		},
	)
	if err == nil {
		t.Fatal("UpsertEngineScanTaskFromPolicy() should reject duplicate engine schedule")
	}
}

func createTaskExecutionTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("ATTACH DATABASE ':memory:' AS common").Error; err != nil {
		t.Fatalf("attach common schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE common.task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			source_task_id TEXT,
			source_task_name TEXT,
			parent_execution_id TEXT,
			status TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			current_step TEXT,
			trigger_type TEXT NOT NULL,
			triggered_by INTEGER,
			actor_principal_id INTEGER,
			actor_tenant_membership_id INTEGER,
			issued_authorization_version INTEGER,
			execution_authorization_id INTEGER,
			authorization_effects TEXT,
			authorization_expires_at DATETIME,
			execution_config JSON,
			error_details JSON,
			metadata JSON,
			execution_time_ms INTEGER,
			rows_affected INTEGER,
			records_read INTEGER,
			records_written INTEGER,
			bytes_read INTEGER,
			bytes_written INTEGER,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create task_executions table: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE task_executions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			execution_id TEXT NOT NULL UNIQUE,
			module TEXT NOT NULL,
			task_type TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT '',
			source_task_id TEXT,
			source_task_name TEXT,
			parent_execution_id TEXT,
			status TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			current_step TEXT,
			trigger_type TEXT NOT NULL,
			triggered_by INTEGER,
			actor_principal_id INTEGER,
			actor_tenant_membership_id INTEGER,
			issued_authorization_version INTEGER,
			execution_authorization_id INTEGER,
			authorization_effects TEXT,
			authorization_expires_at DATETIME,
			execution_config JSON,
			error_details JSON,
			metadata JSON,
			execution_time_ms INTEGER,
			rows_affected INTEGER,
			records_read INTEGER,
			records_written INTEGER,
			bytes_read INTEGER,
			bytes_written INTEGER,
			started_at DATETIME,
			completed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create unqualified task_executions table: %v", err)
	}
}

func createScanTaskTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`
		CREATE TABLE meta.scan_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			engine_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			schedule TEXT,
			enabled BOOLEAN,
			scope JSON,
			parameters JSON,
			owner_module TEXT NOT NULL DEFAULT 'meta',
			owner_ref TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			last_execution_id TEXT,
			last_execution_status TEXT,
			created_by INTEGER,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create scan_tasks table: %v", err)
	}
}

func jsonMapUint(m commonModels.JSONMap, key string) uint {
	switch value := m[key].(type) {
	case uint:
		return value
	case int:
		if value > 0 {
			return uint(value)
		}
	case int64:
		if value > 0 {
			return uint(value)
		}
	case float64:
		if value > 0 {
			return uint(value)
		}
	}
	return 0
}

func jsonMapStringSlice(m commonModels.JSONMap, key string) []string {
	switch value := m[key].(type) {
	case []string:
		return value
	case []interface{}:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}
