package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func pendingGLBAuthorizationFixture(t *testing.T, db *gorm.DB, createdAt time.Time) (*Model3DGLBRepository, *commonExecution.TaskExecution) {
	t.Helper()
	task := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
	item := newManagerRepositoryTestExecution(uuid.NewString(), 7, commonExecution.TaskTypeModel3DGLBGeneration, createdAt)
	actor, principal, membership, version := 31, int64(31), int64(41), int64(1)
	item.TriggeredBy, item.ActorPrincipalID, item.ActorTenantMembershipID, item.IssuedAuthorizationVersion = &actor, &principal, &membership, &version
	item.ExecutionConfig = commonModels.JSONMap{"large_number": json.Number("9007199254740993")}
	repo := NewModel3DGLBRepository(db)
	snapshot, err := repo.TaskConfigSnapshot(t.Context(), task.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimExecution(t.Context(), task.ID, 7, item, false, snapshot); err != nil {
		t.Fatal(err)
	}
	return repo, item
}

func TestGLBUnboundPreparationPreservesRawConfigAndClosesOwnerAtomically(t *testing.T) {
	db := newTileCacheExecutionRepositoryTestDB(t)
	repo, item := pendingGLBAuthorizationFixture(t, db, time.Now())
	raw, err := repo.RawExecutionConfig(t.Context(), 7, item.ExecutionID)
	if err != nil || !strings.Contains(string(raw), "9007199254740993") {
		t.Fatalf("raw config=%s err=%v", raw, err)
	}
	if other, err := repo.GetExecution(t.Context(), 8, item.ExecutionID); err != nil || other != nil {
		t.Fatalf("cross tenant=%#v err=%v", other, err)
	}
	if err := repo.FailUnboundExecution(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetExecution(t.Context(), 7, item.ExecutionID)
	if err != nil || stored.Status != commonExecution.ExecutionStatusFailed || stored.CompletedAt == nil || stored.Attempt != 0 || stored.ExecutionAuthorizationID != nil || stored.ErrorDetails["code"] != "source_authorization_required" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	assertGLBPreparationOwnerStatus(t, db, item, commonExecution.ExecutionStatusFailed)
	if err := repo.FailUnboundExecution(t.Context(), item); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("terminal execution rewritten: %v", err)
	}
}

func TestGLBTaskSnapshotRetainsWholeConfigAndRejectsPreparationDrift(t *testing.T) {
	for _, change := range []string{"unchanged", "version", "source", "options"} {
		t.Run(change, func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			task := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
			config := commonModels.JSONMap{"source": commonModels.JSONMap{"item_locator": "source.ifc"}, "result": commonModels.JSONMap{"storage_ref": "managed.glb"}, "options": commonModels.JSONMap{"large_integer": json.Number("9007199254740993")}}
			if err := db.Model(&task).Update("config", config).Error; err != nil {
				t.Fatal(err)
			}
			repo := NewModel3DGLBRepository(db)
			snapshot, err := repo.TaskConfigSnapshot(t.Context(), task.ID, 7)
			if err != nil || !strings.Contains(string(snapshot.Config), "9007199254740993") {
				t.Fatalf("snapshot=%#v err=%v", snapshot, err)
			}
			if _, err := repo.TaskConfigSnapshot(t.Context(), task.ID, 8); !errors.Is(err, commonAPI.ErrNotFound) {
				t.Fatalf("other tenant snapshot: %v", err)
			}
			switch change {
			case "version":
				if err := db.Model(&task).Update("version", snapshot.Version+1).Error; err != nil {
					t.Fatal(err)
				}
			case "source":
				config["source"] = commonModels.JSONMap{"item_locator": "other.ifc"}
			case "options":
				config["options"] = commonModels.JSONMap{"large_integer": json.Number("9007199254740992")}
			}
			if change == "source" || change == "options" {
				if err := db.Model(&task).Update("config", config).Error; err != nil {
					t.Fatal(err)
				}
			}
			err = db.Transaction(func(tx *gorm.DB) error { return verifyModel3DGLBTaskSnapshot(t.Context(), tx, snapshot) })
			if change == "unchanged" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("changed snapshot accepted: %v", err)
			}
		})
	}
}

