package repository

import (
	"context"
	"encoding/json"
	"github.com/addp/quality/internal/testsupport"
	"reflect"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
)

func TestPlanExecutionClaimsBeforeDynamicAuthorization(t *testing.T) {
	db := newPlanRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	now := time.Now().UTC()
	task := models.QualityPlan{
		TenantID: 7, Code: "outdoor_gate", Name: "Outdoor gate", Version: 1,
		TableBindings: []byte(`[{"alias":"orders","locator":"addp://engine/12/path/public/table_3?type=table"}]`),
		Rules:         []byte(`{"schema_version":"addp.quality.plan-rules/v1","rules":[{"rule_key":"f3889a4a-1675-4623-b6e3-773f9125a04d","type":"not_null","severity":"error","params":{"table":"orders","column":"id"}}]}`),
		CreatedBy:     1, UpdatedBy: 1, CreatedAt: now, UpdatedAt: now,
	}
	testsupport.SeedPlanRules(t, db, &task)
	if err := repo.Create(context.Background(), &task); err != nil {
		t.Fatal(err)
	}
	parent := "dbad5a67-d6cd-4d63-b99b-7646d1c89ec5"
	principalID, membershipID, authorizationVersion := int64(21), int64(22), int64(23)
	parentExecution := &commonExecution.TaskExecution{
		ExecutionID: parent, TenantID: 7, Module: commonExecution.ModuleOrchestrator,
		TaskType: "workflow", Source: commonExecution.ModuleOrchestrator,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status:            commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
		CreatedAt:                  now, UpdatedAt: now,
	}
	if err := db.Create(parentExecution).Error; err != nil {
		t.Fatalf("create parent execution: %v", err)
	}
	execution := &commonExecution.TaskExecution{
		ExecutionID: "gate-execution-1", TenantID: 7, Module: commonExecution.ModuleQuality,
		TaskType: commonExecution.TaskTypeQualityPlan, Source: commonExecution.ModuleOrchestrator,
		ParentExecutionID: &parent, ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		MaxAttempts: 3, CreatedAt: now, UpdatedAt: now,
		ExecutionConfig: commonModels.JSONMap{"schema_version": "addp.quality.plan-execution-config/v2"},
	}
	if _, err := repo.CreateExecution(context.Background(), task.ID, task.TenantID, execution, models.PlanRunRequest{}); err != nil {
		t.Fatal(err)
	}
	var storedExecution commonExecution.TaskExecution
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatalf("load gate execution: %v", err)
	}
	if storedExecution.ActorPrincipalID == nil || *storedExecution.ActorPrincipalID != principalID ||
		storedExecution.ActorTenantMembershipID == nil || *storedExecution.ActorTenantMembershipID != membershipID ||
		storedExecution.IssuedAuthorizationVersion == nil || *storedExecution.IssuedAuthorizationVersion != authorizationVersion {
		t.Fatalf("gate execution authorization lineage = principal %v membership %v version %v",
			storedExecution.ActorPrincipalID, storedExecution.ActorTenantMembershipID, storedExecution.IssuedAuthorizationVersion)
	}
	var expectedRules interface{}
	if err := json.Unmarshal(task.Rules, &expectedRules); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(storedExecution.ExecutionConfig["rules"], expectedRules) || storedExecution.ExecutionConfig["task_version"] != float64(task.Version) {
		t.Fatalf("execution did not freeze the complete plan: %#v", storedExecution.ExecutionConfig)
	}
	claimed, claimedTask, err := repo.ClaimPendingExecution(context.Background(), "quality-worker", now.Add(time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimedTask == nil || claimed.ExecutionAuthorizationID != nil || claimed.Status != commonExecution.ExecutionStatusRunning {
		t.Fatalf("claimed execution/task = %#v / %#v", claimed, claimedTask)
	}
	lease, err := commonExecution.LeaseFromExecution(*claimed)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachExecutionAuthorization(context.Background(), lease, map[string]interface{}{"execution_authorization_id": int64(41)}); err != nil {
		t.Fatal(err)
	}
	// A derived authorization belongs to one attempt/lease, not the whole execution.
	if err := repo.RecoverExpiredExecutions(context.Background(), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	storedExecution = commonExecution.TaskExecution{}
	if err := db.Where("execution_id = ?", execution.ExecutionID).First(&storedExecution).Error; err != nil {
		t.Fatal(err)
	}
	if storedExecution.ExecutionAuthorizationID != nil || storedExecution.AuthorizationExpiresAt != nil || storedExecution.Status != commonExecution.ExecutionStatusPending {
		t.Fatalf("retry retained derived authorization: %#v", storedExecution)
	}
	retried, _, err := repo.ClaimPendingExecution(context.Background(), "replacement-worker", now.Add(3*time.Minute), time.Minute)
	if err != nil || retried == nil || retried.Attempt != 2 {
		t.Fatalf("retry claim: %#v, %v", retried, err)
	}
	if err := repo.AttachExecutionAuthorization(context.Background(), lease, map[string]interface{}{"execution_authorization_id": int64(42)}); err == nil {
		t.Fatal("expired attempt attached authorization")
	}
	lease, err = commonExecution.LeaseFromExecution(*retried)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachExecutionAuthorization(context.Background(), lease, map[string]interface{}{"execution_authorization_id": int64(43)}); err != nil {
		t.Fatal(err)
	}
	completedAt := now.Add(3*time.Minute + time.Second)
	if err := repo.CompleteExecutionWithLease(context.Background(), task.ID, task.TenantID, lease, commonExecution.ExecutionStatusSuccess, map[string]interface{}{"progress": 100}, completedAt); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Get(context.Background(), task.TenantID, task.ID)
	if err != nil || stored.LastExecutionStatus != commonExecution.ExecutionStatusSuccess {
		t.Fatalf("stored task = %#v, err=%v", stored, err)
	}
}
func TestPlanCompletionFencesIssuesAndRollsBackTogether(t *testing.T) {
	db := newPlanDeletionRepositoryTestDB(t)
	repo := NewPlanRepository(db)
	task := createPlanRepositoryTestTask(t, db, 7)
	now := time.Now().UTC()
	ctx := context.Background()
	execution := newQualityRepositoryTestExecution("issue-fence", 7, now)
	if _, err := repo.CreateExecution(ctx, task.ID, 7, execution, models.PlanRunRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachPendingAuthorization(ctx, 7, execution.ExecutionID, map[string]interface{}{"execution_authorization_id": int64(1)}); err != nil {
		t.Fatal(err)
	}
	running, _, err := repo.ClaimPendingExecution(ctx, "worker", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := commonExecution.LeaseFromExecution(*running)
	if err != nil {
		t.Fatal(err)
	}
	observation := models.IssueObservation{TargetKey: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",PlanID: task.ID, RuleKey: "00000000-0000-4000-8000-000000000001", RuleType: "not_null", Severity: "error", Table: "customers", SchemaName: "public", ColumnName: "id", EngineID: 1, FailedCount: 1, TotalCount: 2, PassRate: 50}
	stale := lease
	stale.Token = "other-worker"
	if err := repo.CompleteExecutionWithLease(ctx, task.ID, 7, stale, "failed", nil, now, observation); err == nil {
		t.Fatal("stale lease wrote terminal state")
	}
	var count int64
	db.Model(&models.Issue{}).Count(&count)
	if count != 0 {
		t.Fatal("stale worker wrote issue")
	}
	// Owner-summary failure happens after issue reconciliation. Both writes must roll back.
	if err := db.Exec(`CREATE TRIGGER quality.fail_summary BEFORE UPDATE ON plans BEGIN SELECT RAISE(FAIL, 'summary write failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecutionWithLease(ctx, task.ID, 7, lease, "failed", nil, now, observation); err == nil {
		t.Fatal("summary conflict accepted")
	}
	db.Model(&models.Issue{}).Count(&count)
	if count != 0 {
		t.Fatal("issue survived failed terminal transaction")
	}
	var stored commonExecution.TaskExecution
	if err := db.Where("execution_id=?", execution.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "running" {
		t.Fatalf("status=%s", stored.Status)
	}
	if err := db.Exec(`DROP TRIGGER quality.fail_summary`).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteExecutionWithLease(ctx, task.ID, 7, lease, "failed", nil, now, observation); err != nil {
		t.Fatal(err)
	}
	db.Model(&models.Issue{}).Count(&count)
	if count != 1 {
		t.Fatal("valid terminal completion did not reconcile issue")
	}
}
