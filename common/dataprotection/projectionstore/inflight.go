package projectionstore

import (
	"errors"
	"sync"
)

// InflightReads tracks owner-local work from before its protection gate until
// its final output. It grants no access and does not replace durable leases
// when an owner has multiple data-plane processes. The zero value is ready.
type InflightReads struct {
	mu     sync.Mutex
	active map[int64]int
}

func (r *InflightReads) Begin(tenantID int64) (func(), error) {
	if r == nil || tenantID <= 0 {
		return nil, errors.New("protection read lifecycle is unavailable")
	}
	r.mu.Lock()
	if r.active == nil {
		r.active = make(map[int64]int)
	}
	r.active[tenantID]++
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.active[tenantID]--
			if r.active[tenantID] == 0 {
				delete(r.active, tenantID)
			}
		})
	}, nil
}

func (r *InflightReads) HasActiveExecutionsForTenant(tenantID int64) bool {
	if r == nil || tenantID <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[tenantID] > 0
}
