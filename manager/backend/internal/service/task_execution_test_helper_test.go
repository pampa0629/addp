package service

import (
	"testing"

	commonExecution "github.com/addp/common/execution"
	"gorm.io/gorm"
)

func addTaskExecutionRuntimeColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, field := range []string{
		"LeaseOwner",
		"LeaseExpiresAt",
		"Attempt",
		"MaxAttempts",
		"ActorPrincipalID",
		"ActorTenantMembershipID",
		"IssuedAuthorizationVersion",
		"ExecutionAuthorizationID",
		"AuthorizationEffects",
		"AuthorizationExpiresAt",
	} {
		if err := db.Migrator().AddColumn(&commonExecution.TaskExecution{}, field); err != nil {
			t.Fatalf("add task_executions.%s: %v", field, err)
		}
	}
}
