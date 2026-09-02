package service

import (
	"context"
	"errors"
	"testing"
)

type runnerTestRegistry struct{ tenantIDs []uint }

func (r runnerTestRegistry) ListRuntimeTenantIDs(context.Context) ([]uint, error) {
	return append([]uint(nil), r.tenantIDs...), nil
}

type runnerTestSynchronizer struct {
	name    string
	err     error
	tenants []int64
}

func (s *runnerTestSynchronizer) SourceName() string { return s.name }

func (s *runnerTestSynchronizer) SyncTenant(_ context.Context, tenantID int64) error {
	s.tenants = append(s.tenants, tenantID)
	return s.err
}

func TestSourceSyncRunnerKeepsOwnerSourcesIndependent(t *testing.T) {
	meta := &runnerTestSynchronizer{name: "Meta", err: errors.New("Meta unavailable")}
	model := &runnerTestSynchronizer{name: "Model"}
	standard := &runnerTestSynchronizer{name: "Standard"}
	service := &runnerTestSynchronizer{name: "Service"}
	develop := &runnerTestSynchronizer{name: "Develop"}
	runner := NewSourceSyncRunner(nil, 0, runnerTestRegistry{tenantIDs: []uint{7}}, meta, model, standard, service, develop)

	runner.syncObservedTenants(context.Background())

	if len(meta.tenants) != 1 || meta.tenants[0] != 7 || len(model.tenants) != 1 || model.tenants[0] != 7 || len(standard.tenants) != 1 || standard.tenants[0] != 7 || len(service.tenants) != 1 || service.tenants[0] != 7 || len(develop.tenants) != 1 || develop.tenants[0] != 7 {
		t.Fatalf("source calls Meta=%v Model=%v Standard=%v Service=%v Develop=%v", meta.tenants, model.tenants, standard.tenants, service.tenants, develop.tenants)
	}
}
