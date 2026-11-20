package resourcequery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFrontendSamplesChargesObservedModules(t *testing.T) {
	const address = "node.test:9443"
	const revision = "3:1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := map[string]string{"entry_up": "1", "cpu_cores": "0", "memory_bytes": "65536"}
		var rows []map[string]any
		for name, v := range values {
			rows = append(rows, map[string]any{"metric": map[string]string{"__name__": "addp_frontend_" + name, "instance": address, "addp_node_id": testNode, "addp_frontend_revision": revision, "frontend_module": "portal", "frontend_mode": "vite"}, "value": []any{testTime.Unix(), v}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": rows}})
	}))
	defer server.Close()
	c := Client{origin: server.URL, http: server.Client()}
	b := DefaultBudget()
	b.MaxSeries = 3
	rows, err := c.FrontendSamples(context.Background(), testNode, address, revision, testTime, b)
	if err != nil || len(rows) != 1 {
		t.Fatalf("samples %+v: %v", rows, err)
	}
	row := rows[0]
	if row.Module != "portal" || row.CPUCores == nil || *row.CPUCores != 0 || row.MemoryBytes == nil || *row.MemoryBytes != 65536 || row.AssetUp != nil {
		t.Fatalf("unexpected module values: %+v", row)
	}
	b.MaxSeries = 2
	if _, err := c.FrontendSamples(context.Background(), testNode, address, revision, testTime, b); !errors.Is(err, ErrBudget) {
		t.Fatalf("over budget: %v", err)
	}
}
