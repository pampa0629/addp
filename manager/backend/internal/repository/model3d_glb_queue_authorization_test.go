package repository

import (
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
)

func TestFTPModelGLBQueueRequiresBoundCurrentAuthorization(t *testing.T) {
	for _, name := range []string{"bound", "unbound", "expired", "scheduled", "foreign source", "legacy"} {
		t.Run(name, func(t *testing.T) {
			db := newTileCacheExecutionRepositoryTestDB(t)
			now := time.Now().UTC()
			repo, item := pendingGLBAuthorizationFixture(t, db, now.Add(-3*time.Minute))
			id := int64(51)
			expires := now.Add(time.Minute)
			config := commonModels.JSONMap{"config_version": models.Model3DGLBFileConfigVersion}
			fields := map[string]interface{}{"execution_config": config, "execution_authorization_id": id, "authorization_expires_at": expires}
			switch name {
			case "unbound":
				fields["execution_authorization_id"] = nil
				fields["authorization_expires_at"] = nil
			case "expired":
				fields["authorization_expires_at"] = now.Add(-time.Second)
			case "scheduled":
				fields["trigger_type"] = commonExecution.TriggerTypeScheduled
			case "foreign source":
				fields["source"] = commonExecution.ModuleOrchestrator
			case "legacy":
				fields["execution_config"] = commonModels.JSONMap{}
				fields["execution_authorization_id"] = nil
				fields["authorization_expires_at"] = nil
			}
			if err := db.Model(item).Updates(fields).Error; err != nil {
				t.Fatal(err)
			}
			queue := NewBoundedExecutionQueueRepository(db)
			claimed, lease, err := queue.ClaimNext(t.Context(), []string{commonExecution.TaskTypeModel3DGLBGeneration}, "queue-eligibility-only", now, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if name == "bound" || name == "legacy" {
				if claimed == nil || lease == nil {
					t.Fatal("eligible execution was not claimed")
				}
				return // A reference enables claim, never source I/O or an Allow.
			}
			if claimed != nil || lease != nil {
				t.Fatal("unprepared FTP execution was claimed")
			}
			stored, err := repo.GetExecution(t.Context(), 7, item.ExecutionID)
			if err != nil || stored.Status != commonExecution.ExecutionStatusPending || stored.Attempt != 0 {
				t.Fatalf("queue changed pending: %+v %v", stored, err)
			}
			if n, err := queue.RecoverUnleased(t.Context(), now, 100); err != nil || n != 1 {
				t.Fatalf("preparation recovery=%d %v", n, err)
			}
			stored, err = repo.GetExecution(t.Context(), 7, item.ExecutionID)
			if err != nil || stored.Status != commonExecution.ExecutionStatusFailed || stored.StartedAt != nil || stored.Attempt != 0 {
				t.Fatalf("recovery invented start: %+v %v", stored, err)
			}
			assertGLBPreparationOwnerStatus(t, db, item, commonExecution.ExecutionStatusFailed)
		})
	}
}
