package schema

import (
	"errors"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/database"
	platformmysql "github.com/addp/common/infra/database/mysql"
	"github.com/addp/common/models"
	"github.com/addp/common/runtimehealth"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPlatformMySQLExecutionLifecycle(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_COMMON_EXECUTION") != "1" {
		t.Skip("run the disposable platform-execution MySQL gate")
	}
	t.Setenv("TZ", "Asia/Tokyo")
	dialector, err := platformmysql.Dialector(database.Config{
		Host: "127.0.0.1", Port: os.Getenv("ADDP_TEST_MYSQL_PORT"),
		User: os.Getenv("ADDP_TEST_MYSQL_USER"), Password: os.Getenv("ADDP_TEST_MYSQL_PASSWORD"),
		DBName: "common", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close() })
	ctx := t.Context()
	var isolation string
	if err := db.Raw("SELECT @@SESSION.transaction_isolation").Scan(&isolation).Error; err != nil || isolation != "READ-COMMITTED" {
		t.Fatalf("execution connection isolation %q: %v", isolation, err)
	}
	for boot := 0; boot < 2; boot++ {
		if err := InitializeCommon(db); err != nil {
			t.Fatal(err)
		}
	}
	if err := RequireMySQL(db, "common", CommonVersion); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	id := uuid.NewString()
	pending := execution.TaskExecution{
		ExecutionID: id, TenantID: 7, Module: execution.ModuleMeta, TaskType: execution.TaskTypeScan,
		Source: execution.ModuleMeta, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded,
		TriggerType: execution.TriggerTypeManual, ExecutionConfig: models.JSONMap{"name": "冷启动"},
	}
	if err := execution.CreatePending(ctx, db, &pending); err != nil {
		t.Fatal(err)
	}
	identity := execution.PendingIdentity{
		ExecutionID: id, TenantID: pending.TenantID, Module: pending.Module, TaskType: pending.TaskType, Source: pending.Source,
	}
	options := execution.ClaimOptions{
		Module: pending.Module, TaskType: pending.TaskType, Source: pending.Source, WorkerID: "mysql-worker-fixture", LeaseDuration: time.Minute,
	}
	aborted := errors.New("owner rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, lease, err := execution.ClaimPending(ctx, tx, identity, options, nil)
		if err != nil {
			return err
		}
		if err := execution.AppendBoundedEvent(ctx, tx, *lease, execution.EventInput{Kind: "started"}); err != nil {
			return err
		}
		return aborted
	}); !errors.Is(err, aborted) {
		t.Fatalf("rollback of claim and started: %v", err)
	}
	page, err := repo.ListEvents(ctx, id, pending.TenantID, 0, 5)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("aborted attempt left events: %+v, %v", page, err)
	}

	var lease *execution.Lease
	if err := db.Transaction(func(tx *gorm.DB) error {
		item, claimed, err := execution.ClaimPending(ctx, tx, identity, options, nil)
		if err != nil {
			return err
		}
		if item.ExecutionID != id || item.Status != execution.ExecutionStatusRunning || claimed.Attempt != 1 ||
			item.ExecutionConfig["name"] != "冷启动" {
			t.Fatalf("unexpected claim: %+v", item)
		}
		lease = claimed
		return execution.AppendBoundedEvent(ctx, tx, *lease, execution.EventInput{Kind: "started"})
	}); err != nil {
		t.Fatal(err)
	}
	if second, _, err := execution.ClaimNext(ctx, db, options); err != nil || second != nil {
		t.Fatalf("same task was offered to a second claim: %+v, %v", second, err)
	}
	if err := execution.CompleteWithEvent(ctx, db, *lease, execution.ExecutionStatusSuccess, map[string]any{"progress": 100}); err != nil {
		t.Fatal(err)
	}
	terminal, err := repo.GetByExecutionID(ctx, id, pending.TenantID)
	if err != nil || terminal.Status != execution.ExecutionStatusSuccess || terminal.Attempt != 1 ||
		terminal.CompletedAt == nil || terminal.LeaseToken != nil || terminal.Progress != 100 {
		t.Fatalf("unexpected terminal state: %+v, %v", terminal, err)
	}
	if err := execution.RenewLease(ctx, db, *lease, time.Minute); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("terminal attempt retained its lease: %v", err)
	}
	page, err = repo.ListEvents(ctx, id, pending.TenantID, 0, 5)
	if err != nil || len(page.Items) != 2 || page.Items[0].Kind != "started" || page.Items[1].Kind != "completed" {
		t.Fatalf("unexpected event history: %+v, %v", page, err)
	}
	for _, event := range page.Items {
		if event.ExecutionID != id || event.Attempt != 1 {
			t.Fatalf("event identity changed: %+v", event)
		}
	}
	now := time.Now().UTC()
	health := runtimehealth.NewRepository(db)
	h := runtimehealth.Heartbeat{
		InstanceID: "fixture-" + id, Module: execution.ModuleMeta, Role: runtimehealth.RoleExecutionWorker,
		RuntimeName: execution.TaskTypeScan, Capacity: 1, StartedAt: now, HeartbeatAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := health.Publish(ctx, &h); err != nil {
		t.Fatal(err)
	}
	if err := health.Stop(ctx, h.InstanceID, now); err != nil {
		t.Fatal(err)
	}
	t.Logf("execution_id=%s attempt=1 events=started,completed status=%s", id, terminal.Status)
}
