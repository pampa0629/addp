package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func graphExecutionPG(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("standard Graph PostgreSQL gate required")
	}
	db, err := gorm.Open(postgres.Open(graphRepositoryIntegrationDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err = db.Exec("CREATE SCHEMA IF NOT EXISTS graph").Error; err != nil {
		t.Fatal(err)
	}
	if err = execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	if err = MigrateGraphTables(db); err != nil {
		t.Fatal(err)
	}
	return db
}
func graphExecutionPGFixture(t *testing.T, db *gorm.DB) (models.BuildTask, models.BuildMaterial, *execution.TaskExecution) {
	t.Helper()
	tenant := uint(930000000 + time.Now().UnixNano()%10000000)
	task, mat := createBuildRepositoryTestTask(t, db, tenant, models.BuildStatusPending)
	t.Cleanup(func() {
		for _, model := range []interface{}{&models.GraphOperation{}, &models.BuildExecutionControl{}, &models.ReviewItem{}, &models.BuildMaterial{}, &models.BuildTask{}, &models.KnowledgeGraph{}, &models.Ontology{}, &models.TenantAdmissionLock{}, &execution.Event{}, &execution.TaskExecution{}} {
			if err := db.Where("tenant_id = ?", tenant).Delete(model).Error; err != nil {
				t.Error(err)
			}
		}
	})
	e := newGraphRepositoryTestExecution(uuid.NewString(), int(tenant), time.Time{})
	if _, _, err := enqueueRepositoryFixture(graphRepositoryUserContext(tenant), NewBuildRepository(db), task, e, BuildExecutionClaimRun); err != nil {
		t.Fatal(err)
	}
	return task, mat, e
}

func TestIntegrationPostgresGraphExactClaimAndCancellation(t *testing.T) {
	db := graphExecutionPG(t)
	task, _, e := graphExecutionPGFixture(t, db)
	repo := NewBuildRepository(db)
	ctx := graphRepositoryUserContext(task.TenantID)
	start := make(chan struct{})
	results := make(chan error, 2)
	leases := make(chan *execution.Lease, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-start
			_, lease, _, _, err := NewBuildRepository(db).ClaimBuild(ctx, *e, fmt.Sprintf("backend-%d", i), time.Minute)
			leases <- lease
			results <- err
		}(i)
	}
	close(start)
	wins, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, commonapi.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("claims=%d conflicts=%d", wins, conflicts)
	}
	var lease *execution.Lease
	for range 2 {
		if value := <-leases; value != nil {
			lease = value
		}
	}
	if lease == nil {
		t.Fatal("winning claim missing lease")
	}
	if _, err := repo.CancelExecution(ctx, task.ID, task.GraphID, task.TenantID, 7, e.ExecutionID, 0); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("stale pending observation accepted: %v", err)
	}
	result, err := repo.CancelExecution(ctx, task.ID, task.GraphID, task.TenantID, 7, e.ExecutionID, lease.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execution.ExecutionStatusRunning || result.Attempt != 1 || result.StartedAt == nil {
		t.Fatalf("cancel intent invented terminal: %+v", result)
	}
	if err := repo.CompleteBuild(execution.ContextWithLease(ctx, *lease), &task, execution.ExecutionStatusSuccess, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	var terminal execution.TaskExecution
	if err := db.Where("execution_id = ?", e.ExecutionID).First(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	if terminal.Status != execution.ExecutionStatusCancelled || terminal.ExecutionTimeMs == nil {
		t.Fatalf("terminal=%+v", terminal)
	}
	other, _, unclaimed := graphExecutionPGFixture(t, db)
	if _, err := repo.CancelExecution(ctx, other.ID, other.GraphID, other.TenantID, 7, unclaimed.ExecutionID, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := repo.ClaimBuild(ctx, *unclaimed, "too-late", time.Minute); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("cancelled candidate claimed: %v", err)
	}
}

func TestIntegrationPostgresGraphWaitForAdmissionPastLease(t *testing.T) {
	db := graphExecutionPG(t)
	for _, action := range []string{"material", "review", "progress", "terminal"} {
		t.Run(action, func(t *testing.T) {
			task, mat, e := graphExecutionPGFixture(t, db)
			repo := NewBuildRepository(db)
			ctx := graphRepositoryUserContext(task.TenantID)
			_, lease, _, _, err := repo.ClaimBuild(ctx, *e, "waiting-backend", time.Minute)
			if err != nil || lease == nil {
				t.Fatalf("claim before lock wait: %v lease=%+v", err, lease)
			}
			if err = db.Model(&execution.TaskExecution{}).Where("execution_id = ?", e.ExecutionID).Update("lease_expires_at", gorm.Expr("clock_timestamp() + interval '700 milliseconds'")).Error; err != nil {
				t.Fatal(err)
			}
			holder := db.Begin()
			if holder.Error != nil {
				t.Fatal(holder.Error)
			}
			defer holder.Rollback()
			if err = LockAdmission(holder, task.TenantID, true); err != nil {
				t.Fatal(err)
			}
			waitCtx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			result := make(chan error, 1)
			pidCh := make(chan int, 1)
			go func() {
				result <- db.WithContext(waitCtx).Connection(func(conn *gorm.DB) error {
					var pid int
					if err := conn.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					pidCh <- pid
					owned := execution.ContextWithLease(waitCtx, *lease)
					worker := NewBuildRepository(conn)
					switch action {
					case "material":
						mat.ProcessedChunks = 99
						return worker.UpdateMaterial(owned, &mat)
					case "review":
						return worker.CreateReviewItem(owned, &models.ReviewItem{TenantID: task.TenantID, GraphID: task.GraphID, TaskID: task.ID, MaterialID: mat.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{}`), Status: models.ReviewStatusPending})
					case "progress":
						return worker.ProgressBuild(owned, 99, nil)
					default:
						return worker.CompleteBuild(owned, &task, execution.ExecutionStatusSuccess, "", nil, nil)
					}
				})
			}()
			var pid int
			select {
			case pid = <-pidCh:
			case <-waitCtx.Done():
				t.Fatal(waitCtx.Err())
			}
			graphWaitPG(t, waitCtx, db, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock')", pid)
			graphWaitPG(t, waitCtx, db, "SELECT clock_timestamp() >= lease_expires_at FROM common.task_executions WHERE execution_id = ?", e.ExecutionID)
			if err = holder.Commit().Error; err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-result:
			case <-waitCtx.Done():
				t.Fatal(waitCtx.Err())
			}
			if !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("expired private %s write=%v", action, err)
			}
			var stored models.BuildTask
			var material models.BuildMaterial
			var current execution.TaskExecution
			var count int64
			if err = db.First(&stored, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err = db.First(&material, mat.ID).Error; err != nil {
				t.Fatal(err)
			}
			if err = db.Where("execution_id = ?", e.ExecutionID).First(&current).Error; err != nil {
				t.Fatal(err)
			}
			if err = db.Model(&models.ReviewItem{}).Where("task_id = ? AND tenant_id = ?", task.ID, task.TenantID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != models.BuildStatusRunning || stored.CompletedAt != nil || material.ProcessedChunks != 0 || count != 0 || current.Progress != 0 || current.Status != execution.ExecutionStatusRunning {
				t.Fatalf("private write escaped expired lease: %s", action)
			}
		})
	}
}
func graphWaitPG(t *testing.T, ctx context.Context, db *gorm.DB, query string, args ...interface{}) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var ok bool
		if err := db.WithContext(ctx).Raw(query, args...).Scan(&ok).Error; err != nil {
			t.Fatal(err)
		}
		if ok {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestIntegrationPostgresGraphUncertainEffects(t *testing.T) {
	db := graphExecutionPG(t)
	task, mat, e := graphExecutionPGFixture(t, db)
	repo := NewBuildRepository(db)
	ctx := graphRepositoryUserContext(task.TenantID)
	_, lease, _, _, err := repo.ClaimBuild(ctx, *e, "old-backend", time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("claim=%v %+v", err, lease)
	}
	owned := execution.ContextWithLease(ctx, *lease)
	var kg models.KnowledgeGraph
	if err = db.First(&kg, task.GraphID).Error; err != nil {
		t.Fatal(err)
	}
	descriptor, err := json.Marshal(models.GraphOperationTarget{EngineID: kg.EngineID, EngineVersion: 1, CatalogPath: kg.CatalogPath, IndexManagement: kg.IndexManagement})
	if err != nil {
		t.Fatal(err)
	}
	targetHash := sha256.Sum256(descriptor)
	op := models.GraphOperation{ID: uuid.NewString(), TenantID: task.TenantID, GraphID: task.GraphID, ExecutionID: e.ExecutionID, Attempt: lease.Attempt, Kind: "graph", TargetDescriptor: string(descriptor), TargetDigest: hex.EncodeToString(targetHash[:]), RequestDigest: strings.Repeat("b", 64)}
	if err = repo.BeginBuildOperation(owned, &op); err != nil {
		t.Fatal(err)
	}
	if err = db.Model(&execution.TaskExecution{}).Where("execution_id = ?", e.ExecutionID).Update("lease_expires_at", gorm.Expr("clock_timestamp() - interval '1 second'")).Error; err != nil {
		t.Fatal(err)
	}
	if err = repo.FailExpiredBuilds(ctx); err != nil {
		t.Fatal(err)
	}
	wrong := *lease
	wrong.Token = uuid.NewString()
	if err = repo.AppendLateEvidence(execution.ContextWithLease(ctx, wrong), op, strings.Repeat("c", 64)); err == nil {
		t.Fatal("unbound old runtime evidence accepted")
	}
	if err = repo.AppendLateEvidence(owned, op, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if err = repo.SettleBuildOperation(owned, op, strings.Repeat("c", 64), true); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("old runtime settled current facts: %v", err)
	}
	if err = repo.DeleteMaterial(ctx, mat.ID, task.ID, task.GraphID, task.TenantID); !errors.Is(err, ErrGraphAdmissionClosed) {
		t.Fatalf("uncertain material removed: %v", err)
	}
	if _, _, err = enqueueRepositoryFixture(ctx, repo, task, newGraphRepositoryTestExecution(uuid.NewString(), int(task.TenantID), time.Time{}), BuildExecutionClaimRerun); !errors.Is(err, ErrGraphAdmissionClosed) {
		t.Fatalf("uncertain effects replayed: %v", err)
	}
	if err = db.Model(&models.KnowledgeGraph{}).Where("id = ?", kg.ID).Update("admission_closed", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err = NewKnowledgeGraphRepository(db).Reconcile(ctx, kg.ID, kg.TenantID, 7, kg.Version); !errors.Is(err, ErrGraphReconciliationIncomplete) {
		t.Fatalf("local return unlocked graph: %v", err)
	}
	var after models.GraphOperation
	if err = db.First(&after, "id = ?", op.ID).Error; err != nil {
		t.Fatal(err)
	}
	var terminal execution.TaskExecution
	if err = db.Where("execution_id = ?", e.ExecutionID).First(&terminal).Error; err != nil {
		t.Fatal(err)
	}
	if after.State != models.GraphOperationUncertain || after.LateEvidenceDigest != strings.Repeat("c", 64) || terminal.Status != execution.ExecutionStatusFailed || terminal.Attempt != 1 || terminal.LeaseToken != nil {
		t.Fatal("late evidence changed execution or operation authority")
	}
}
