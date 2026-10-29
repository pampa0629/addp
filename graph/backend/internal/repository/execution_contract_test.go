package repository

import (
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

func TestGraphPendingInputAndAdmission(t *testing.T) {
	for _, reason := range []string{"input", "closed"} {
		t.Run(reason, func(t *testing.T) {
			db := newBuildRepositoryTestDB(t)
			repo := NewBuildRepository(db)
			task, mat := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
			e := newGraphRepositoryTestExecution("pending-"+reason, 7, time.Time{})
			ctx := graphRepositoryUserContext(7)
			if _, _, err := enqueueRepositoryFixture(ctx, repo, task, e, BuildExecutionClaimRun); err != nil {
				t.Fatal(err)
			}
			if err := repo.DeleteMaterial(ctx, mat.ID, task.ID, task.GraphID, 7); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("active material deleted: %v", err)
			}
			if err := repo.DeleteTask(ctx, task.ID, task.GraphID, 7); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("active task deleted: %v", err)
			}
			if reason == "input" {
				if err := db.Model(&models.BuildExecutionControl{}).Where("execution_id = ?", e.ExecutionID).Update("snapshot", []byte(`{}`)).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Model(&models.KnowledgeGraph{}).Where("id = ?", task.GraphID).Update("admission_closed", true).Error; err != nil {
					t.Fatal(err)
				}
			}
			claimed, lease, _, _, err := repo.ClaimBuild(ctx, *e, "fresh-backend", time.Minute)
			if err != nil || claimed != nil || lease != nil {
				t.Fatalf("unsafe pending claimed: %v %+v %+v", err, claimed, lease)
			}
			var stored execution.TaskExecution
			if err := db.Where("execution_id = ?", e.ExecutionID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Status != execution.ExecutionStatusFailed || stored.Attempt != 0 || stored.StartedAt != nil || stored.ExecutionTimeMs != nil || stored.LeaseToken != nil {
				t.Fatalf("rejection invented runtime: %+v", stored)
			}
			var operations int64
			db.Model(&models.GraphOperation{}).Count(&operations)
			if operations != 0 {
				t.Fatal("unclaimed execution created an operation")
			}
		})
	}
}

