package service

import (
	"context"
	"errors"
	"testing"

	"github.com/addp/common/execution"
	"github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	managermodels "github.com/addp/manager/internal/models"
	managerprotection "github.com/addp/manager/internal/protection"
	"gorm.io/gorm"
)

type dispatcherFreshener struct{ err error }

func (s dispatcherFreshener) EnsureCurrent(ctx context.Context, _ int64) error {
	if s.err != nil {
		return s.err
	}
	return ctx.Err()
}

type trackedProfileSampler struct {
	dataProfileServiceTestSampler
	t     *testing.T
	reads *managerprotection.ReadBoundary
}

func (s *trackedProfileSampler) Sample(ctx context.Context, target *DataProfileTarget, scope dataprofile.DataScope, budget DataProfileBudget) (*DataProfileSample, error) {
	if !s.reads.HasActiveExecutionsForTenant(7) {
		s.t.Error("sampling was not registered")
	}
	return s.dataProfileServiceTestSampler.Sample(ctx, target, scope, budget)
}

type trackedProfileStore struct {
	dataProfileServiceTestProfileStore
	t     *testing.T
	reads *managerprotection.ReadBoundary
}

func (s *trackedProfileStore) ReplaceCurrent(ctx context.Context, tx *gorm.DB, state *managermodels.DataProfile, profile dataprofile.Profile) error {
	if !s.reads.HasActiveExecutionsForTenant(7) {
		s.t.Error("result persistence was not registered")
	}
	return s.dataProfileServiceTestProfileStore.ReplaceCurrent(ctx, tx, state, profile)
}

func TestBoundedExecutionDispatcherTracksProfileThroughPersistence(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "sampling-fails"}[failure], func(t *testing.T) {
			reads := managerprotection.NewReadBoundary(dispatcherFreshener{})
			target := &DataProfileTarget{Locator: "addp://engine/1/path/public/orders?type=table", ItemFingerprint: "orders", SourceVersion: "version"}
			sampler := &trackedProfileSampler{dataProfileServiceTestSampler: dataProfileServiceTestSampler{
				target: target, sample: &DataProfileSample{},
			}, t: t, reads: reads}
			if failure {
				sampler.sampleErr = errors.New("sample failed")
			}
			profiles := &trackedProfileStore{t: t, reads: reads}
			executions := &dataProfileServiceTestExecutionStore{}
			profileService := NewDataProfileService(profiles, executions, sampler, &dataProfileServiceTestProtectionGate{})
			dispatcher := &BoundedExecutionDispatcher{dataProfile: profileService, readBoundary: reads}
			item := &execution.TaskExecution{
				ExecutionID: "profile", TenantID: 7, TaskType: execution.TaskTypeDataProfiling,
				ExecutionConfig: models.JSONMap{
					"locator": target.Locator, "item_fingerprint": target.ItemFingerprint, "source_version": target.SourceVersion,
					"data_scope": dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
				},
			}
			if err := dispatcher.RunClaimedExecution(t.Context(), item, execution.Lease{ExecutionID: item.ExecutionID, TenantID: 7}); err != nil {
				t.Fatal(err)
			}
			if failure && executions.failedCode != "sample_failed" {
				t.Fatalf("failure = %s", executions.failedCode)
			}
			if !failure && profiles.replaceCalls != 1 {
				t.Fatal("profile was not stored")
			}
			if reads.HasActiveExecutionsForTenant(7) {
				t.Fatal("completed dispatcher retained read")
			}
		})
	}
}

func TestBoundedExecutionDispatcherFailsBeforeDomainWorkWhenRefreshFails(t *testing.T) {
	for _, boundary := range []*managerprotection.ReadBoundary{nil, managerprotection.NewReadBoundary(dispatcherFreshener{err: errors.New("checkpoint unavailable")})} {
		dispatcher := &BoundedExecutionDispatcher{readBoundary: boundary}
		item := &execution.TaskExecution{ExecutionID: "profile", TenantID: 7, TaskType: execution.TaskTypeDataProfiling}
		if err := dispatcher.RunClaimedExecution(t.Context(), item, execution.Lease{ExecutionID: item.ExecutionID, TenantID: 7}); !errors.Is(err, managerprotection.ErrRequired) {
			t.Fatalf("RunClaimedExecution: %v", err)
		}
		if boundary.HasActiveExecutionsForTenant(7) {
			t.Fatal("failed dispatcher retained read")
		}
	}
}
