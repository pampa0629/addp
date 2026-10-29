package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/client"
	"github.com/addp/common/events"
)

type cleanupIndexToken struct{}

func (cleanupIndexToken) Token(context.Context, uint) (string, error) { return "fixture", nil }
func (cleanupIndexToken) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform prohibited")
}

func TestCleanupIndexScopeUsesExactEngineFactsAndConfirmedInventory(t *testing.T) {
	for _, mode := range []string{"missing", "deleting", "active", "forbidden", "unavailable", "wrong_identity", "changed_inventory", "changed_engine", "changed_tenant"} {
		t.Run(mode, func(t *testing.T) {
			docID := "orphan-projection"
			search, db := contentDeliveryServiceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/indexes/content/documents/fetch" || r.Method != "POST" {
					t.Error("unexpected provider mutation")
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"offset": 0, "limit": 256, "total": 1, "results": []any{map[string]any{"id": docID, "document_id": docID, "tenant_id": 7, "engine_id": 9}}})
			}))
			lifecycle := "deleting"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/system/engines/9" || r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("must read exact Engine with runtime identity")
					w.WriteHeader(403)
					return
				}
				switch mode {
				case "missing":
					w.WriteHeader(404)
					return
				case "forbidden":
					w.WriteHeader(403)
					return
				case "unavailable":
					w.WriteHeader(503)
					return
				case "active":
					lifecycle = "active"
				}
				id := 9
				if mode == "wrong_identity" {
					id = 10
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "tenant_id": 7, "lifecycle_state": lifecycle, "capabilities": map[string]any{"schema_version": "engine.capabilities/v1", "engine_type": "test", "engine_family": "test", "storage": map[string]any{}}})
			}))
			defer server.Close()
			svc := &CleanupService{db: db, search: search, systemClient: client.NewSystemServiceClient(server.URL, cleanupIndexToken{}, server.Client())}
			input := events.CleanupRequest{RequestID: "scan-fixture", TenantID: 7, RequestedBy: 7, Action: events.CleanupActionScan}
			plan, _, err := svc.assessCleanupIndex(t.Context(), input)
			if mode == "forbidden" || mode == "unavailable" || mode == "wrong_identity" {
				if err == nil {
					t.Fatal("unavailable fact interpreted as missing")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode != "active" && (plan.CandidateCount != 1 || len(plan.EngineIDs) != 1 || plan.EngineIDs[0] != 9) {
				t.Fatal("projection without Meta row not independently discovered")
			}
			if mode == "active" && plan.CandidateCount != 0 {
				t.Fatal("active Engine selected")
			}
			if err := db.Create(plan).Error; err != nil {
				t.Fatal(err)
			}
			input.Action = events.CleanupActionExecute
			input.BasedOnScan = input.RequestID
			input.RequestID = "execute-fixture"
			switch mode {
			case "changed_inventory":
				docID = "replacement"
			case "changed_engine":
				lifecycle = "active"
			case "changed_tenant":
				input.TenantID = 8
			}
			_, _, err = svc.prepareCleanupIndex(t.Context(), &cleanup.Run{Input: input})
			changed := mode == "changed_inventory" || mode == "changed_engine" || mode == "changed_tenant"
			if changed && err == nil {
				t.Fatal("changed confirmed scope accepted")
			}
			if !changed && err != nil {
				t.Fatal(err)
			}
		})
	}
}
