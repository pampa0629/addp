package service

import (
	"context"
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
