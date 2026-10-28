package authorizationfacts

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIdentityQueryDistinguishesCapacityFromInvalidInput(t *testing.T) {
	query := IdentityQuery{EngineID: 2, Page: 1, PageSize: 20, FingerprintFilter: "include"}
	for i := 0; i <= MaxIdentityQueryFingerprints; i++ {
		query.Fingerprints = append(query.Fingerprints, fmt.Sprintf("%064x", i))
	}
	if err := query.Validate(); !errors.Is(err, ErrIdentityQueryCapacity) {
		t.Fatalf("20001 complete fingerprints: %v", err)
	}
	query.Fingerprints = query.Fingerprints[:MaxIdentityQueryFingerprints]
	if err := query.Validate(); err != nil {
		t.Fatalf("20000 complete fingerprints: %v", err)
	}
	query.FingerprintFilter = "invalid"
	if err := query.Validate(); err == nil || errors.Is(err, ErrIdentityQueryCapacity) {
		t.Fatalf("invalid filter classified as capacity: %v", err)
	}
}

func TestIdentityQueryRejectsAmbiguousOrUnboundedFilters(t *testing.T) {
	base := IdentityQuery{EngineID: 2, Page: 1, PageSize: 20}
	for _, change := range []func(*IdentityQuery){
		func(q *IdentityQuery) { q.EngineID = 0 }, func(q *IdentityQuery) { q.Page = 0 }, func(q *IdentityQuery) { q.PageSize = 101 },
		func(q *IdentityQuery) { q.Search = "\x00" }, func(q *IdentityQuery) { q.Search = strings.Repeat("a", 513) },
		func(q *IdentityQuery) { q.FingerprintFilter = "all" }, func(q *IdentityQuery) { q.Fingerprints = []string{strings.Repeat("a", 64)} },
		func(q *IdentityQuery) { q.FingerprintFilter = "include"; q.Fingerprints = []string{"invalid"} },
		func(q *IdentityQuery) {
			q.FingerprintFilter = "include"
			q.Fingerprints = []string{strings.Repeat("a", 64), strings.Repeat("a", 64)}
		},
		func(q *IdentityQuery) {
			q.FingerprintFilter = "exclude"
			q.Fingerprints = make([]string, MaxIdentityQueryFingerprints+1)
		},
	} {
		q := base
		change(&q)
		if q.Validate() == nil {
			t.Fatal("invalid query accepted")
		}
	}
	for _, mode := range []string{"", "include", "exclude"} {
		q := base
		q.FingerprintFilter = mode
		if err := q.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestIdentityPageBindsMembershipAndOrdering(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	query := IdentityQuery{EngineID: 2, Page: 1, PageSize: 20, FingerprintFilter: "include", Fingerprints: []string{a}}
	page := IdentityPage{Data: []DataItemIdentity{{ItemFingerprint: a, EngineID: 2, Name: "n", FullName: "s.n", ItemType: "table"}}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}
	if err := page.Validate(query); err != nil {
		t.Fatal(err)
	}
	query.Fingerprints = []string{b}
	if page.Validate(query) == nil {
		t.Fatal("include mismatch accepted")
	}
	query.FingerprintFilter = "exclude"
	query.Fingerprints = []string{a}
	if page.Validate(query) == nil {
		t.Fatal("excluded identity returned")
	}
	query.FingerprintFilter = ""
	query.Fingerprints = nil
	page.Data = append(page.Data, page.Data[0])
	page.Total = 2
	if page.Validate(query) == nil {
		t.Fatal("duplicate identity accepted")
	}
	page.Data[0].ItemFingerprint = b
	if page.Validate(query) == nil {
		t.Fatal("unordered identities accepted")
	}
}
