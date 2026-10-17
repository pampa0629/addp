package plugin

import (
	"context"
	"errors"
	"testing"
)

func TestPreparedGraphQueryPreservesResultContractAndOneShotState(t *testing.T) {
	executions := 0
	plan, err := NewPreparedGraphQuery(&QueryAnalysis{Language: "cypher", SchemaCoverage: QuerySchemaCoverageUnknown}, func(context.Context) (*QueryEngineReadScope, error) { return NewQueryEngineReadScope(13) }, nil, func(context.Context) (*GraphQueryResult, error) {
		executions++
		return &GraphQueryResult{GraphData: &GraphData{Nodes: []GraphNode{{ElementId: "node"}}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, isTable := plan.(PreparedQuery); isTable {
		t.Fatal("graph plan incorrectly implements table execution")
	}
	if scope, err := plan.EngineReadScope(t.Context()); err != nil || !scope.Contains(13) {
		t.Fatalf("scope=%v %v", scope, err)
	}
	result, err := plan.ExecuteGraph(t.Context())
	if err != nil || result.GraphData == nil || len(result.GraphData.Nodes) != 1 {
		t.Fatalf("result=%v %v", result, err)
	}
	if _, err := plan.ExecuteGraph(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) || executions != 1 {
		t.Fatalf("one shot consumed %d times: %v", executions, err)
	}
	if _, err := plan.ReadSet(t.Context()); !errors.Is(err, ErrPreparedQueryConsumed) {
		t.Fatalf("facts created after execution: %v", err)
	}
}
