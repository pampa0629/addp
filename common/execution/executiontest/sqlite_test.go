package executiontest

import (
	"testing"
	"time"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/lib/pq"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureSQLiteStoreCreatesCanonicalTaskExecutionSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}

	if err := EnsureSQLiteStore(db); err != nil {
		t.Fatalf("ensure SQLite execution store: %v", err)
	}

	for _, column := range []string{
		"execution_boundary",
		"retry_of_execution_id",
		"lease_owner",
		"lease_token",
		"lease_expires_at",
		"attempt",
		"max_attempts",
		"actor_principal_id",
		"actor_tenant_membership_id",
		"issued_authorization_version",
		"execution_authorization_id",
		"authorization_effects",
		"authorization_expires_at",
	} {
		var count int
		if err := db.Raw("SELECT COUNT(*) FROM pragma_table_info('task_executions', 'common') WHERE name = ?", column).Scan(&count).Error; err != nil {
			t.Fatalf("inspect common.task_executions column %q: %v", column, err)
		}
		if count != 1 {
			t.Errorf("common.task_executions missing column %q", column)
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	leaseOwner := "manager-worker-1"
	principalID := int64(11)
	membershipID := int64(22)
	authorizationVersion := int64(33)
	authorizationID := int64(44)
	record := execution.TaskExecution{
		TenantID:                   7,
		ExecutionID:                "sqlite-execution-store-test",
		Module:                     execution.ModuleManager,
		TaskType:                   execution.TaskTypeDataProfiling,
		Source:                     execution.ModuleManager,
		Status:                     execution.ExecutionStatusPending,
		LeaseOwner:                 &leaseOwner,
		LeaseExpiresAt:             &now,
		Attempt:                    1,
		MaxAttempts:                3,
		TriggerType:                execution.TriggerTypeManual,
		ActorPrincipalID:           &principalID,
		ActorTenantMembershipID:    &membershipID,
		IssuedAuthorizationVersion: &authorizationVersion,
		ExecutionAuthorizationID:   &authorizationID,
		AuthorizationEffects:       pq.StringArray{"manager.data.read"},
		AuthorizationExpiresAt:     &now,
		ExecutionConfig:            models.JSONMap{"sample": true},
		ErrorDetails:               models.JSONMap{},
		Metadata:                   models.JSONMap{"source": "test"},
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := execution.NewTaskExecutionRepository(db).Create(t.Context(), &record); err != nil {
		t.Fatalf("insert canonical task execution: %v", err)
	}
}
