package projectionstore

import (
	"sync"
	"testing"
)

func TestInflightReadsTenantIsolationAndIdempotentEnd(t *testing.T) {
	var reads InflightReads
	endA, err := reads.Begin(7)
	if err != nil {
		t.Fatal(err)
	}
	endB, err := reads.Begin(7)
	if err != nil {
		t.Fatal(err)
	}
	endOther, err := reads.Begin(8)
	if err != nil {
		t.Fatal(err)
	}
	endA()
	endA()
	if !reads.HasActiveExecutionsForTenant(7) {
		t.Fatal("duplicate release ended another read")
	}
	endB()
	if reads.HasActiveExecutionsForTenant(7) || !reads.HasActiveExecutionsForTenant(8) {
		t.Fatal("tenant lifecycle leaked")
	}
	endOther()
	if len(reads.active) != 0 {
		t.Fatal("ended tenant counters were retained")
	}
}

func TestInflightReadsConcurrentRelease(t *testing.T) {
	var reads InflightReads
	end, err := reads.Begin(7)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() { defer group.Done(); end() }()
	}
	group.Wait()
	if reads.HasActiveExecutionsForTenant(7) {
		t.Fatal("read still active")
	}
}

func TestInflightReadsRejectsInvalidBoundary(t *testing.T) {
	for _, reads := range []*InflightReads{nil, {}} {
		for _, tenant := range []int64{0, -1} {
			if end, err := reads.Begin(tenant); err == nil || end != nil {
				t.Fatal("invalid read was registered")
			}
		}
	}
}
