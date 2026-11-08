package service

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"unsafe"
)

func TestIncrementalRRFMatchesEveryCompletePrefix(t *testing.T) {
	rng := rand.New(rand.NewSource(20261108))
	exercise := func(a, b []searchIdentity) {
		t.Helper()
		ranks := map[searchIdentity]*searchRankedIdentity{}
		for source, ids := range [][]searchIdentity{a, b} {
			for i, id := range ids {
				row := ranks[id]
				if row == nil {
					row = &searchRankedIdentity{ID: id}
					ranks[id] = row
				}
				row.Ranks[source] = uint64(i + 1)
			}
		}
		expected := make([]searchRankedIdentity, 0, len(ranks))
		for _, row := range ranks {
			expected = append(expected, *row)
		}
		sort.Slice(expected, func(i, j int) bool { return searchRankLess(expected[i], expected[j]) })
		allocated := int64(0)
		s := newSearchRRF(func(delta int64) error { allocated += delta; return nil })
		positions := [2]int{}
		sources := [2][]searchIdentity{a, b}
		delivered := 0
		for steps := 0; delivered < len(expected); steps++ {
			if steps > len(a)+len(b)+len(expected)+5 {
				t.Fatal("RRF did not make progress")
			}
			for source, ids := range sources {
				if positions[source] < len(ids) {
					if err := s.accept(source, ids[positions[source]], 2); err != nil {
						t.Fatal(err)
					}
					positions[source]++
				} else {
					s.done[source] = true
				}
			}
			if _, err := s.promote(200); err != nil {
				t.Fatal(err)
			}
			for {
				row, ok := s.pop()
				if !ok {
					break
				}
				if row.ID != expected[delivered].ID || row.Ranks != expected[delivered].Ranks {
					t.Fatalf("prefix %d differs: actual=%+v expected=%+v", delivered, row, expected[delivered])
				}
				delivered++
			}
			heapEntries := 0
			for c := range s.heaps {
				heapEntries += s.heaps[c].Len()
			}
			if heapEntries != s.count {
				t.Fatal("pending row has missing or stale heap entries")
			}
			actual := int64(cap(s.rows))*int64(unsafe.Sizeof(searchRRFRow{})) + int64(len(s.table))*4
			for c := range s.heaps {
				actual += int64(cap(s.heaps[c].slots)) * 4
			}
			if allocated != actual || s.bytes != actual {
				t.Fatalf("allocated capacity not accounted: reserved=%d actual=%d", allocated, actual)
			}
		}
		if s.count != 0 {
			t.Fatal("delivered identities remained pending")
		}
	}
	for n := 0; n < 1200; n++ {
		population := make([]searchIdentity, rng.Intn(50))
		for i := range population {
			population[i] = sha256.Sum256([]byte(fmt.Sprintf("%d/%d", n, i)))
		}
		permutation := rng.Perm(len(population))
		a := make([]searchIdentity, rng.Intn(len(population)+1))
		for i := range a {
			a[i] = population[permutation[i]]
		}
		permutation = rng.Perm(len(population))
		b := make([]searchIdentity, rng.Intn(len(population)+1))
		for i := range b {
			b[i] = population[permutation[i]]
		}
		exercise(a, b)
	}
	disjointA, disjointB := make([]searchIdentity, 1000), make([]searchIdentity, 1000)
	for i := range disjointA {
		disjointA[i] = sha256.Sum256([]byte(fmt.Sprintf("k%d", i)))
		disjointB[i] = sha256.Sum256([]byte(fmt.Sprintf("v%d", i)))
	}
	exercise(disjointA, disjointB)
	long := make([]searchIdentity, 4000)
	for i := range long {
		long[i] = sha256.Sum256([]byte(fmt.Sprintf("long%d", i)))
	}
	exercise(long, []searchIdentity{sha256.Sum256([]byte("independent vector"))})
}

func TestIncrementalRRFCapacityFailureDoesNotConsumeInput(t *testing.T) {
	s := newSearchRRF(func(int64) error { return ErrSearchCapacityExceeded })
	id := sha256.Sum256([]byte("candidate"))
	if err := s.accept(0, id, 2); !errors.Is(err, ErrSearchCapacityExceeded) {
		t.Fatal(err)
	}
	if s.ranks[0] != 0 || s.count != 0 || len(s.rows) != 0 || len(s.table) != 0 {
		t.Fatal("capacity failure advanced candidate state")
	}
}
