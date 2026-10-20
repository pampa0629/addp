package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type glbSourceEngineFixture struct {
	engine     *commonModels.Engine
	err        error
	tenant, id uint
}

func (c *glbSourceEngineFixture) GetEngineForTenant(_ context.Context, tenant, id uint) (*commonModels.Engine, error) {
	c.tenant, c.id = tenant, id
	return c.engine, c.err
}

func TestGLBSourceClassificationUsesCurrentSystemFacts(t *testing.T) {
	for _, name := range []string{"ftp despite caller label", "nfs", "wrong engine", "wrong tenant", "nil ftp tenant", "offline", "unavailable", "locator mismatch", "noncanonical ftp", "space ftp", "empty type"} {
		t.Run(name, func(t *testing.T) {
			tenant := uint(7)
			engine := &commonModels.Engine{ID: 33, TenantID: &tenant, EngineType: "ftp", LifecycleState: "active", ConnectionStatus: "online"}
			reader := &glbSourceEngineFixture{engine: engine}
			snapshot := &repository.Model3DGLBTaskSnapshot{TaskID: 17, TenantID: 7, Version: 1, Config: json.RawMessage(`{"source":{"source_engine_id":33,"item_locator":"addp://engine/33/path/model.3DS?type=file&item_id=71","engine_type":"nfs"}}`)}
			switch name {
			case "nfs":
				engine.EngineType = "nfs"
			case "noncanonical ftp":
				engine.EngineType = "FTP"
			case "space ftp":
				engine.EngineType = " ftp "
			case "empty type":
				engine.EngineType = ""
			case "wrong engine":
				engine.ID = 34
			case "wrong tenant":
				tenant = 8
			case "nil ftp tenant":
				engine.TenantID = nil
			case "offline":
				engine.ConnectionStatus = "offline"
			case "unavailable":
				reader.err = errors.New("unavailable")
			case "locator mismatch":
				snapshot.Config = json.RawMessage(`{"source":{"source_engine_id":33,"item_locator":"addp://engine/34/path/model.3DS?type=file&item_id=71"}}`)
			}
			svc := &Model3DGLBTaskService{}
			svc.SetSourceEngineClient(reader)
			actual, err := svc.sourceEngineForSnapshot(t.Context(), snapshot)
			if name == "ftp despite caller label" || name == "nfs" {
				if err != nil || actual != engine || reader.tenant != 7 || reader.id != 33 {
					t.Fatalf("engine=%#v err=%v", actual, err)
				}
				return
			}
			if err == nil || actual != nil {
				t.Fatalf("untrusted engine accepted: %#v %v", actual, err)
			}
		})
	}
}

func TestGLBClaimRejectsIncompleteChangedOrExpiredOwnership(t *testing.T) {
	for _, name := range []string{"valid", "no lease", "attempt", "token", "owner", "tenant", "missing authorization", "expired authorization", "expired lease", "principal", "actor mismatch", "membership", "version", "parent", "scheduled", "source", "module", "task type", "task owner", "pending", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			principal, membership, version, authorization := int64(31), int64(41), int64(1), int64(51)
			actor := 31
			token, owner := uuid.NewString(), "glb-runtime"
			expires := time.Now().Add(time.Hour)
			item := &commonExecution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: 7, Module: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypeModel3DGLBGeneration, Source: commonExecution.ModuleManager, TriggerType: commonExecution.TriggerTypeManual, SourceTaskID: commonExecution.NewSourceTaskIDFromUint(17), ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, Status: commonExecution.ExecutionStatusRunning, Attempt: 1, TriggeredBy: &actor, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorization, AuthorizationExpiresAt: &expires, LeaseExpiresAt: &expires, LeaseToken: &token, LeaseOwner: &owner}
			lease := commonExecution.Lease{ExecutionID: item.ExecutionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner}
			ctx := commonExecution.ContextWithLease(context.Background(), lease)
			switch name {
			case "no lease":
				ctx = context.Background()
			case "attempt":
				item.Attempt = 2
			case "token":
				wrong := uuid.NewString()
				item.LeaseToken = &wrong
			case "owner":
				wrong := "other"
				item.LeaseOwner = &wrong
			case "tenant":
				item.TenantID = 8
			case "missing authorization":
				item.ExecutionAuthorizationID = nil
			case "expired authorization":
				past := time.Now().Add(-time.Second)
				item.AuthorizationExpiresAt = &past
			case "expired lease":
				past := time.Now().Add(-time.Second)
				item.LeaseExpiresAt = &past
			case "principal":
				item.ActorPrincipalID = nil
			case "actor mismatch":
				actor = 32
			case "membership":
				item.ActorTenantMembershipID = nil
			case "version":
				item.IssuedAuthorizationVersion = nil
			case "parent":
				parent := uuid.NewString()
				item.ParentExecutionID = &parent
			case "scheduled":
				item.TriggerType = commonExecution.TriggerTypeScheduled
			case "source":
				item.Source = commonExecution.ModuleOrchestrator
			case "module":
				item.Module = commonExecution.ModuleOrchestrator
			case "task type":
				item.TaskType = commonExecution.TaskTypeDataProfiling
			case "task owner":
				item.SourceTaskID = nil
			case "pending":
				item.Status = commonExecution.ExecutionStatusPending
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := validateModel3DGLBClaim(ctx, item)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrModel3DGLBSourceAuthorizationRequired) {
				t.Fatalf("untrusted claim accepted: %v", err)
			}
		})
	}
}
