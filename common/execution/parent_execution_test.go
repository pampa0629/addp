package execution_test

import (
	"context"
	"testing"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInheritOrchestratorActor(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:parent_execution_actor?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	principalID, membershipID, authorizationVersion := int64(41), int64(51), int64(6)
	parent := &commonExecution.TaskExecution{
		TenantID: 7, ExecutionID: "11111111-1111-4111-8111-111111111111",
		Module: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
	}
	if err := commonExecution.NewTaskExecutionRepository(db).Create(context.Background(), parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}

	parentID := parent.ExecutionID
	child := &commonExecution.TaskExecution{TenantID: 7, Source: commonExecution.ModuleOrchestrator, ParentExecutionID: &parentID}
	if err := db.Transaction(func(tx *gorm.DB) error { return commonExecution.InheritOrchestratorActor(tx, child) }); err != nil {
		t.Fatalf("inherit actor: %v", err)
	}
	if child.ActorPrincipalID == nil || *child.ActorPrincipalID != principalID ||
		child.ActorTenantMembershipID == nil || *child.ActorTenantMembershipID != membershipID ||
		child.IssuedAuthorizationVersion == nil || *child.IssuedAuthorizationVersion != authorizationVersion ||
		child.TriggeredBy == nil || *child.TriggeredBy != int(principalID) {
		t.Fatalf("child actor facts were not inherited: %#v", child)
	}
}

func TestInheritOrchestratorActorRejectsCrossTenantParent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:parent_execution_cross_tenant?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure execution store: %v", err)
	}
	principalID, membershipID, authorizationVersion := int64(41), int64(51), int64(6)
	parent := &commonExecution.TaskExecution{
		TenantID: 8, ExecutionID: "22222222-2222-4222-8222-222222222222",
		Module: commonExecution.ModuleOrchestrator, TaskType: commonExecution.TaskTypeOrchestration, Source: commonExecution.ModuleOrchestrator,
		Status: commonExecution.ExecutionStatusRunning, TriggerType: commonExecution.TriggerTypeManual,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
	}
	if err := commonExecution.NewTaskExecutionRepository(db).Create(context.Background(), parent); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	parentID := parent.ExecutionID
	child := &commonExecution.TaskExecution{TenantID: 7, Source: commonExecution.ModuleOrchestrator, ParentExecutionID: &parentID}
	if err := db.Transaction(func(tx *gorm.DB) error { return commonExecution.InheritOrchestratorActor(tx, child) }); err == nil {
		t.Fatal("cross-tenant parent inheritance unexpectedly succeeded")
	}
}

func TestNormalizeOrchestratorChildContext(t *testing.T) {
	const parentID = "33333333-3333-4333-8333-333333333333"
	got, err := commonExecution.NormalizeOrchestratorChildContext(commonExecution.ModuleOrchestrator, " "+parentID+" ")
	if err != nil || got != parentID {
		t.Fatalf("normalized parent = %q error = %v", got, err)
	}
	for _, testCase := range []struct{ source, parent string }{
		{"", parentID},
		{commonExecution.ModuleMeta, parentID},
		{commonExecution.ModuleOrchestrator, ""},
		{commonExecution.ModuleOrchestrator, "not-a-uuid"},
	} {
		if _, err := commonExecution.NormalizeOrchestratorChildContext(testCase.source, testCase.parent); err == nil {
			t.Fatalf("source=%q parent=%q unexpectedly accepted", testCase.source, testCase.parent)
		}
	}
}
