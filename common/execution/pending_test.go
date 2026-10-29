package execution_test

import (
	"errors"
	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"testing"
	"time"
)

func TestPendingAuthorizationRequiresImmutableUnclaimedIdentity(t *testing.T) {
	db := newLeaseTestDB(t)
	actor := int64(31)
	item := execution.TaskExecution{ExecutionID: "pending-authorized", TenantID: 7, Module: execution.ModuleQuality, TaskType: execution.TaskTypeQualityPlan, Source: execution.ModuleQuality, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, ActorPrincipalID: &actor, ActorTenantMembershipID: &actor, IssuedAuthorizationVersion: &actor}
	if err := execution.CreatePending(t.Context(), db, &item); err != nil {
		t.Fatal(err)
	}
	id := execution.PendingIdentity{ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source}
	wrong := id
	wrong.TenantID++
	if err := execution.AttachPendingAuthorization(t.Context(), db, wrong, execution.PendingAuthorization{ID: 51, ExpiresAt: time.Now().Add(time.Hour), ActorPrincipalID: actor, ActorTenantMembershipID: actor, IssuedAuthorizationVersion: actor}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("wrong tenant authorization=%v", err)
	}
	if err := execution.AttachPendingAuthorization(t.Context(), db, id, execution.PendingAuthorization{ID: 51, ExpiresAt: time.Now().Add(-time.Second), ActorPrincipalID: actor, ActorTenantMembershipID: actor, IssuedAuthorizationVersion: actor}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired authorization=%v", err)
	}
	if err := execution.AttachPendingAuthorization(t.Context(), db, id, execution.PendingAuthorization{ID: 51, ExpiresAt: time.Now().Add(time.Hour), ActorPrincipalID: actor, ActorTenantMembershipID: actor, IssuedAuthorizationVersion: actor}); err != nil {
		t.Fatal(err)
	}
	if err := execution.AttachPendingAuthorization(t.Context(), db, id, execution.PendingAuthorization{ID: 52, ExpiresAt: time.Now().Add(time.Hour), ActorPrincipalID: actor, ActorTenantMembershipID: actor, IssuedAuthorizationVersion: actor}); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("authorization replacement=%v", err)
	}
	if _, _, err := execution.ClaimPending(t.Context(), db, id, execution.ClaimOptions{Module: item.Module, TaskType: item.TaskType, WorkerID: "worker", LeaseDuration: time.Minute}, nil); err != nil {
		t.Fatal(err)
	}
	if err := execution.FailPending(t.Context(), db, id, nil); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("pending failure changed claimed row=%v", err)
	}
}

func TestUnleasedRecoveryCannotTerminateCurrentLease(t *testing.T) {
	db := newLeaseTestDB(t)
	item := execution.TaskExecution{ExecutionID: "unleased-recovery", TenantID: 7, Module: execution.ModuleDevelop, TaskType: execution.TaskTypeQuery, Source: execution.ModuleDevelop, Status: execution.ExecutionStatusPending, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual}
	if err := execution.CreatePending(t.Context(), db, &item); err != nil {
		t.Fatal(err)
	}
	_, lease, err := execution.ClaimNext(t.Context(), db, execution.ClaimOptions{Module: item.Module, TaskType: item.TaskType, WorkerID: "worker", LeaseDuration: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	fail := func() error {
		return execution.FailUnleased(t.Context(), db, item.ExecutionID, item.TenantID, item.Module, item.TaskType, nil)
	}
	if err := fail(); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("valid lease closed as unleased=%v", err)
	}
	if err := db.Model(&item).Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	if err := fail(); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired complete lease closed as unleased=%v", err)
	}
	if err := db.Model(&item).Update("lease_token", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := fail(); err != nil {
		t.Fatal(err)
	}
	var stored execution.TaskExecution
	if err := db.Where("execution_id = ?", item.ExecutionID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != execution.ExecutionStatusFailed || stored.Attempt != lease.Attempt || stored.StartedAt == nil || stored.CompletedAt == nil || stored.ExecutionTimeMs == nil || *stored.ExecutionTimeMs != stored.CompletedAt.Sub(*stored.StartedAt).Milliseconds() || stored.LeaseOwner != nil || stored.LeaseToken != nil || stored.LeaseExpiresAt != nil {
		t.Fatal("unleased closure lost persisted attempt facts")
	}
	if err := fail(); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("terminal rewritten=%v", err)
	}
}
