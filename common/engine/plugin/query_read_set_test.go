package plugin

import (
	"context"
	"errors"
	"testing"
)

func TestNewQueryReadSetCanonicalizesPaths(t *testing.T) {
	orders := TabularItemPath(9, EngineCatalogTermSchema, "business", "orders")
	persons := TabularItemPath(9, EngineCatalogTermSchema, "business", "persons")

	readSet, err := NewQueryReadSet(persons, orders, persons)
	if err != nil {
		t.Fatal(err)
	}
	if len(readSet.Paths) != 2 {
		t.Fatalf("path count = %d, want 2", len(readSet.Paths))
	}
	if readSet.Paths[0].StringPath() != "business/orders" || readSet.Paths[1].StringPath() != "business/persons" {
		t.Fatalf("paths = %#v", readSet.Paths)
	}

	persons.Segments[len(persons.Segments)-1].Name = "changed"
	if readSet.Paths[1].StringPath() != "business/persons" {
		t.Fatal("read set retained caller-owned path storage")
	}
}

func TestValidateQueryReadSetBindsReadOnlyQueryAndEngine(t *testing.T) {
	request := QueryRequest{
		EngineID: 9,
		Language: "sql",
		Query:    "SELECT * FROM business.orders",
		Options:  QueryOptions{ReadOnly: true},
	}
	readSet, err := NewQueryReadSet(TabularItemPath(9, EngineCatalogTermSchema, "business", "orders"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueryReadSet(request, readSet); err != nil {
		t.Fatal(err)
	}

	crossEngine, err := NewQueryReadSet(TabularItemPath(10, EngineCatalogTermSchema, "business", "orders"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateQueryReadSet(request, crossEngine); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("cross-engine validation error = %v", err)
	}

	request.Options.ReadOnly = false
	if err := ValidateQueryReadSet(request, readSet); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("write query validation error = %v", err)
	}
}

func TestQueryReadSetRejectsBranchesAndNonCanonicalInput(t *testing.T) {
	branch := TabularNamespacePath(9, EngineCatalogTermSchema, "business")
	if _, err := NewQueryReadSet(branch); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("branch error = %v", err)
	}

	orders := TabularItemPath(9, EngineCatalogTermSchema, "business", "orders")
	persons := TabularItemPath(9, EngineCatalogTermSchema, "business", "persons")
	request := QueryRequest{EngineID: 9, Language: "sql", Query: "SELECT 1", Options: QueryOptions{ReadOnly: true}}
	if err := ValidateQueryReadSet(request, &QueryReadSet{Paths: []EngineCatalogPath{persons, orders}}); !errors.Is(err, ErrQueryReadSetUnresolved) {
		t.Fatalf("unsorted read set error = %v", err)
	}
}

func TestEmptyQueryReadSetIsValidForScalarRead(t *testing.T) {
	readSet, err := NewQueryReadSet()
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateQueryReadSet(QueryRequest{
		EngineID: 5, Language: "sql", Query: "SELECT 1", Options: QueryOptions{ReadOnly: true},
	}, readSet)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreparedQueryCachesReadSetAndExecutesOnce(t *testing.T) {
	resolved := 0
	executed := 0
	prepared, err := NewPreparedQuery(
		&QueryAnalysis{Language: "sql", SchemaCoverage: QuerySchemaCoverageUnknown},
		func(context.Context) (*QueryReadSet, error) {
			resolved++
			return NewQueryReadSet(TabularItemPath(9, EngineCatalogTermSchema, "business", "persons"))
		},
		nil,
		func(context.Context) (*QueryResult, error) {
			executed++
			return &QueryResult{Columns: []string{"id"}}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	first, err := prepared.ReadSet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.Paths[0].Segments[len(first.Paths[0].Segments)-1].Name = "changed"
	second, err := prepared.ReadSet(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if resolved != 1 || second.Paths[0].StringPath() != "business/persons" {
		t.Fatalf("resolved = %d, cached read set = %#v", resolved, second.Paths)
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatalf("second execution error = %v", err)
	}
	if executed != 1 {
		t.Fatalf("executed = %d, want 1", executed)
	}
}

func TestPreparedQueryCannotResolveReadSetAfterExecution(t *testing.T) {
	prepared, err := NewPreparedQuery(
		&QueryAnalysis{Language: "sql", SchemaCoverage: QuerySchemaCoverageUnknown},
		func(context.Context) (*QueryReadSet, error) { return NewQueryReadSet() },
		nil,
		func(context.Context) (*QueryResult, error) { return &QueryResult{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := prepared.ReadSet(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatalf("read set after execution error = %v", err)
	}
}
