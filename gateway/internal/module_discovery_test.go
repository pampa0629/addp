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

func TestSelectRoutableBackendIgnoresWorkerAndExpiredInstances(t *testing.T) {
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
	selected, ok := selectRoutableBackend(module, now)
	if !ok || selected.InstanceID != "backend-a" {
		t.Fatalf("selected backend = %#v, ok=%v", selected, ok)
	}
}
