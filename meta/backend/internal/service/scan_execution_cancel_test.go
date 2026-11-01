package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
	"testing"
	"time"
)

func exerciseScanCancellation(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, running := range []bool{false, true} {
		name := "unclaimed"
		if running {
			name = "running"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			task := models.ScanTask{TenantID: 7, EngineID: 9, Name: "cancel precise execution", Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic", "force": false}, Enabled: true}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			run := scantask.NewTaskExecution(&task, 33, "postgresql", "scheduled", "meta", nil, time.Now())
			run.ExecutionConfig["engine_version"] = "3"
			run.ExecutionConfig["engine_type"] = "postgresql"
			if err := freezeScanTaskDefinition(db, run); err != nil {
				t.Fatal(err)
			}
			if err := db.Create(run).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&task).Updates(map[string]any{"last_execution_id": run.ExecutionID, "last_execution_status": "pending"}).Error; err != nil {
				t.Fatal(err)
			}
			s := NewScanExecutionService(db, nil, nil, nil)
			membership := int64(77)
			actor := ScanCancellationActor{PrincipalID: 76, MembershipID: &membership}
			allow := func(_ context.Context, engine uint) error {
				if engine != 9 {
					t.Fatal("wrong target engine")
				}
				return nil
			}
			// Permission denial and another tenant must leave the exact execution intact.
			if _, err := s.CancelPGCollection(ctx, 7, run.ExecutionID, actor, func(context.Context, uint) error { return commonapi.ErrForbidden }); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal(err)
			}
			if _, err := s.CancelPGCollection(ctx, 8, run.ExecutionID, actor, allow); !errors.Is(err, commonapi.ErrNotFound) {
				t.Fatal(err)
			}
			var clean execution.TaskExecution
			if err := db.Where("execution_id=?", run.ExecutionID).Take(&clean).Error; err != nil {
				t.Fatal(err)
			}
			if _, found := clean.Metadata[scanCancellationKey]; found || clean.Status != "pending" {
				t.Fatal("rejected cancellation mutated execution")
			}
			var readCtx context.Context
			checks := 0
			if running {
				claimed, lease, err := s.ClaimNextBoundedExecution(ctx, "cancel-fixture", time.Now(), time.Minute)
				if err != nil || claimed == nil || claimed.ExecutionID != run.ExecutionID {
					t.Fatalf("claim %v %v", claimed, err)
				}
				s.BindBoundedLease(run.ExecutionID, *lease)
				defer s.UnbindBoundedLease(run.ExecutionID)
				run = claimed
				s.SetCollectionAuthorizationClient(collectionClientFunc(func(_ context.Context, _ uint, r execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
					checks++
					return &execution.MetaCollectionAccessObservation{ExecutionID: r.ExecutionID, ExecutionPlanDigest: r.ExecutionPlanDigest, QualificationID: "fixture", ObservedAt: time.Now()}, nil
				}))
				readCtx, err = s.bindCollectionRead(ctx, run, *lease)
				if err != nil {
					t.Fatal(err)
				}
				tenant := uint(7)
				resource := &commonmodels.Engine{ID: 9, TenantID: &tenant, Version: 3, EngineType: "postgresql"}
				if err := scanresource.CheckCollectionRead(readCtx, resource); err != nil {
					t.Fatal(err)
				}
			}
			result, err := s.CancelPGCollection(ctx, 7, run.ExecutionID, actor, allow)
			if err != nil {
				t.Fatal(err)
			}
			intent := result.Metadata[scanCancellationKey]
			intentJSON, _ := json.Marshal(intent)
			if intent == nil {
				t.Fatal("missing cancellation intent")
			}
			if running {
				if result.Status != "running" || result.CompletedAt != nil {
					t.Fatal("cancel manufactured completion for live runner")
				}
				if readCtx.Err() != nil {
					t.Fatal("cancel interrupted admitted enumeration")
				}
				// Progress from an admitted enumeration must preserve the intent.
				s.UpdateExecutionProgress(run.ExecutionID, 7, map[string]any{"metadata": commonmodels.JSONMap{"items_scanned": 2}})
				tenant := uint(7)
				if err := scanresource.CheckCollectionRead(readCtx, &commonmodels.Engine{ID: 9, TenantID: &tenant, Version: 3, EngineType: "postgresql"}); !errors.Is(err, errScanCancellationRequested) {
					t.Fatalf("next enumeration not blocked: %v", err)
				}
				if checks != 2 {
					t.Fatal("cancelled enumeration reached System or source")
				}
				if err := s.completeBoundedExecution(ctx, run, "success", time.Now(), map[string]any{"metadata": commonmodels.JSONMap{"items_scanned": 2}}); err != nil {
					t.Fatal(err)
				}
			} else {
				if result.Status != "cancelled" || result.Attempt != 0 || result.StartedAt != nil || result.CompletedAt == nil {
					t.Fatal("unclaimed cancellation manufactured attempt")
				}
				claimed, _, err := s.ClaimNextBoundedExecution(ctx, "must-not-claim", time.Now(), time.Minute)
				if err != nil || claimed != nil {
					t.Fatal("cancelled execution claimed", err)
				}
			}
			final, err := s.CancelPGCollection(ctx, 7, run.ExecutionID, actor, allow)
			finalJSON, _ := json.Marshal(final.Metadata[scanCancellationKey])
			if err != nil || final.Status != "cancelled" || final.CompletedAt == nil || !bytes.Equal(finalJSON, intentJSON) {
				t.Fatalf("idempotent cancel %v %v", final, err)
			}
			var current models.ScanTask
			if err := db.First(&current, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !current.Enabled || current.LastExecutionStatus == nil || *current.LastExecutionStatus != "cancelled" {
				t.Fatal("cancel changed scheduling or lost summary")
			}
		})
	}
}

func TestScanCancellationStopsNextEnumerationWithoutInterruptingAdmittedWork(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	exerciseScanCancellation(t, db)
}
