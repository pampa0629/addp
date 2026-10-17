package plugin

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPreparedQueryEngineScopeIsUnknownWithoutProof(t *testing.T) {
	prepared, err := NewPreparedQuery(&QueryAnalysis{Language: "cypher", SchemaCoverage: QuerySchemaCoverageUnknown}, nil, nil, nil,
		func(context.Context) (*QueryResult, error) { return &QueryResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if scope, err := prepared.EngineReadScope(t.Context()); scope != nil || !errors.Is(err, ErrQueryEngineReadScopeUnresolved) {
		t.Fatalf("unproved scope = %#v, %v", scope, err)
	}
}

func TestPreparedQueryFreezesEngineScopeAndRejectsReadSetOutsideIt(t *testing.T) {
	scope, err := NewQueryEngineReadScope(13, 3, 13)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scope.EngineIDs, []uint{3, 13}) {
		t.Fatalf("scope = %#v", scope)
	}
	calls := 0
	prepared, err := NewPreparedQuery(&QueryAnalysis{Language: "sql", SchemaCoverage: QuerySchemaCoverageUnknown},
		func(context.Context) (*QueryEngineReadScope, error) { calls++; return scope, nil },
		func(context.Context) (*QueryReadSet, error) {
			return NewQueryReadSet(TabularItemPath(29, EngineCatalogTermSchema, "public", "people"))
		}, nil,
		func(context.Context) (*QueryResult, error) { return &QueryResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepared.EngineReadScope(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.EngineIDs[0] = 99
	scope.EngineIDs[0] = 88
	second, err := prepared.EngineReadScope(t.Context())
	if err != nil || calls != 1 || !reflect.DeepEqual(second.EngineIDs, []uint{3, 13}) {
		t.Fatalf("scope = %#v, calls=%d, err=%v", second, calls, err)
	}
	if _, err := prepared.ReadSet(t.Context()); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("out-of-scope read set error = %v", err)
	}
}

func TestQueryEngineScopeRequiresCanonicalIDsAndKeepsProvenEmptyDistinct(t *testing.T) {
	if _, err := NewQueryEngineReadScope(0); err == nil {
		t.Fatal("zero engine ID accepted")
	}
	for _, ids := range [][]uint{{0}, {13, 3}, {13, 13}} {
		if err := (&QueryEngineReadScope{EngineIDs: ids}).Validate(); err == nil {
			t.Fatalf("noncanonical IDs accepted: %v", ids)
		}
	}
	empty, err := NewQueryEngineReadScope()
	if err != nil || empty == nil || len(empty.EngineIDs) != 0 {
		t.Fatalf("proven empty = %#v, %v", empty, err)
	}
	var unknown *QueryEngineReadScope
	if !errors.Is(unknown.Validate(), ErrQueryEngineReadScopeUnresolved) {
		t.Fatal("unknown scope accepted as empty")
	}
}
