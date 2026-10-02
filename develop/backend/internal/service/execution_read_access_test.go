package service

import (
	"context"
	"github.com/addp/common/authorization/authtest"
	commonExecution "github.com/addp/common/execution"
	"testing"
)

func TestScriptExecutionOriginComesOnlyFromVerifiedCurrentUser(t *testing.T) {
	userID := 9
	execution := &commonExecution.TaskExecution{TenantID: 7, TaskType: "script", TriggeredBy: &userID}
	applyScriptExecutionActor(context.Background(), execution)
	if execution.ActorPrincipalID != nil {
		t.Fatal("invented actor without auth context")
	}
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"develop.task.execute", "develop.notebook.execute"})
	ctx := WithExecutionAuthContext(t.Context(), facts)
	facts.Authorization.AuthorizationVersion = "2"
	applyScriptExecutionActor(ctx, execution)
	if execution.ActorPrincipalID == nil || *execution.ActorPrincipalID != 9 || execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID != 1 || execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion != 1 {
		t.Fatalf("wrong frozen origin: %#v", execution)
	}
	other := &commonExecution.TaskExecution{TenantID: 8, TaskType: "script", TriggeredBy: &userID}
	applyScriptExecutionActor(ctx, other)
	if other.ActorPrincipalID != nil {
		t.Fatal("used user facts across tenant")
	}
}
