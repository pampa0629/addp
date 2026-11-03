package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
)

func TestMetadataRefreshClientBindsIdentityScopeAndSingleRequest(t *testing.T) {
	query := authorization.MetadataRefreshQuery{Target: plugin.TabularNamespacePath(9, "schema", "public"), Kind: "node"}
	for _, scenario := range []string{"ok", "wrong target", "empty", "unknown field", "forbidden", "redirect"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/system/engine-access/metadata-refresh-scopes/query" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer addp_at_current" {
					t.Error("wrong credential or purpose")
				}
				binding := factstest.Binding(plugin.TabularItemPath(9, "schema", "public", "people"))
				result := authorization.MetadataRefreshScope{TenantID: 7, EngineVersion: "1", Query: query, Operator: authorization.SharingFulfillmentOperator{PrincipalID: 3, MembershipID: 4, AuthorizationVersion: 1}, ObservedAt: time.Now(), Ranges: []authorization.MetadataRefreshRange{{Item: &binding}}}
				switch scenario {
				case "wrong target":
					result.Query.Target.Segments = append([]plugin.EngineCatalogSegment(nil), result.Query.Target.Segments...)
					result.Query.Target.Segments[1].Name = "other"
				case "empty":
					result.Ranges = nil
				case "forbidden":
					w.WriteHeader(403)
				case "redirect":
					w.Header().Set("Location", "/redirect")
					w.WriteHeader(307)
				}
				raw, _ := json.Marshal(result)
				if scenario == "unknown field" {
					raw = append(raw[:len(raw)-1], []byte(`,"allow":true}`)...)
				}
				_, _ = w.Write(raw)
			}))
			defer server.Close()
			c := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("must-not-use"), server.Client())
			got, err := c.GetMetadataRefreshScope(t.Context(), "addp_at_current", query)
			if calls != 1 || ((scenario == "ok") != (err == nil)) || (err != nil && got != nil) {
				t.Fatalf("scope=%+v calls=%d err=%v", got, calls, err)
			}
			if _, err := c.GetMetadataRefreshScope(t.Context(), "addp_dat_tool", query); err == nil || calls != 1 {
				t.Fatal("delegate credential used for scanning")
			}
		})
	}
}
