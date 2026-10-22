package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanresource"
	"github.com/addp/meta/internal/scantask"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type collectionClientFunc func(context.Context, uint, execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error)

func (f collectionClientFunc) CheckMetaCollection(ctx context.Context, tenant uint, r execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
	return f(ctx, tenant, r)
}

func TestGovernanceBasicChecksCurrentQualificationOnEveryRead(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	task := models.ScanTask{TenantID: 7, EngineID: 9, Name: "basic task", Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic", "force": false}}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	run := scantask.NewTaskExecution(&task, 33, "postgresql", "manual", "meta", nil, time.Now())
	run.Status = "running"
	run.Attempt = 1
	run.ExecutionConfig["engine_version"] = "3"
	run.ExecutionConfig["engine_type"] = "postgresql"
	if err := freezeScanTaskDefinition(db, run); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	lease := execution.Lease{ExecutionID: run.ExecutionID, TenantID: 7, Attempt: 1, Token: uuid.NewString()}
	s := NewScanExecutionService(db, nil, nil, nil)
	calls := 0
	var rejected error
	s.SetCollectionAuthorizationClient(collectionClientFunc(func(_ context.Context, tenant uint, input execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
		calls++
		if tenant != 7 || input.ExecutionID != run.ExecutionID || input.LeaseToken != lease.Token || input.Attempt != 1 || input.Validate() != nil {
			t.Fatal("wrong current binding")
		}
		if rejected != nil {
			return nil, rejected
		}
		return &execution.MetaCollectionAccessObservation{ExecutionID: input.ExecutionID, ExecutionPlanDigest: input.ExecutionPlanDigest, QualificationID: uuid.NewString(), ObservedAt: time.Now()}, nil
	}))
	ctx, err := s.bindCollectionRead(context.Background(), run, lease)
	if err != nil || calls != 1 {
		t.Fatal("first check", err, calls)
	}
	tenant := uint(7)
	resource := &commonmodels.Engine{ID: 9, TenantID: &tenant, Version: 3, EngineType: "postgresql"}
	if err := scanresource.CheckCollectionRead(ctx, resource); err != nil || calls != 2 {
		t.Fatal("subsequent read reused old allow", err, calls)
	}
	rejected = commonapi.ErrForbidden
	if err := scanresource.CheckCollectionRead(ctx, resource); !errors.Is(err, commonapi.ErrForbidden) || calls != 3 {
		t.Fatal("revoked qualification reused", err, calls)
	}
	rejected = nil
	resource.Version = 4
	if err := scanresource.CheckCollectionRead(ctx, resource); !errors.Is(err, commonapi.ErrForbidden) || calls != 3 {
		t.Fatal("actual Engine differs from frozen plan", err, calls)
	}
	resource.Version = 3
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		rejected = &client.TenantAPIError{Method: "POST", Path: "/runtime/meta_collection_checks", StatusCode: status}
		err := scanresource.CheckCollectionRead(ctx, resource)
		if (status == http.StatusForbidden) != errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("HTTP %d lost its exact denial/dependency classification: %v", status, err)
		}
		var remote *client.TenantAPIError
		if !errors.As(err, &remote) || remote.StatusCode != status {
			t.Fatal("typed upstream evidence was lost", err)
		}
	}
	rejected = nil
	if err := db.Model(&task).Update("parameters", models.JSONMap{"scan_depth": "deep", "force": false}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.bindCollectionRead(context.Background(), run, lease); !errors.Is(err, commonapi.ErrForbidden) || calls != 5 {
		t.Fatal("changed task consumed old approval", err, calls)
	}
	if taskID := strconv.FormatUint(uint64(task.ID), 10); *run.SourceTaskID != taskID {
		t.Fatal("fixture lost task identity")
	}
}

func exerciseCollectionDeniedAttempt(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, scenario := range []struct {
		name                                               string
		unavailable, deletedBeforeClaim, deletedAfterClaim bool
	}{
		{name: "current qualification denied"},
		{name: "dependency unavailable", unavailable: true},
		{name: "task deleted while queued", deletedBeforeClaim: true},
		{name: "task deleted while running", deletedAfterClaim: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			task := models.ScanTask{TenantID: 7, EngineID: 9, Name: "denied before source", Scope: models.JSONMap{"type": "engine"}, Parameters: models.JSONMap{"scan_depth": "basic", "force": false}}
			if err := db.Create(&task).Error; err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			run := scantask.NewTaskExecution(&task, 33, "postgresql", "manual", "meta", nil, now)
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
			if scenario.deletedBeforeClaim {
				if err := db.Delete(&task).Error; err != nil {
					t.Fatal(err)
				}
			}
			claimed, lease, err := s.ClaimNextBoundedExecution(context.Background(), "collection-fixture", time.Now().UTC(), time.Minute)
			if err != nil || claimed == nil || lease == nil || claimed.ExecutionID != run.ExecutionID {
				t.Fatalf("claim=%+v %v", claimed, err)
			}
			s.BindBoundedLease(run.ExecutionID, *lease)
			defer s.UnbindBoundedLease(run.ExecutionID)
			if scenario.deletedAfterClaim {
				if err := db.Delete(&task).Error; err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			if !scenario.unavailable {
				s.SetCollectionAuthorizationClient(collectionClientFunc(func(context.Context, uint, execution.MetaCollectionAccessRequest) (*execution.MetaCollectionAccessObservation, error) {
					calls++
					return nil, commonapi.ErrForbidden
				}))
			}
			// No EngineService / Provider is installed: reaching source dispatch
			// would fail this regression instead of quietly performing a scan.
			if err := s.ExecuteScanRun(context.Background(), run.ExecutionID); err == nil {
				t.Fatal("denied execution succeeded")
			}
			var current execution.TaskExecution
			if err := db.Where("execution_id=?", run.ExecutionID).Take(&current).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != "failed" || current.CompletedAt == nil || current.ErrorDetails["message"] == nil {
				t.Fatalf("denial left nonterminal execution: %+v", current)
			}
			if err := db.Unscoped().First(&task, task.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !scenario.deletedBeforeClaim && !scenario.deletedAfterClaim && (task.LastExecutionStatus == nil || *task.LastExecutionStatus != "failed") {
				t.Fatalf("summary did not fail: %+v %v", task, err)
			}
			if (!scenario.unavailable && !scenario.deletedBeforeClaim && !scenario.deletedAfterClaim && calls != 1) || ((scenario.unavailable || scenario.deletedBeforeClaim || scenario.deletedAfterClaim) && calls != 0) {
				t.Fatal("unexpected authorization calls", calls)
			}
			var nodes, items int64
			if err := db.Model(&models.MetaNode{}).Count(&nodes).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&models.MetaItem{}).Count(&items).Error; err != nil {
				t.Fatal(err)
			}
			if nodes != 0 || items != 0 {
				t.Fatal("denied attempt changed source metadata")
			}
		})
	}
}

func TestCollectionDenialCompletesAttemptBeforeSourceAccess(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	createTaskExecutionTable(t, db)
	createScanTaskTable(t, db)
	exerciseCollectionDeniedAttempt(t, db)
}
