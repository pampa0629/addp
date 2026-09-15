package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	commonclient "github.com/addp/common/client"
)

type mermaidStandardFixture struct {
	ID   int64
	Code string
	Name string
}

func newMermaidStandardClient(t *testing.T, tenantID int64, domains, elements []mermaidStandardFixture) *commonclient.StandardClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/standard/references/resolve" {
			var request struct {
				References []struct {
					ObjectType string `json:"object_type"`
					ID         int64  `json:"id"`
					Code       string `json:"code"`
				} `json:"references"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode Standard reference request: %v", err)
			}
			results := make([]map[string]any, 0, len(request.References))
			for _, reference := range request.References {
				fixtures := domains
				if reference.ObjectType == "element" {
					fixtures = elements
				}
				result := map[string]any{"object_type": reference.ObjectType, "id": reference.ID, "code": reference.Code, "found": false, "referenceable": false}
				for _, fixture := range fixtures {
					if (reference.ID > 0 && fixture.ID == reference.ID) || (reference.Code != "" && fixture.Code == reference.Code) {
						result = map[string]any{
							"object_type": reference.ObjectType, "id": fixture.ID, "code": fixture.Code,
							"name": fixture.Name, "found": true, "referenceable": true,
							"status": "active", "lifecycle_state": "active", "version": 1,
						}
						if reference.ObjectType == "element" {
							result["status"] = "published"
							result["revision_id"] = fixture.ID + 1000
							result["revision_no"] = 1
						}
						break
					}
				}
				results = append(results, result)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
			return
		}
		for _, fixture := range domains {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/standard/domains/"+strconv.FormatInt(fixture.ID, 10) {
				_ = json.NewEncoder(w).Encode(map[string]any{"id": fixture.ID, "tenant_id": tenantID, "code": fixture.Code, "name": fixture.Name, "lifecycle_state": "active"})
				return
			}
		}
		for _, fixture := range elements {
			if r.Method == http.MethodGet && r.URL.Path == "/api/v1/standard/elements/"+strconv.FormatInt(fixture.ID, 10) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id": fixture.ID, "tenant_id": tenantID, "code": fixture.Code, "lifecycle_state": "active",
					"current_revision": map[string]any{"id": fixture.ID + 1000, "revision_no": 1, "status": "published", "name": fixture.Name, "data_type": "string"},
				})
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	return commonclient.NewStandardClient(server.URL, commonclient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "tenant-token-" + strings.TrimSpace(strconv.FormatInt(tenantID, 10)), nil
	}), server.Client())
}
