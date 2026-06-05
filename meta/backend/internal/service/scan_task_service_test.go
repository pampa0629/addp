package service

import (
	"context"
	commonExecution "github.com/addp/common/execution"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

func TestCreateAutoRunsSubmitsUnscannedEngines(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)

	capabilities, err := plugin.MarshalEngineCapabilities(plugin.NewObjectCapabilities("s3"))
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}
	capJSON := commonModels.JSONString(capabilities)
	tenantID := uint(1)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.cacheEngine(&commonModels.Engine{
		ID:           9,
		TenantID:     &tenantID,
		Name:         "Business MinIO",
		EngineType:   "s3",
		IsActive:     true,
		Capabilities: &capJSON,
	})

	repo := NewScanService(db, engineSvc)
	taskSvc := NewScanTaskService(db, repo, engineSvc, nil)

	runs, err := taskSvc.CreateAutoRuns(context.Background(), tenantID, 7, "token")
	if err != nil {
		t.Fatalf("CreateAutoRuns() error = %v", err)
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
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.cacheEngine(&commonModels.Engine{
		ID:           9,
		TenantID:     &tenantID,
		Name:         "Business MinIO",
		EngineType:   "s3",
		IsActive:     true,
		Capabilities: &capJSON,
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
	taskSvc := NewScanTaskService(db, repo, engineSvc, nil)

	run, err := taskSvc.CreateManualRun(context.Background(), tenantID, 7, "token", &models.ScanRequest{
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
	refGroups, ok := run.ExecutionConfig["ref_groups"].([]models.ScanRefGroup)
	if !ok || len(refGroups) != 1 || refGroups[0].Primary != "manager/a5.shp" {
		t.Fatalf("ref_groups = %#v", run.ExecutionConfig["ref_groups"])
	}
}

func TestCreateManualRunPreservesItemID(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createTaskExecutionTable(t, db)

	tenantID := uint(1)
	engineSvc := NewEngineService(db, "", "", nil)
	engineSvc.cacheEngine(&commonModels.Engine{
		ID:         9,
		TenantID:   &tenantID,
		Name:       "Business MinIO",
		EngineType: "s3",
		IsActive:   true,
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

	taskSvc := NewScanTaskService(db, NewScanService(db, engineSvc), engineSvc, nil)
	run, err := taskSvc.CreateManualRun(context.Background(), tenantID, 7, "token", &models.ScanRequest{
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
}

func TestCreateManualRunRejectsUnsupportedTriggerType(t *testing.T) {
	t.Parallel()

	taskSvc := NewScanTaskService(nil, nil, nil, nil)
	_, err := taskSvc.CreateManualRun(context.Background(), 1, 7, "token", &models.ScanRequest{
		EngineID:    9,
		TriggerType: "bad",
	})
	if err == nil {
		t.Fatal("CreateManualRun() should reject unsupported trigger_type")
	}
}

func TestCreateOrUpdateTaskFromScanConfigCreatesAutomaticTask(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	taskSvc := NewScanTaskService(db, nil, nil, nil)
	err := taskSvc.CreateOrUpdateTaskFromScanConfig(&commonModels.Engine{
		ID:       9,
		TenantID: &tenantID,
		Name:     "Business MinIO",
		ScanConfig: &commonModels.ScanConfig{
			Enabled:       true,
			ScheduledScan: true,
			ScheduleType:  "daily",
			ScheduleTime:  "03:15",
			ScanDepth:     "deep",
		},
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateTaskFromScanConfig() error = %v", err)
	}

	var task models.ScanTask
	if err := db.Where("owner_module = ? AND owner_ref = ?", "system", scantask.AutomaticTaskOwnerRef(9)).First(&task).Error; err != nil {
		t.Fatalf("find automatic task: %v", err)
	}
	if task.Schedule != "15 3 * * *" || !task.Enabled || task.NextRunAt == nil {
		t.Fatalf("automatic task schedule fields = %#v", task)
	}
	if task.Scope["type"] != "engine" || task.Parameters["scan_depth"] != "deep" {
		t.Fatalf("automatic task scope/parameters = %#v %#v", task.Scope, task.Parameters)
	}
}

func TestCreateOrUpdateTaskFromScanConfigDeletesDisabledSchedule(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	existing := scantask.NewAutomaticTask(&commonModels.Engine{
		ID:       9,
		TenantID: &tenantID,
		Name:     "Business MinIO",
		ScanConfig: &commonModels.ScanConfig{
			Enabled: true,
		},
	}, tenantID, "15 3 * * *")
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create existing automatic task: %v", err)
	}

	taskSvc := NewScanTaskService(db, nil, nil, nil)
	err := taskSvc.CreateOrUpdateTaskFromScanConfig(&commonModels.Engine{
		ID:       9,
		TenantID: &tenantID,
		Name:     "Business MinIO",
		ScanConfig: &commonModels.ScanConfig{
			Enabled:       false,
			ScheduledScan: true,
			ScheduleType:  "daily",
		},
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateTaskFromScanConfig() error = %v", err)
	}

	var count int64
	if err := db.Model(&models.ScanTask{}).Where("owner_module = ? AND owner_ref = ?", "system", scantask.AutomaticTaskOwnerRef(9)).Count(&count).Error; err != nil {
		t.Fatalf("count automatic task: %v", err)
	}
	if count != 0 {
		t.Fatalf("automatic task count = %d, want 0", count)
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

	taskSvc := NewScanTaskService(db, nil, nil, nil)
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

	got := taskSvc.computeInheritedTargets(parent)
	if !reflect.DeepEqual(got.CatalogPaths, []string{"bucket/a", "bucket/b"}) {
		t.Fatalf("catalog paths = %#v", got.CatalogPaths)
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
			source_task_id INTEGER,
			source_task_name TEXT,
			parent_execution_id TEXT,
			status TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			current_step TEXT,
			trigger_type TEXT NOT NULL,
			triggered_by INTEGER,
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
