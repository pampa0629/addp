package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEngineDeploymentReferencesUseOnlyCurrentUserAndRejectPartialBatch(t *testing.T) {
	id, node := uuid.NewString(), uuid.NewString()
	denied := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/api/v1/system/platform/engine_deployments/"+id || r.Header.Get("Authorization") != "Bearer addp_at_human" {
			t.Error("wrong owner read or credential")
		}
		w.Header().Set("Content-Type", "application/json")
		if denied {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"safe","error_code":"forbidden"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"deployment_id": id, "display_name": "PostgreSQL", "engine_type": "postgresql", "version": 1, "enabled": true, "node_id": node, "host_binding_state": "bound", "inventory": map[string]string{"sql": "not retained"}})
	}))
	defer server.Close()
	c := &SystemServiceClient{baseURL: server.URL, httpClient: server.Client()}
	rows, err := c.GetEngineDeploymentsForUser(context.Background(), []string{id}, "addp_at_human")
	if err != nil || len(rows) != 1 || rows[0].NodeID != node {
		t.Fatalf("reference %+v %v", rows, err)
	}
	raw, _ := json.Marshal(rows)
	if strings.Contains(string(raw), "not retained") {
		t.Fatal("inventory retained")
	}
	denied = true
	if rows, err := c.GetEngineDeploymentsForUser(context.Background(), []string{id}, "addp_at_human"); err == nil || rows != nil {
		t.Fatal("denied owner read accepted")
	}
	if calls != 2 {
		t.Fatal("machine retry occurred")
	}
	for _, ids := range [][]string{nil, {id, id}, {"foreign"}} {
		if _, err := c.GetEngineDeploymentsForUser(context.Background(), ids, "addp_at_human"); err == nil {
			t.Fatal("invalid IDs accepted")
		}
	}
	if calls != 2 {
		t.Fatal("invalid batch read private facts")
	}
}
