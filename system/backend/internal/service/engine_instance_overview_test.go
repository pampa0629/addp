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

type overviewServiceFixture struct {
	*postgresql.PostgreSQLPlugin
	calls  int
	fail   bool
	budget time.Duration
}

func (p *overviewServiceFixture) Type() string { return "test_instance_overview_service" }
func (p *overviewServiceFixture) Capabilities() plugin.EngineCapabilities {
	c := p.PostgreSQLPlugin.Capabilities()
	c.EngineType = p.Type()
	return c
}
func (p *overviewServiceFixture) ReadInstanceOverview(ctx context.Context, _ plugin.ConnectionInfo) (*plugin.InstanceOverview, error) {
	p.calls++
	deadline, _ := ctx.Deadline()
	p.budget = time.Until(deadline)
	if p.fail {
		return nil, errors.New("password=private-native-error")
	}
	m := plugin.InstanceOverviewMetric{Key: "database_size_bytes", Scope: "database", ScopeName: "fixture", Unit: "bytes", Semantics: "gauge"}
	m.SetValue(0)
	return &plugin.InstanceOverview{Metrics: []plugin.InstanceOverviewMetric{m}}, nil
}

func TestInstanceOverviewTenantBoundaryBudgetAndHealthIndependence(t *testing.T) {
	p := &overviewServiceFixture{PostgreSQLPlugin: &postgresql.PostgreSQLPlugin{}}
	plugin.Register(p)
	repo := newEngineServiceTestRepository(t)
	tenant := uint(3)
	engine := &models.Engine{Name: "Overview fixture", EngineType: p.Type(), TenantID: &tenant, Version: 4, LifecycleState: "active", ConnectionStatus: "online", ConnectionInfo: models.ConnectionInfo{}}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}
	s := NewEngineService(repo, nil, nil)
	if _, err := s.ReadInstanceOverview(t.Context(), engine.ID, 7); !errors.Is(err, ErrResourceForbidden) || p.calls != 0 {
		t.Fatal("foreign tenant reached source", err)
	}
	result, err := s.ReadInstanceOverview(t.Context(), engine.ID, tenant)
	if err != nil || result.EngineID != engine.ID || result.Version != 4 || result.QueriedAt.IsZero() || p.budget <= 0 || p.budget > 5*time.Second {
		t.Fatal(result, err, p.budget)
	}
	p.fail = true
	if _, err = s.ReadInstanceOverview(t.Context(), engine.ID, tenant); !errors.Is(err, ErrInstanceOverviewUnavailable) || err.Error() == "password=private-native-error" {
		t.Fatal("native error exposed", err)
	}
	stored, err := repo.GetByID(engine.ID)
	if err != nil || stored.ConnectionStatus != "online" || stored.Version != 4 {
		t.Fatal("overview changed health or configuration", err)
	}
	for _, state := range []string{"deleting", "deleted"} {
		stored.LifecycleState = state
		if err := repo.Update(stored); err != nil {
			t.Fatal(err)
		}
		before := p.calls
		if _, err := s.ReadInstanceOverview(t.Context(), engine.ID, tenant); err == nil || p.calls != before {
			t.Fatal("removed engine reached source")
		}
	}
}
