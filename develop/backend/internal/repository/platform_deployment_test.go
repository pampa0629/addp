package repository

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestInstalledDevelopTask(t *testing.T) {
	if os.Getenv("ADDP_TEST_DEVELOP_DEPLOYMENT") != "1" {
		t.Skip("requires isolated Develop deployment")
	}
	db, err := bootstrap.OpenPlatformRuntime(database.Config{Host: os.Getenv("POSTGRES_HOST"), Port: os.Getenv("POSTGRES_PORT"), DBName: os.Getenv("POSTGRES_DB"), Schema: "develop,public"}, "develop", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewDevTaskRepository(db)
	task := models.DevTask{TenantID: 7, Name: "OrderReport", DisplayName: "订单", DevType: "query", Content: models.DevTaskContent{"query_type": "sql"}, Status: "active", Tags: []string{"统计", "季度,报告"}}
	if err := repo.Create(&task); err != nil {
		t.Fatal(err)
	}
	filter := &models.ListDevTasksRequest{Keyword: "orderreport", Tag: "季度,报告", Page: 1, PageSize: 10}
	rows, total, err := repo.List(filter, 7)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].ID != task.ID || len(rows[0].Tags) != 2 {
		t.Fatalf("task list: %v %d %v", rows, total, err)
	}
	_, total, err = repo.List(filter, 8)
	if err != nil || total != 0 {
		t.Fatalf("other tenant count: %d %v", total, err)
	}
	var event models.CatalogResourceChangeRow
	if err := db.Where("tenant_id=? AND source_identity=?", 7, task.ID).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Operation != "upsert" {
		t.Fatalf("catalog operation: %s", event.Operation)
	}
	t.Run("compiled workflow keeps original parent lease", func(t *testing.T) {
		testInstalledFileWorkflowParent(t, db)
	})
	t.Run("file workflow admission expiry", func(t *testing.T) {
		if err := db.WithContext(t.Context()).Connection(func(conn *gorm.DB) error {
			conn = conn.Session(&gorm.Session{NewDB: true})
			if conn.Dialector.Name() == "mysql" {
				if err := conn.Exec(fmt.Sprintf("SET timestamp = %d.750000", time.Now().Unix())).Error; err != nil {
					return err
				}
				defer func() {
					if err := conn.Exec("SET timestamp = DEFAULT").Error; err != nil {
						t.Errorf("restore database fixture clock: %v", err)
					}
				}()
			}
			item, sources := newFilePendingValues()
			runFileQueueCase(t, conn, NewFileExecutionRepository(conn), item, sources, "101")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		t.Log("expired admission returned for observation; refreshed admission permitted exactly one claim")
	})
	t.Run("dual target worker admission", func(t *testing.T) { checkDualTargetWorkerClaim(t, db) })
	t.Logf("task %d: tags and query definition read in tenant; catalog upsert observed", task.ID)
}

func testInstalledFileWorkflowParent(t *testing.T, db *gorm.DB) {
	t.Helper()
	now, err := execution.DatabaseNow(db)
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := newFilePendingValues()
	parent.Module, parent.Source, parent.TaskType = execution.ModuleOrchestrator, execution.ModuleOrchestrator, execution.TaskTypeOrchestration
	parent.Status, parent.Attempt, parent.StartedAt = execution.ExecutionStatusRunning, 1, &now
	owner, token, expires := "workflow-fixture", uuid.NewString(), now.Add(time.Minute)
	parent.LeaseOwner, parent.LeaseToken, parent.LeaseExpiresAt = &owner, &token, &expires
	if err := db.Create(parent).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewFileExecutionRepository(db)
	child, sources := newFilePendingValues()
	child.Source, child.ParentExecutionID = execution.ModuleOrchestrator, &parent.ExecutionID
	facts, err := repo.CreatePending(t.Context(), child, sources, nil)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := repo.Read(t.Context(), uint(child.TenantID), child.ExecutionID)
	expected, parentErr := execution.NewDevelopFileParent(parent.ExecutionID, parent.Attempt, token)
	if err != nil || parentErr != nil || !reflect.DeepEqual(facts.Intent.Parent, expected) || !reflect.DeepEqual(frozen.Intent.Parent, expected) {
		t.Fatalf("parent lease binding failed: read=%v parent=%v", err, parentErr)
	}

	for _, reason := range []string{"other tenant", "expired parent"} {
		denied, deniedSources := newFilePendingValues()
		denied.Source, denied.ParentExecutionID = execution.ModuleOrchestrator, &parent.ExecutionID
		if reason == "other tenant" {
			denied.TenantID++
		} else if err := db.Model(parent).UpdateColumn("lease_expires_at", now.Add(-time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CreatePending(t.Context(), denied, deniedSources, nil); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("%s accepted or storage error: %v", reason, err)
		}
		for _, model := range []any{&execution.TaskExecution{}, &models.FileExecution{}} {
			var count int64
			if err := db.Model(model).Where("execution_id = ?", denied.ExecutionID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("%s left a rejected execution: count=%d err=%v", reason, count, err)
			}
		}
	}
}

// Exercise the same candidate filter and lease acquisition as the workflow Worker.
func checkDualTargetWorkerClaim(t *testing.T, db *gorm.DB) {
	ctx := t.Context()
	files := NewFileExecutionRepository(db)
	queue := NewExecutionRepository(db)
	item, sources := newFilePendingValues()
	targets := []execution.DevelopFileTargetIntent{
		{NodeID: "main", Target: plugin.FileItemPath(11, "outputs/main.gpkg"), Mode: "fail"},
		{NodeID: "members", Target: plugin.FileItemPath(11, "outputs/members.gpkg"), Mode: "fail"},
	}
	facts, err := files.CreatePending(ctx, item, sources, targets)
	if err != nil {
		t.Fatal(err)
	}
	issued := fileIssuedFixture(t, facts)
	issued.Authorization.ID = "102"
	if err := files.BindAuthorization(ctx, 7, item.ExecutionID, issued); err != nil {
		t.Fatal(err)
	}
	facts, err = files.Read(ctx, 7, item.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	obs := fileAdmissionFixture(facts)
	obs.AuthorizationID, obs.State = issued.Authorization.ID, "awaiting_approval"
	if err := files.ApplyAdmission(ctx, 7, item.ExecutionID, obs); err != nil {
		t.Fatal(err)
	}
	obs.State, obs.ApprovalIDs = "ready", []string{uuid.NewString()}
	if err := files.ApplyAdmission(ctx, 7, item.ExecutionID, obs); err == nil {
		t.Fatal("ready receipt accepted with only one approval for two targets")
	}
	claimed, lease, err := queue.ClaimNext("workflow", ctx, "two-file-worker", time.Now(), time.Minute, nil)
	if err != nil || claimed != nil || lease != nil {
		t.Fatalf("partial approvals allowed a claim: claimed=%v lease=%v err=%v", claimed != nil, lease != nil, err)
	}
	obs.ApprovalIDs = append(obs.ApprovalIDs, uuid.NewString())
	if err := files.ApplyAdmission(ctx, 7, item.ExecutionID, obs); err != nil {
		t.Fatal(err)
	}
	claimed, lease, err = queue.ClaimNext("workflow", ctx, "two-file-worker", time.Now(), time.Minute, nil)
	if err != nil || lease == nil || claimed == nil || claimed.ExecutionID != item.ExecutionID {
		t.Fatal("fully approved task did not receive its worker lease", err)
	}
}
