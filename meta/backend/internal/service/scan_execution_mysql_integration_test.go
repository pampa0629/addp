package service

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/schema"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMetaClaimAndTaskSummaryAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_META_SCAN") != "1" {
		t.Skip("run the disposable platform-meta-scan MySQL gate")
	}
	t.Setenv("TZ", "Asia/Shanghai")
	open := func(name string) *gorm.DB {
		t.Helper()
		adapter, err := platformmysql.Dialector(database.Config{
			Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
			User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
			DBName: name, ConnectTimeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		db, err := gorm.Open(adapter, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Close() })
		return db
	}
	commonDB := open("common")
	if err := schema.InitializeCommon(commonDB); err != nil {
		t.Fatal(err)
	}
	db := open("meta")
	if err := repository.MigrateMySQLScanTasks(db); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	now := time.Now().UTC()
	create := func(name string, config commonmodels.JSONMap, createdAt time.Time) (*models.ScanTask, string) {
		t.Helper()
		id, pending := uuid.NewString(), execution.ExecutionStatusPending
		task := &models.ScanTask{
			TenantID: 7, EngineID: 99, Name: name, LastExecutionID: &id, LastExecutionStatus: &pending,
		}
		if err := db.Create(task).Error; err != nil {
			t.Fatal(err)
		}
		sourceTask := strconv.FormatUint(uint64(task.ID), 10)
		item := execution.TaskExecution{
			ExecutionID: id, TenantID: int(task.TenantID), SourceTaskID: &sourceTask,
			Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan, Source: execution.ModuleMeta,
			Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded,
			TriggerType: execution.TriggerTypeManual, ExecutionConfig: config, CreatedAt: createdAt,
		}
		if err := execution.CreatePending(ctx, db, &item); err != nil {
			t.Fatal(err)
		}
		return task, id
	}
	_, basicID := create("basic", nil, now)
	deepTask, deepID := create("deep", commonmodels.JSONMap{"production_basic_execution_id": basicID}, now.Add(-time.Second))
	svc := NewScanExecutionService(db, nil, nil, nil)
	basic, lease, err := svc.ClaimNextBoundedExecution(ctx, "meta-mysql-fixture", now, time.Minute)
	if err != nil || basic == nil || basic.ExecutionID != basicID || lease == nil {
		t.Fatalf("expected the basic scan first: %v", err)
	}
	if blocked, _, err := svc.ClaimNextBoundedExecution(ctx, "other-mysql-fixture", now, time.Minute); err != nil || blocked != nil {
		t.Fatalf("deep scan eligible before the basic scan completed: %v", err)
	}
	if err := svc.completeBoundedExecution(execution.ContextWithLease(ctx, *lease), basic, execution.ExecutionStatusSuccess,
		time.Now().UTC(), map[string]any{"progress": 100}); err != nil {
		t.Fatal(err)
	}
	deep, lease, err := svc.ClaimNextBoundedExecution(ctx, "meta-mysql-fixture", now, time.Minute)
	if err != nil || deep == nil || deep.ExecutionID != deepID || lease == nil || lease.Attempt != 1 {
		t.Fatalf("deep scan was not claimed exactly once: %v", err)
	}
	if err := svc.completeBoundedExecution(execution.ContextWithLease(ctx, *lease), deep, execution.ExecutionStatusSuccess,
		time.Now().UTC(), map[string]any{"progress": 100}); err != nil {
		t.Fatal(err)
	}
	var summary models.ScanTask
	if err := db.First(&summary, deepTask.ID).Error; err != nil {
		t.Fatal(err)
	}
	if summary.LastExecutionID == nil || *summary.LastExecutionID != deepID ||
		summary.LastExecutionStatus == nil || *summary.LastExecutionStatus != execution.ExecutionStatusSuccess {
		t.Fatal("task summary does not match the terminal execution")
	}
	repo := execution.NewTaskExecutionRepository(db)
	terminal, err := repo.GetByExecutionID(ctx, deepID, int(deepTask.TenantID))
	if err != nil || terminal.Status != execution.ExecutionStatusSuccess || terminal.CompletedAt == nil || terminal.Attempt != 1 {
		t.Fatalf("terminal execution did not match the summary: %v", err)
	}
	page, err := repo.ListEvents(ctx, deepID, int(deepTask.TenantID), 0, 5)
	if err != nil || len(page.Items) != 2 || page.Items[0].Kind != "started" || page.Items[1].Kind != "completed" {
		t.Fatalf("unexpected execution event sequence: %+v, %v", page, err)
	}
	for _, item := range page.Items {
		if item.ExecutionID != deepID || item.Attempt != 1 {
			t.Fatalf("unexpected event identity: %+v", item)
		}
	}
	t.Logf("task_id=%d execution_id=%s attempt=1 events=started,completed status=%s", deepTask.ID, deepID, terminal.Status)
	t.Run("concurrent production receipt and terminal replay", func(t *testing.T) {
		testProductionBasicScan(t, db, true)
	})
	exerciseUploadScanTerminalIdentity(t, db)
	t.Run("logical file ownership", func(t *testing.T) {
		tx := db.Begin()
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer tx.Rollback()
		if err := tx.Exec(`INSERT INTO meta.meta_item(tenant_id,engine_id,node_id,item_type,name,full_name,fingerprint,attributes)
			VALUES (7,99,1,'file','root','资料/目录',?,JSON_OBJECT('item',JSON_OBJECT('layout','whole'))),
			(7,99,1,'file','primary','group/main',?,JSON_OBJECT('item',JSON_OBJECT('refs',JSON_ARRAY(JSON_OBJECT('path','group/配图.png','role','sidecar')))))`,
			uuid.NewString(), uuid.NewString()).Error; err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			tenant uint
			path   string
			known  bool
		}{
			{7, "资料/目录/子项", true}, {7, "资料/目录二/子项", false},
			{7, "group/配图.png", true}, {7, "group/配图.PNG", false},
			{8, "group/配图.png", false},
		} {
			if _, known, err := logicalItemAlreadyKnown(tx, tc.tenant, plugin.FileItemPath(99, tc.path)); err != nil || known != tc.known {
				t.Fatalf("tenant %d path %q known=%t, err=%v", tc.tenant, tc.path, known, err)
			}
		}
	})
}
