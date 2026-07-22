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
		ModuleURL:  "http://manager.test",
		Status:     "up",
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
