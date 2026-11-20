package internal

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/models"
)

type recoveringModuleLister struct {
	mu    sync.Mutex
	calls int
}

func (l *recoveringModuleLister) WatchModules(ctx context.Context, revision int64, wait time.Duration) (*models.ModuleRoutingSnapshot, error) {
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
	return &models.ModuleRoutingSnapshot{Revision: 1, Endpoints: []models.ModuleRoutingEndpoint{{ModuleName: "manager", EndpointID: "manager-backend", URL: "http://manager.test", ExpiresAt: time.Now().Add(time.Minute)}}}, nil
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

func TestModuleDiscoveryRoundRobinsValidBackendsAndStopsAtLeaseExpiry(t *testing.T) {
	now := time.Now()
	lister := &staticModuleLister{endpoints: []models.ModuleRoutingEndpoint{
		{ModuleName: "manager", EndpointID: "backend-b", URL: "http://b", ExpiresAt: now.Add(time.Minute)},
		{ModuleName: "manager", EndpointID: "backend-a", URL: "http://a", ExpiresAt: now.Add(time.Minute)},
	}}
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
	if modules := discovery.RouteAddresses(); len(modules) != 0 {
		t.Fatalf("expired module snapshot = %#v, want empty", modules)
	}

	for index := range lister.endpoints {
		lister.endpoints[index].ExpiresAt = now.Add(time.Minute)
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
	catalog := models.ModuleRoutingEndpoint{ModuleName: "catalog", EndpointID: "catalog-a", URL: "http://catalog-a", ExpiresAt: now.Add(30 * time.Second)}
	lister := &staticModuleLister{revision: 5, endpoints: []models.ModuleRoutingEndpoint{catalog}}
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
	lister.revision, lister.endpoints = 6, nil
	refresh()
	lister.revision, lister.endpoints = 5, []models.ModuleRoutingEndpoint{catalog}
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
	now = catalog.ExpiresAt
	refresh()
	if _, err := discovery.GetProxy("catalog"); err == nil {
		t.Fatal("replayed snapshot extended the endpoint deadline")
	}
}

type renewingModuleWatcher struct {
	now func() time.Time
}

func (w *renewingModuleWatcher) WatchModules(context.Context, int64, time.Duration) (*models.ModuleRoutingSnapshot, error) {
	return &models.ModuleRoutingSnapshot{Revision: 1, Endpoints: []models.ModuleRoutingEndpoint{{ModuleName: "manager", EndpointID: "manager-backend", URL: "http://manager.test", ExpiresAt: w.now().Add(30 * time.Second)}}}, nil
}

type staticModuleLister struct {
	revision  int64
	endpoints []models.ModuleRoutingEndpoint
}

func (l *staticModuleLister) WatchModules(ctx context.Context, revision int64, wait time.Duration) (*models.ModuleRoutingSnapshot, error) {
	if revision == 1 && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return &models.ModuleRoutingSnapshot{Revision: l.revision, Endpoints: l.endpoints}, nil
}

type deadlineWatcher struct{ wait, budget time.Duration }

func (w *deadlineWatcher) WatchModules(ctx context.Context, _ int64, wait time.Duration) (*models.ModuleRoutingSnapshot, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil, errors.New("watch transport deadline is missing")
	}
	w.wait, w.budget = wait, time.Until(deadline)
	return &models.ModuleRoutingSnapshot{}, nil
}

func TestStalledWatchLeavesTimeToReconnectBeforeEndpointExpiry(t *testing.T) {
	watcher := new(deadlineWatcher)
	md := NewModuleDiscovery(watcher)
	defer md.Stop()
	now := time.Now()
	md.now = func() time.Time { return now }
	md.backendPools["catalog"] = []moduleBackend{{expiresAt: now.Add(16 * time.Second)}}
	if err := md.refreshModules(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	if watcher.wait != 8*time.Second || watcher.budget > 10*time.Second || watcher.budget <= 8*time.Second {
		t.Fatalf("wait=%s deadline budget=%s", watcher.wait, watcher.budget)
	}
}
