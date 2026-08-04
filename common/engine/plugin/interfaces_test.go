package plugin

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGraphQueryResultUsesSnakeCaseJSONContract(t *testing.T) {
	payload, err := json.Marshal(GraphQueryResult{
		QueryResult: QueryResult{Columns: []string{"n"}, Rows: []map[string]interface{}{{"n": "value"}}},
		GraphData:   &GraphData{},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, field := range []string{`"columns"`, `"rows"`, `"graph_data"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("GraphQueryResult JSON %s does not contain %s", text, field)
		}
	}
	if strings.Contains(text, `"Columns"`) || strings.Contains(text, `"Rows"`) {
		t.Fatalf("GraphQueryResult JSON contains non-canonical field names: %s", text)
	}
}
