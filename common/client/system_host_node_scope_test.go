package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestUserHostNodeScopeRejectsPartialInventoryAndInvalidIdentity(t *testing.T) {
	node := map[string]any{"node_id": uuid.NewString(), "display_name": "节点", "node_kind": "virtual", "version": 3, "enabled": false, "addresses": []string{"private"}}
	for _, tc := range []string{"valid", "empty", "boundary", "partial", "wrong-count", "missing-total", "missing-enabled", "duplicate", "wrong-kind", "denied", "revoked", "redirect"} {
		t.Run(tc, func(t *testing.T) {
			data := []map[string]any{node}
			total := 1
			pages := 1
			status := 200
			copyNode := func() map[string]any {
				n := map[string]any{}
				for k, v := range node {
					n[k] = v
				}
				return n
			}
			switch tc {
			case "empty":
				data = []map[string]any{}
				total = 0
				pages = 0
			case "partial", "boundary":
				total = 101
				pages = 2
				if tc == "boundary" {
					total = 100
					pages = 1
				}
				data = []map[string]any{}
				for i := 0; i < 100; i++ {
					n := copyNode()
					n["node_id"] = uuid.NewString()
					data = append(data, n)
				}
			case "wrong-count":
				total = 2
			case "missing-enabled":
				n := copyNode()
				delete(n, "enabled")
				data = []map[string]any{n}
			case "duplicate":
				data = append(data, node)
				total = 2
			case "wrong-kind":
				n := copyNode()
				n["node_kind"] = "container"
				data = []map[string]any{n}
			case "denied":
				status = 403
			case "revoked":
				status = 401
			case "redirect":
				status = 302
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/system/platform/host_nodes" || r.URL.RawQuery != "page=1&page_size=100" || r.Header.Get("Authorization") != "Bearer addp_at_current_user" {
					t.Error("wrong owner scope or credential")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				body := map[string]any{"data": data, "total": total, "page": 1, "page_size": 100, "total_pages": pages}
				if tc == "missing-total" {
					delete(body, "total")
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			rows, err := NewSystemServiceClient(server.URL, nil, server.Client()).HostNodesForUser(context.Background(), "addp_at_current_user")
			valid := tc == "valid" || tc == "empty" || tc == "boundary"
			if (err == nil) != valid || calls != 1 {
				t.Fatalf("rows=%+v err=%v calls=%d", rows, err, calls)
			}
			if tc == "partial" && !errors.Is(err, ErrHostNodeScopeBudget) {
				t.Fatal("partial scope not budget failure", err)
			}
			if tc == "valid" && (rows[0].Enabled || rows[0].Version != 3 || rows[0].DisplayName != "节点") {
				t.Fatal("owner facts changed", rows)
			}
			if status == 401 || status == 403 {
				code, ok := SystemAPIStatusCode(err)
				if !ok || code != status {
					t.Fatal("denial lost", err)
				}
			}
		})
	}
}
