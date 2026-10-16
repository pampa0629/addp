package service

import (
	"context"
	"errors"
	commonClient "github.com/addp/common/client"
	execution "github.com/addp/common/execution"
	"github.com/google/uuid"
	"testing"
)

type importSourceVerifierStub struct {
	calls  int
	mutate func(*commonClient.ManagerImportSource)
	err    error
}

func (v *importSourceVerifierStub) VerifyImportSource(_ context.Context, tenant uint, r commonClient.ManagerImportSourceRequest) (*commonClient.ManagerImportSource, error) {
	v.calls++
	result := &commonClient.ManagerImportSource{TenantID: tenant, UploaderID: r.PrincipalID, TaskID: r.TaskID, SourceLocator: r.SourceLocator}
	if v.mutate != nil {
		v.mutate(result)
	}
	return result, v.err
}
func TestImportSourceRequiresCurrentCompleteActorTaskAndLease(t *testing.T) {
	source := "addp-infra://minio/manager/tenant_7/import/date/upload/roads.shp?type=object"
	for _, kind := range []string{"valid", "missing_actor", "contradictory_actor", "tenant", "task", "status", "lease", "lease_owner", "attempt", "continuous", "owner_denied", "wrong_uploader", "wrong_source"} {
		t.Run(kind, func(t *testing.T) {
			principal, membership, version := int64(31), int64(41), int64(2)
			by := 31
			task := "9"
			token := "runtime-token"
			owner := "worker-1"
			current := &execution.TaskExecution{TenantID: 7, Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, ExecutionID: uuid.NewString(), SourceTaskID: &task, TriggeredBy: &by, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, Status: execution.ExecutionStatusRunning, Attempt: 1, LeaseToken: &token, LeaseOwner: &owner, ExecutionBoundary: execution.ExecutionBoundaryBounded}
			lease := execution.Lease{ExecutionID: current.ExecutionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner}
			verifier := &importSourceVerifierStub{}
			switch kind {
			case "missing_actor":
				current.ActorTenantMembershipID = nil
			case "contradictory_actor":
				by = 32
			case "tenant":
				lease.TenantID = 8
			case "task":
				current.SourceTaskID = nil
			case "status":
				current.Status = execution.ExecutionStatusSuccess
			case "lease":
				lease.Token = "stale"
			case "lease_owner":
				lease.Owner = "other-worker"
			case "attempt":
				lease.Attempt++
			case "continuous":
				current.ExecutionBoundary = execution.ExecutionBoundaryContinuous
			case "owner_denied":
				verifier.err = errors.New("denied")
			case "wrong_uploader":
				verifier.mutate = func(s *commonClient.ManagerImportSource) { s.UploaderID++ }
			case "wrong_source":
				verifier.mutate = func(s *commonClient.ManagerImportSource) { s.SourceLocator += "other" }
			}
			svc := &ExecutionEngineService{importSourceVerifier: verifier}
			err := svc.verifyInfraImportSource(execution.ContextWithLease(t.Context(), lease), current, source)
			if (kind == "valid") != (err == nil) {
				t.Fatalf("accepted=%v err=%v", err == nil, err)
			}
			if kind != "valid" && kind != "owner_denied" && kind != "wrong_uploader" && kind != "wrong_source" && verifier.calls != 0 {
				t.Fatal("invalid local origin reached Manager")
			}
		})
	}
}
