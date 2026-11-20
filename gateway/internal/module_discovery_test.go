package internal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/client"
)

type recoveringModuleLister struct {
	mu    sync.Mutex
	calls int
}

func (l *recoveringModuleLister) WatchModules(ctx context.Context, revision int64, wait time.Duration) (*client.ModuleRoutingSnapshot, error) {
	l.mu.Lock()
	l.calls++
	call := l.calls
	l.mu.Unlock()
	if call == 1 {
		return nil, errors.New("system unavailable")
	}
	if revision == 1 && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return &client.ModuleRoutingSnapshot{Revision: 1, Modules: []*client.ModuleInfo{{
		ModuleName: "manager",
		Enabled:    true,
		Instances: []client.ModuleRuntimeInstanceInfo{{
			InstanceID: "manager-backend", Role: "backend", ModuleURL: "http://manager.test",
			Status: "up", LeaseExpiresAt: time.Now().Add(time.Minute),
		}},
	}}}, nil
}

func TestModuleDiscoveryRetriesAfterInitialRefreshFailure(t *testing.T) {
	lister := &recoveringModuleLister{}
	discovery := NewModuleDiscovery(lister)
	defer discovery.Stop()

	if err := discovery.Start(5 * time.Millisecond); err == nil {
		t.Fatal("expected initial refresh error")
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := discovery.GetProxy("manager"); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("module discovery did not recover after the initial refresh failure")
}

func TestSelectRoutableBackendsIgnoresNonBackendAndExpiredInstances(t *testing.T) {
	now := time.Now()
	module := &client.ModuleInfo{
		ModuleName: "manager", Enabled: true,
		Instances: []client.ModuleRuntimeInstanceInfo{
			{InstanceID: "worker", Role: "worker", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
			{InstanceID: "ingress", Role: client.ModuleRuntimeRoleIngress, ModuleURL: "http://gateway", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
			{InstanceID: "expired", Role: "backend", ModuleURL: "http://expired", Status: "up", LeaseExpiresAt: now.Add(-time.Second)},
			{InstanceID: "backend-b", Role: "backend", ModuleURL: "http://b", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
			{InstanceID: "backend-a", Role: "backend", ModuleURL: "http://a", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
		},
	}
	selected := selectRoutableBackends(module, now)
	if len(selected) != 2 || selected[0].InstanceID != "backend-a" || selected[1].InstanceID != "backend-b" {
		t.Fatalf("selected backends = %#v", selected)
	}
}

func TestModuleDiscoveryRoundRobinsValidBackendsAndStopsAtLeaseExpiry(t *testing.T) {
	now := time.Now()
	lister := &staticModuleLister{modules: []*client.ModuleInfo{{
		ModuleName: "manager", Enabled: true,
		Instances: []client.ModuleRuntimeInstanceInfo{
			{InstanceID: "backend-b", Role: "backend", ModuleURL: "http://b", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
			{InstanceID: "backend-a", Role: "backend", ModuleURL: "http://a", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
		},
	}}}
	discovery := NewModuleDiscovery(lister)
	discovery.now = func() time.Time { return now }
	defer discovery.Stop()
	if err := discovery.refreshModules(0); err != nil {
		t.Fatal(err)
	}

	for index, want := range []string{"http://a", "http://b", "http://a"} {
		selected, err := discovery.GetProxy("manager")
		if err != nil {
			t.Fatalf("selection %d: %v", index, err)
		}
		if got := selected.GetTargetURL(); got != want {
			t.Fatalf("selection %d = %q, want %q", index, got, want)
		}
	}

	now = now.Add(2 * time.Minute)
	if _, err := discovery.GetProxy("manager"); err == nil {
		t.Fatal("expected expired Backend pool to be unavailable")
	}
	if modules := discovery.GetModules(); len(modules) != 0 {
		t.Fatalf("expired module snapshot = %#v, want empty", modules)
	}

	for index := range lister.modules[0].Instances {
		lister.modules[0].Instances[index].LeaseExpiresAt = now.Add(time.Minute)
	}
	if err := discovery.refreshModules(0); err != nil {
		t.Fatal(err)
	}
	if _, err := discovery.GetProxy("manager"); err != nil {
		t.Fatalf("refreshed Backend leases did not recover routing: %v", err)
	}
}

func TestModuleDiscoveryRefreshesLeaseProjectionWithoutRevisionChange(t *testing.T) {
	now := time.Now()
	watcher := &renewingModuleWatcher{now: func() time.Time { return now }}
	discovery := NewModuleDiscovery(watcher)
	discovery.now = func() time.Time { return now }
	defer discovery.Stop()

	if err := discovery.refreshModules(0); err != nil {
		t.Fatal(err)
	}
	now = now.Add(25 * time.Second)
	if err := discovery.refreshModules(0); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	if _, err := discovery.GetProxy("manager"); err != nil {
		t.Fatalf("same-revision fresh snapshot did not renew cached lease: %v", err)
	}
}

func TestCatalogWithdrawalSurvivesOlderSnapshot(t *testing.T) {
	now := time.Now()
	catalog := &client.ModuleInfo{ModuleName: "catalog", Enabled: true, Instances: []client.ModuleRuntimeInstanceInfo{
		{InstanceID: "catalog-a", Role: "backend", Status: "up", ModuleURL: "http://catalog-a", LeaseExpiresAt: now.Add(30 * time.Second)},
	}}
	lister := &staticModuleLister{revision: 5, modules: []*client.ModuleInfo{catalog}}
	discovery := NewModuleDiscovery(lister)
	defer discovery.Stop()
	discovery.now = func() time.Time { return now }
	refresh := func() {
		t.Helper()
		if err := discovery.refreshModules(0); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	lister.revision, lister.modules = 6, nil
	refresh()
	lister.revision, lister.modules = 5, []*client.ModuleInfo{catalog}
	if err := discovery.refreshModules(0); err == nil {
		t.Fatal("accepted a superseded routing revision")
	}
	if _, err := discovery.GetProxy("catalog"); err == nil {
		t.Fatal("withdrawn endpoint became routable again")
	}
	lister.revision = 7
	refresh()
	if _, err := discovery.GetProxy("catalog"); err != nil {
		t.Fatal(err)
	}
	now = catalog.Instances[0].LeaseExpiresAt
	refresh()
	if _, err := discovery.GetProxy("catalog"); err == nil {
		t.Fatal("replayed snapshot extended the endpoint deadline")
	}
}

type renewingModuleWatcher struct {
	now func() time.Time
}

func (w *renewingModuleWatcher) WatchModules(context.Context, int64, time.Duration) (*client.ModuleRoutingSnapshot, error) {
	return &client.ModuleRoutingSnapshot{Revision: 1, Modules: []*client.ModuleInfo{{
		ModuleName: "manager", Enabled: true,
		Instances: []client.ModuleRuntimeInstanceInfo{{
			InstanceID: "manager-backend", Role: "backend", ModuleURL: "http://manager.test",
			Status: "up", LeaseExpiresAt: w.now().Add(30 * time.Second),
		}},
	}}}, nil
}

type staticModuleLister struct {
	revision int64
	modules  []*client.ModuleInfo
}

func (l *staticModuleLister) WatchModules(ctx context.Context, revision int64, wait time.Duration) (*client.ModuleRoutingSnapshot, error) {
	if revision == 1 && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return &client.ModuleRoutingSnapshot{Revision: l.revision, Modules: l.modules}, nil
}
