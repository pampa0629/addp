package repository

import (
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestExecutionRepositoryRejectsForeignLeasesAndReceipts(t *testing.T) {
	for _, kind := range []string{"foreign_module", "foreign_type", "query_receipt", "expired_receipt", "workflow_receipt"} {
		t.Run(kind, func(t *testing.T) {
			db := newQueryExecutionTestDB(t)
			item := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "workflow", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", CreatedAt: time.Now(), UpdatedAt: time.Now(), ExecutionConfig: commonModels.JSONMap{}}
			if kind == "query_receipt" {
				item.TaskType = "query"
			}
			if err := db.Create(item).Error; err != nil {
				t.Fatal(err)
			}
			repo := NewExecutionRepository(db)
			claimed, lease, err := repo.ClaimNext(item.TaskType, t.Context(), "owner", time.Now(), time.Minute, nil)
			if err != nil || lease == nil {
				t.Fatalf("claim: %v", err)
			}
			switch kind {
			case "foreign_module":
				db.Model(item).Update("module", "meta")
			case "foreign_type":
				db.Model(item).Update("task_type", "scan")
			case "expired_receipt":
				db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute))
			}
			err = repo.RecordWorkflowReceipt(t.Context(), *lease, "remote-private-sentinel")
			if kind == "workflow_receipt" {
				if err != nil {
					t.Fatal(err)
				}
				var stored commonExecution.TaskExecution
				if err := db.First(&stored, item.ID).Error; err != nil || stored.Metadata["runtime_execution_id"] != "remote-private-sentinel" {
					t.Fatalf("receipt missing: %v", err)
				}
				var count int64
				db.Model(&commonExecution.Event{}).Where("execution_id = ?", item.ExecutionID).Count(&count)
				if count != 1 {
					t.Fatal("receipt created a public event")
				}
			} else if err == nil {
				t.Fatal("foreign/expired/non-workflow receipt accepted")
			}
			if kind == "foreign_module" || kind == "foreign_type" {
				if err := repo.UpdateProgressWithLease(t.Context(), *lease, 30, "private"); err == nil {
					t.Fatal("foreign lease updated progress")
				}
				if err := repo.Renew(t.Context(), *lease, time.Now().Add(time.Minute)); err == nil {
					t.Fatal("foreign lease renewed")
				}
				if err := repo.AttachAuthorizationWithLease(t.Context(), *lease, 9, time.Now().Add(time.Minute)); err == nil {
					t.Fatal("foreign lease attached authorization")
				}
				if err := repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now(), nil); err == nil {
					t.Fatal("foreign lease completed")
				}
			}
		})
	}
}
