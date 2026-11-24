package repository

import (
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/google/uuid"
)

func TestIntegrationMySQLManagerQueue(t *testing.T) {
	db := managerMySQLStoreDB(t, "common", "ADDP_TEST_MYSQL_MANAGER_QUEUE")
	ctx := t.Context()
	now := time.Now().UTC()
	pending := func(taskType string) execution.TaskExecution {
		return execution.TaskExecution{
			ExecutionID: uuid.NewString(), TenantID: 42, Module: execution.ModuleManager,
			TaskType: taskType, Source: execution.ModuleManager, TriggerType: execution.TriggerTypeManual,
			ExecutionBoundary: execution.ExecutionBoundaryBounded, Status: execution.ExecutionStatusPending,
		}
	}
	filePolicy := managerFileSourcePolicies[0]
	file := pending(filePolicy.taskType)
	file.ExecutionConfig = models.JSONMap{"config_version": filePolicy.configVersion}
	if err := execution.CreatePending(ctx, db, &file); err != nil {
		t.Fatal(err)
	}
	profile := pending(execution.TaskTypeDataProfiling)
	principal, membership, version, authID := int64(11), int64(17), int64(3), int64(73)
	user := int(principal)
	profile.TriggeredBy = &user
	profile.ActorPrincipalID, profile.ActorTenantMembershipID, profile.IssuedAuthorizationVersion = &principal, &membership, &version
	expiry := now.Add(time.Hour)
	profile.ExecutionAuthorizationID, profile.AuthorizationExpiresAt = &authID, &expiry
	if err := execution.CreatePending(ctx, db, &profile); err != nil {
		t.Fatal(err)
	}
	queue := NewBoundedExecutionQueueRepository(db)
	taskTypes := []string{file.TaskType, profile.TaskType}
	item, lease, err := queue.ClaimNext(ctx, taskTypes, "manager-mysql-gate", now, time.Minute)
	if err != nil || item == nil || lease == nil || item.ExecutionID != profile.ExecutionID {
		t.Fatalf("claim profile behind unbound file: %+v, %v", item, err)
	}
	if err := execution.CompleteWithEvent(ctx, db, *lease, execution.ExecutionStatusSuccess, nil); err != nil {
		t.Fatal(err)
	}
	repo := execution.NewTaskExecutionRepository(db)
	page, err := repo.ListEvents(ctx, profile.ExecutionID, profile.TenantID, 0, 5)
	if err != nil || len(page.Items) != 2 || page.Items[0].Kind != "started" || page.Items[1].Kind != "completed" {
		t.Fatalf("Manager execution events: %+v, %v", page, err)
	}
	item, _, err = queue.ClaimNext(ctx, taskTypes, "manager-mysql-gate", now, time.Minute)
	if err != nil || item != nil {
		t.Fatalf("unbound file claimed: %+v, %v", item, err)
	}
	recovered, err := queue.RecoverUnleased(ctx, ManagerBoundedTaskTypes(), now.Add(3*time.Minute), 10)
	if err != nil || recovered != 1 {
		t.Fatalf("recover expired preparation: %d, %v", recovered, err)
	}
	stored, err := repo.GetByExecutionID(ctx, file.ExecutionID, file.TenantID)
	if err != nil || stored.Status != execution.ExecutionStatusFailed || stored.Attempt != 0 || stored.StartedAt != nil {
		t.Fatalf("file execution after preparation expired: %+v, %v", stored, err)
	}
}
