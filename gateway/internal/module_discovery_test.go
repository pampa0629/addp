package internal

import (
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

func (l *recoveringModuleLister) GetModules() ([]*client.ModuleInfo, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls == 1 {
		return nil, errors.New("system unavailable")
	}
	return []*client.ModuleInfo{{
		ModuleName: "manager",
		Enabled:    true,
		Instances: []client.ModuleRuntimeInstanceInfo{{
			InstanceID: "manager-backend", Role: "backend", ModuleURL: "http://manager.test",
			Status: "up", LeaseExpiresAt: time.Now().Add(time.Minute),
		}},
	}}, nil
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

func TestSelectRoutableBackendsIgnoresWorkerAndExpiredInstances(t *testing.T) {
	now := time.Now()
	module := &client.ModuleInfo{
		ModuleName: "manager", Enabled: true,
		Instances: []client.ModuleRuntimeInstanceInfo{
			{InstanceID: "worker", Role: "worker", Status: "up", LeaseExpiresAt: now.Add(time.Minute)},
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
	if err := discovery.refreshModules(); err != nil {
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
	if err := discovery.refreshModules(); err != nil {
		t.Fatal(err)
	}
	if _, err := discovery.GetProxy("manager"); err != nil {
		t.Fatalf("refreshed Backend leases did not recover routing: %v", err)
	}
}

type staticModuleLister struct {
	modules []*client.ModuleInfo
}

func (l *staticModuleLister) GetModules() ([]*client.ModuleInfo, error) {
	return l.modules, nil
}