func TestGraphCancelAndVersionedReconciliation(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	graphs := NewKnowledgeGraphRepository(db)
	task, _ := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	ctx := graphRepositoryUserContext(7)
	e := newGraphRepositoryTestExecution("cancel-exact", 7, time.Time{})
	if _, _, err := enqueueRepositoryFixture(ctx, repo, task, e, BuildExecutionClaimRun); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CancelExecution(ctx, task.ID, task.GraphID, 7, 7, e.ExecutionID, 1); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("wrong attempt=%v", err)
	}
	if _, err := repo.CancelExecution(ctx, task.ID, task.GraphID, 8, 7, e.ExecutionID, 0); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross tenant=%v", err)
	}
	if _, err := graphs.UpdateDefinition(ctx, task.GraphID, 7, &models.UpdateKnowledgeGraphRequest{Version: 2, Name: "stale"}); !errors.Is(err, ErrGraphVersionConflict) {
		t.Fatalf("stale version=%v", err)
	}
	if err := db.Model(&models.KnowledgeGraph{}).Where("id = ?", task.GraphID).Update("admission_closed", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := graphs.Reconcile(ctx, task.GraphID, 7, 7, 1); !errors.Is(err, ErrGraphReconciliationIncomplete) {
		t.Fatalf("active graph reconciled=%v", err)
	}
	if _, err := graphs.UpdateDefinition(ctx, task.GraphID, 7, &models.UpdateKnowledgeGraphRequest{Version: 1, Name: "bypass"}); !errors.Is(err, ErrGraphAdmissionClosed) {
		t.Fatalf("closed graph edited=%v", err)
	}
	cancelled, err := repo.CancelExecution(ctx, task.ID, task.GraphID, 7, 7, e.ExecutionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != execution.ExecutionStatusCancelled || cancelled.StartedAt != nil || cancelled.Attempt != 0 {
		t.Fatalf("cancel=%+v", cancelled)
	}
	again, err := repo.CancelExecution(ctx, task.ID, task.GraphID, 7, 7, e.ExecutionID, 0)
	if err != nil || !again.CompletedAt.Equal(*cancelled.CompletedAt) {
		t.Fatalf("repeat cancel changed terminal: %v", err)
	}
	kg, err := graphs.Reconcile(ctx, task.GraphID, 7, 7, 1)
	if err != nil || kg.Version != 2 || !kg.AdmissionClosed {
		t.Fatalf("reconcile=%+v %v", kg, err)
	}
	if _, err = graphs.Reopen(ctx, task.GraphID, 7, 2); !errors.Is(err, ErrGraphPhysicalTargetUnverified) {
		t.Fatalf("unproved physical identity reopened: %v", err)
	}
	var preserved models.KnowledgeGraph
	db.First(&preserved, task.GraphID)
	if preserved.Version != 2 || !preserved.AdmissionClosed {
		t.Fatalf("failed reopen changed graph: %+v", preserved)
	}
}

func TestGraphReviewCommandSurvivesBuildAndBlocksDeletion(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	repo := NewBuildRepository(db)
	task, mat := createBuildRepositoryTestTask(t, db, 7, models.BuildStatusPending)
	ctx := graphRepositoryUserContext(7)
	e := newGraphRepositoryTestExecution("review-build", 7, time.Time{})
	if _, _, err := enqueueRepositoryFixture(ctx, repo, task, e, BuildExecutionClaimRun); err != nil {
		t.Fatal(err)
	}
	_, lease, runtime, _, err := repo.ClaimBuild(ctx, *e, "runtime", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CompleteBuild(execution.ContextWithLease(ctx, *lease), runtime, execution.ExecutionStatusSuccess, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	item := models.ReviewItem{TenantID: 7, GraphID: task.GraphID, TaskID: task.ID, MaterialID: mat.ID, ItemType: models.ReviewItemEntity, Content: []byte(`{}`), Status: models.ReviewStatusPending}
	if err = db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	first, err := repo.BeginReview(ctx, task.GraphID, item.ID, 7, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.BeginReview(ctx, task.GraphID, item.ID, 7, 8); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("second review admitted=%v", err)
	}
	if err = repo.DeleteTask(ctx, task.ID, task.GraphID, 7); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("review task deleted=%v", err)
	}
	next := newGraphRepositoryTestExecution("review-rerun", 7, time.Time{})
	if _, _, err = enqueueRepositoryFixture(ctx, repo, task, next, BuildExecutionClaimRerun); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("rerun erased active review=%v", err)
	}
	if err = repo.EndReview(ctx, first, models.ReviewStatusRejected, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err = enqueueRepositoryFixture(ctx, repo, task, next, BuildExecutionClaimRerun); err != nil {
		t.Fatal(err)
	}
	if next.ExecutionConfig["predecessor_execution_id"] != e.ExecutionID {
		t.Fatal("rerun lost predecessor")
	}
	var preserved models.ReviewItem
	if err = db.First(&preserved, item.ID).Error; err != nil || preserved.Status != models.ReviewStatusRejected {
		t.Fatalf("review erased: %+v %v", preserved, err)
	}
}

func TestGraphOperationRejectsIncompleteIdentity(t *testing.T) {
	for _, op := range []models.GraphOperation{{}, {ID: "op", TenantID: 7, GraphID: 1, Kind: "graph", TargetDigest: strings.Repeat("a", 64), RequestDigest: strings.Repeat("b", 64), TargetDescriptor: `{"engine_id":1}`}} {
		if err := validateOperation(op); err == nil {
			t.Fatal("unverifiable operation admitted")
		}
	}
}

func TestGraphSupervisorDoesNotAdoptLegacyExecutions(t *testing.T) {
	db := newBuildRepositoryTestDB(t)
	legacy := newGraphRepositoryTestExecution("legacy-unowned", 7, time.Time{})
	legacy.Status = execution.ExecutionStatusPending
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewBuildRepository(db)
	candidates, err := repo.PendingBuilds(t.Context(), 32)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("legacy adopted: %v %v", candidates, err)
	}
	before := time.Now().Add(-time.Minute)
	if err := db.Model(legacy).Updates(map[string]interface{}{"status": execution.ExecutionStatusRunning, "attempt": 1, "started_at": before, "lease_expires_at": before, "lease_owner": "legacy", "lease_token": "legacy-token"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.FailExpiredBuilds(t.Context()); err != nil {
		t.Fatal(err)
	}
	var stored execution.TaskExecution
	if err := db.Where("execution_id = ?", legacy.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != execution.ExecutionStatusRunning || stored.CompletedAt != nil {
		t.Fatal("legacy history was rewritten")
	}
}