func TestGLBPendingCreationUsesOneVerifiedRawTaskSnapshot(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "frozen", true: "changed"}[changed], func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			task := createModel3DGLBExecutionRepositoryTestTask(t, db, 7)
			config := commonModels.JSONMap{"source": commonModels.JSONMap{"item_fingerprint": "original"}, "result": commonModels.JSONMap{"file_name": "output.glb"}, "options": commonModels.JSONMap{"integer": json.Number("9007199254740993")}}
			if err := db.Model(&task).Update("config", config).Error; err != nil {
				t.Fatal(err)
			}
			repo := NewModel3DGLBRepository(db)
			snapshot, err := repo.TaskConfigSnapshot(t.Context(), task.ID, 7)
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				config["source"] = commonModels.JSONMap{"item_fingerprint": "new"}
				if err := db.Model(&task).Update("config", config).Error; err != nil {
					t.Fatal(err)
				}
			}
			item := newManagerRepositoryTestExecution(uuid.NewString(), 7, commonExecution.TaskTypeModel3DGLBGeneration, time.Now())
			_, err = repo.ClaimExecution(t.Context(), task.ID, 7, item, false, snapshot)
			if changed {
				if !errors.Is(err, commonAPI.ErrConflict) {
					t.Fatalf("changed config accepted: %v", err)
				}
				var count int64
				if err := db.Model(&commonExecution.TaskExecution{}).Where("execution_id = ?", item.ExecutionID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("executions=%d err=%v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := repo.RawExecutionConfig(t.Context(), 7, item.ExecutionID)
			if err != nil || !strings.Contains(string(raw), "9007199254740993") || !strings.Contains(string(raw), "output.glb") || !strings.Contains(string(raw), "original") {
				t.Fatalf("raw frozen config=%s err=%v", raw, err)
			}
		})
	}
}

func TestGLBUnboundPreparationCannotCloseBoundClaimedOrChangedActor(t *testing.T) {
	for _, change := range []string{"bound", "claimed", "actor", "membership", "version", "triggered by", "task owner", "source", "trigger", "parent", "boundary"} {
		t.Run(change, func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			repo, item := pendingGLBAuthorizationFixture(t, db, time.Now())
			fields := map[string]interface{}{}
			switch change {
			case "bound":
				fields["execution_authorization_id"] = 51
				fields["authorization_expires_at"] = time.Now().Add(time.Minute)
			case "claimed":
				fields["attempt"] = 1
				fields["status"] = commonExecution.ExecutionStatusRunning
			case "actor":
				fields["actor_principal_id"] = 32
			case "membership":
				fields["actor_tenant_membership_id"] = 42
			case "version":
				fields["issued_authorization_version"] = 2
			case "triggered by":
				fields["triggered_by"] = 32
			case "task owner":
				fields["source_task_id"] = "99999"
			case "source":
				fields["source"] = commonExecution.ModuleOrchestrator
			case "trigger":
				fields["trigger_type"] = commonExecution.TriggerTypeScheduled
			case "parent":
				fields["parent_execution_id"] = uuid.NewString()
			case "boundary":
				fields["execution_boundary"] = "continuous"
			}
			if err := db.Model(&commonExecution.TaskExecution{}).Where("id = ?", item.ID).Updates(fields).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo.FailUnboundExecution(t.Context(), item); !errors.Is(err, commonAPI.ErrConflict) {
				t.Fatalf("changed preparation closed: %v", err)
			}
			assertGLBPreparationOwnerStatus(t, db, item, commonExecution.ExecutionStatusPending)
		})
	}
}

func assertGLBPreparationOwnerStatus(t *testing.T, db *gorm.DB, item *commonExecution.TaskExecution, want string) {
	t.Helper()
	id, err := commonExecution.ParseSourceTaskIDUint(item.SourceTaskID)
	if err != nil {
		t.Fatal(err)
	}
	var task models.Model3DGLBTask
	if err := db.First(&task, id).Error; err != nil {
		t.Fatal(err)
	}
	if task.LastExecutionStatus == nil || *task.LastExecutionStatus != want {
		t.Fatalf("task status=%#v want %s", task.LastExecutionStatus, want)
	}
	var count int64
	if err := db.Table("common.execution_events").Where("execution_id = ?", item.ExecutionID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("pending preparation events=%d err=%v", count, err)
	}
}
