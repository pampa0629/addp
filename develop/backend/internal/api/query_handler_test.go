package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExecuteQueryRequestUsesExecutionConfigContract(t *testing.T) {
	req := ExecuteQueryRequest{
		Content: map[string]interface{}{
			"query":      "SELECT 1",
			"query_type": "sql",
		},
		ExecutionConfig: map[string]interface{}{
			"engine_id": 7,
		},
		Timeout: 30,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal ExecuteQueryRequest: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode ExecuteQueryRequest: %v", err)
	}
	if _, ok := decoded["engine_id"]; ok {
		t.Fatalf("ExecuteQueryRequest must not expose top-level engine_id: %s", payload)
	}
	if _, ok := decoded["query"]; ok {
		t.Fatalf("ExecuteQueryRequest must not expose top-level query: %s", payload)
	}
	if _, ok := decoded["execution_config"]; !ok {
		t.Fatalf("ExecuteQueryRequest must expose execution_config: %s", payload)
	}
}

func TestQueryRequestSQLRequiresCanonicalContentQuery(t *testing.T) {
	_, err := queryRequestSQL(map[string]interface{}{
		"sql":        "SELECT 1",
		"query_type": "sql",
	})
	if err == nil {
		t.Fatal("expected legacy content.sql to be rejected")
	}
	if !strings.Contains(err.Error(), "content.query") {
		t.Fatalf("expected content.query error, got %v", err)
	}
}

func TestQueryRequestEngineIDParsesCanonicalConfig(t *testing.T) {
	engineID, err := queryRequestEngineID(map[string]interface{}{
		"engine_id": float64(7),
	})
	if err != nil {
		t.Fatalf("queryRequestEngineID() error = %v", err)
	}
	if engineID != 7 {
		t.Fatalf("engineID = %d, want 7", engineID)
	}
}

func TestQueryRequestModeNormalizesDuckDB(t *testing.T) {
	mode := queryRequestMode(map[string]interface{}{
		"query_mode": " DuckDB ",
	})
	if mode != "duckdb" {
		t.Fatalf("mode = %q, want duckdb", mode)
	}
}
