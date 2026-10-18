package service

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	plugin "github.com/addp/common/engine/plugin"
	execution "github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
)

func TestProductionReceiptRequiresOriginalSuccessfulCommittedExecution(t *testing.T) {
	db := newExecutionServiceTestDB(t)
	now := time.Now().UTC()
	id, retry := uuid.NewString(), uuid.NewString()
	by := 31
	principal, member, version := int64(31), int64(33), int64(5)
	record := models.TargetProduction{SchemaVersion: models.TargetProductionSchema, TenantID: 7, ExecutionID: id, Attempt: 2, RetryOfExecutionID: &retry, TargetLocator: "addp://engine/2/path/outdoor/persons?type=table", Phase: models.TargetProductionCommitted, Preparation: plugin.TableTargetRecreated, StartedAt: now, PreparedAt: &now, CommittedAt: &now}
	item := execution.TaskExecution{TenantID: 7, ExecutionID: id, Module: "transfer", TaskType: "sync", Source: "transfer", TriggerType: "manual", ExecutionBoundary: "bounded", Status: "success", Attempt: 2, RetryOfExecutionID: &retry, TriggeredBy: &by, ActorPrincipalID: &principal, ActorTenantMembershipID: &member, IssuedAuthorizationVersion: &version, Metadata: commonmodels.JSONMap{models.TargetProductionMetadataKey: record, "private": "secret"}}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewExecutionService(db, execution.NewTaskExecutionRepository(db))
	result, err := svc.ReadProductionReceipt(t.Context(), 7, id)
	if err != nil || result.UserOrigin == nil || result.UserOrigin.PrincipalID != 31 || result.Preparation != plugin.TableTargetRecreated || result.RetryOfExecutionID == nil || *result.RetryOfExecutionID != retry {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if _, err := svc.ReadProductionReceipt(t.Context(), 8, id); err == nil {
		t.Fatal("cross tenant receipt returned")
	}
	for _, change := range []map[string]interface{}{{"status": "running"}, {"status": "failed"}, {"attempt": 3}, {"actor_tenant_membership_id": nil}} {
		if err := db.Model(&item).Updates(change).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ReadProductionReceipt(t.Context(), 7, id); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("incomplete/stale receipt accepted: %v", err)
		}
		if err := db.Model(&item).Updates(map[string]interface{}{"status": "success", "attempt": 2, "actor_tenant_membership_id": member}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&item).Updates(map[string]interface{}{"actor_principal_id": nil, "actor_tenant_membership_id": nil, "issued_authorization_version": nil}).Error; err != nil {
		t.Fatal(err)
	}
	result, err = svc.ReadProductionReceipt(t.Context(), 7, id)
	if err != nil || result.UserOrigin != nil {
		t.Fatalf("triggered_by fabricated user authority: %+v %v", result, err)
	}
}
