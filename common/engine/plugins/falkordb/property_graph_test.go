package falkordb

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func searchIndexRow(status string) map[string]interface{} {
	return map[string]interface{}{"label": "Person", "entitytype": "NODE", "properties": []interface{}{"name"},
		"types": map[string]interface{}{"name": []interface{}{"FULLTEXT"}}, "status": status}
}

func TestSearchIndexWaitsForNativeOperationalState(t *testing.T) {
	calls := 0
	run := func(query string, _ map[string]interface{}) (*plugin.GraphQueryResult, error) {
		if !strings.Contains(query, "entitytype, status") {
			t.Fatal("readiness must use the native index status")
		}
		calls++
		status := "[Indexing] 0/3: UNDER CONSTRUCTION"
		if calls == 2 {
			status = "OPERATIONAL"
		}
		return &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: []map[string]interface{}{searchIndexRow(status)}}}, nil
	}
	if err := waitGraphSearchIndexes(t.Context(), map[string][]string{"Person": {"name"}}, run); err != nil || calls != 2 {
		t.Fatalf("index became searchable before native completion: calls=%d err=%v", calls, err)
	}
}

func TestSearchIndexReadinessRejectsInvalidStateAndChangedDefinition(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(map[string]interface{})
	}{
		{"missing status", func(row map[string]interface{}) { delete(row, "status") }},
		{"unknown state", func(row map[string]interface{}) { row["status"] = "FAILED" }},
		{"invalid metadata", func(row map[string]interface{}) { row["types"] = []interface{}{"FULLTEXT"} }},
		{"different fields", func(row map[string]interface{}) {
			row["properties"] = []interface{}{"other"}
			row["types"] = map[string]interface{}{"other": []interface{}{"FULLTEXT"}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := searchIndexRow("OPERATIONAL")
			test.edit(row)
			err := waitGraphSearchIndexes(t.Context(), map[string][]string{"Person": {"name"}},
				func(string, map[string]interface{}) (*plugin.GraphQueryResult, error) {
					return &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: []map[string]interface{}{row}}}, nil
				})
			if err == nil {
				t.Fatal("unusable index was admitted")
			}
		})
	}
}

func TestSearchIndexWaitHonorsCancellationAndQueryErrors(t *testing.T) {
	for _, rows := range [][]map[string]interface{}{nil, {searchIndexRow("[Indexing] 0/3: UNDER CONSTRUCTION")}} {
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		err := waitGraphSearchIndexes(ctx, map[string][]string{"Person": {"name"}}, func(string, map[string]interface{}) (*plugin.GraphQueryResult, error) {
			return &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: rows}}, nil
		})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unfinished index must return its deadline error: %v", err)
		}
	}
	failure := errors.New("native index metadata failed")
	if err := waitGraphSearchIndexes(t.Context(), map[string][]string{"Person": {"name"}}, func(string, map[string]interface{}) (*plugin.GraphQueryResult, error) {
		return nil, failure
	}); !errors.Is(err, failure) {
		t.Fatalf("native query error was hidden: %v", err)
	}
	if err := waitGraphSearchIndexes(t.Context(), nil, func(string, map[string]interface{}) (*plugin.GraphQueryResult, error) {
		t.Fatal("empty search schema must not query indexes")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
}
