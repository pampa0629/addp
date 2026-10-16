package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/system/internal/models"
)

type engineProbeBudgetFixture struct {
	*postgresql.PostgreSQLPlugin
	budgets []time.Duration
}

func (p *engineProbeBudgetFixture) Type() string { return "test_engine_probe_budget" }
func (p *engineProbeBudgetFixture) Capabilities() plugin.EngineCapabilities {
	caps := p.PostgreSQLPlugin.Capabilities()
	caps.EngineType = p.Type()
	return caps
}
func (p *engineProbeBudgetFixture) TestConnection(ctx context.Context, _ plugin.ConnectionInfo) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		p.budgets = append(p.budgets, 0)
	} else {
		p.budgets = append(p.budgets, time.Until(deadline))
	}
	return nil
}

func TestManualAndPeriodicEngineProbesShareBoundedBudget(t *testing.T) {
	fixture := &engineProbeBudgetFixture{PostgreSQLPlugin: &postgresql.PostgreSQLPlugin{}}
	plugin.Register(fixture)
	t.Cleanup(func() { plugin.Unregister(fixture.Type()) })
	repo := newEngineServiceTestRepository(t)
	tenantID := uint(7)
	engine := &models.Engine{Name: "probe budget fixture", EngineType: fixture.Type(), EngineOrigin: "general", TenantID: &tenantID, ConnectionInfo: models.ConnectionInfo{}, LifecycleState: models.EngineLifecycleActive, ConnectionStatus: models.EngineConnectionOnline}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}
	if err := NewStorageEngineService().TestConnection(engine); err != nil {
		t.Fatal(err)
	}
	if !NewEngineService(repo, nil, nil).checkAndUpdateConnectionStatus(engine.ID, false) {
		t.Fatal("periodic probe failed")
	}
	if len(fixture.budgets) != 2 {
		t.Fatalf("expected both probe paths, got %d", len(fixture.budgets))
	}
	manual, periodic := fixture.budgets[0], fixture.budgets[1]
	if manual <= 0 || periodic <= 0 {
		t.Fatalf("all probes must have bounded contexts: manual=%s periodic=%s", manual, periodic)
	}
	difference := manual - periodic
	if difference < 0 {
		difference = -difference
	}
	if difference > time.Second {
		t.Fatalf("manual and periodic probe budgets differ: manual=%s periodic=%s", manual, periodic)
	}
	stored, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ConnectionStatus != models.EngineConnectionOnline || stored.LastCheckAt == nil {
		t.Fatal("successful periodic observation was not persisted")
	}
}

// Probe-capable plugins must not also run the base connectivity call.
type versionProbeFixture struct {
	*engineProbeBudgetFixture
	version string
	failure error
}

func (p *versionProbeFixture) Type() string { return "test_native_version_probe" }
func (p *versionProbeFixture) TestConnection(context.Context, plugin.ConnectionInfo) error {
	panic("duplicate connectivity probe")
}
func (p *versionProbeFixture) ProbeConnection(ctx context.Context, info plugin.ConnectionInfo) (plugin.ConnectionProbeResult, error) {
	if err := p.engineProbeBudgetFixture.TestConnection(ctx, info); err != nil {
		return plugin.ConnectionProbeResult{}, err
	}
	return plugin.ConnectionProbeResult{ServerVersion: p.version}, p.failure
}

func TestNativeVersionProbePersistenceAndFailureRetention(t *testing.T) {
	fixture := &versionProbeFixture{engineProbeBudgetFixture: &engineProbeBudgetFixture{PostgreSQLPlugin: &postgresql.PostgreSQLPlugin{}}, version: "NativeDB 4.5"}
	plugin.Register(fixture)
	t.Cleanup(func() { plugin.Unregister(fixture.Type()) })
	repo := newEngineServiceTestRepository(t)
	engine := &models.Engine{Name: t.Name(), EngineType: fixture.Type(), EngineOrigin: "general", ConnectionInfo: models.ConnectionInfo{}, LifecycleState: models.EngineLifecycleActive, ConnectionStatus: models.EngineConnectionOnline}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}
	svc := NewEngineService(repo, nil, nil)
	if !svc.checkAndUpdateConnectionStatus(engine.ID, false) {
		t.Fatal("successful periodic probe failed")
	}
	saved, err := repo.GetByID(engine.ID)
	if err != nil || saved.ServerVersion != fixture.version || saved.ServerVersionCheckedAt == nil || saved.Version != engine.Version || !saved.UpdatedAt.Equal(engine.UpdatedAt) {
		t.Fatalf("unexpected stored observation: %+v err=%v", saved, err)
	}
	fixture.failure = errors.New("offline")
	fixture.version = "failed result"
	if svc.checkAndUpdateConnectionStatus(engine.ID, false) {
		t.Fatal("failed probe reported online")
	}
	fixture.failure = nil
	fixture.version = ""
	if !svc.checkAndUpdateConnectionStatus(engine.ID, false) {
		t.Fatal("empty version probe failed")
	}
	latest, err := repo.GetByID(engine.ID)
	if err != nil || latest.ServerVersion != saved.ServerVersion || latest.ServerVersionCheckedAt == nil || !latest.ServerVersionCheckedAt.Equal(*saved.ServerVersionCheckedAt) {
		t.Fatalf("last successful version lost: %+v err=%v", latest, err)
	}
	if len(fixture.budgets) != 3 {
		t.Fatal("unexpected number of native probes", len(fixture.budgets))
	}
}
