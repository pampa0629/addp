package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
)

func TestMetaNodeIdentityIsBoundedBoundAndOneAttempt(t *testing.T) {
	query := authorizationfacts.NodeIdentityQuery{Target: plugin.TabularNamespacePath(9, "schema", "quoted.name/ ")}
	body, _ := json.Marshal(authorizationfacts.NodeIdentity{TenantID: 7, Target: query.Target, Exists: true})
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"found", string(body), 200, true},
		{"absent", strings.Replace(string(body), `"exists":true`, `"exists":false`, 1), 200, true},
		{"wrong tenant", strings.Replace(string(body), `"tenant_id":7`, `"tenant_id":8`, 1), 200, false},
		{"wrong engine", strings.Replace(string(body), `"engine_id":9`, `"engine_id":10`, 1), 200, false},
		{"unknown column", strings.Replace(string(body), `"exists":true`, `"exists":true,"attributes":{}`, 1), 200, false},
		{"duplicate key", strings.Replace(string(body), `"exists":true`, `"exists":true,"exists":true`, 1), 200, false},
		{"missing exists", strings.Replace(string(body), `,"exists":true`, ``, 1), 200, false},
		{"null exists", strings.Replace(string(body), `"exists":true`, `"exists":null`, 1), 200, false},
		{"trailing body", string(body) + `{}`, 200, false},
		{"oversize", strings.Repeat(" ", authorizationfacts.MaxNodeIdentityBytes) + string(body), 200, false},
		{"unauthorized", `{"error_code":"token_expired"}`, 401, false},
		{"unavailable", `{"error":"private-sentinel"}`, 503, false},
		{"redirect", `{}`, 302, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/api/v1/meta/runtime/node-identities/resolve" || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("wrong node identity request")
				}
				var got authorizationfacts.NodeIdentityQuery
				if json.NewDecoder(r.Body).Decode(&got) != nil || got.Validate() != nil {
					t.Error("invalid outbound typed target")
				}
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			source := &identityQueryTokenSource{}
			result, err := NewMetaClient(server.URL, source).ResolveNodeIdentity(context.Background(), 7, query)
			if (err == nil) != tc.ok || calls != 1 || source.calls != 1 || source.invalidations != 0 {
				t.Fatalf("result=%+v err=%v calls=%d source=%+v", result, err, calls, source)
			}
			if err != nil && strings.Contains(err.Error(), "private-sentinel") {
				t.Fatal("private owner diagnostic leaked")
			}
		})
	}
}

func TestMetaChildNodeIdentityQueryRejectsUnboundOrMalformedPages(t *testing.T) {
	query := authorizationfacts.NodeIdentityPageQuery{Target: plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), 9), Page: 1, PageSize: 100}
	page := authorizationfacts.NodeIdentityPage{Parent: authorizationfacts.NodeIdentity{TenantID: 7, Target: query.Target, Exists: true}, Nodes: []authorizationfacts.NodeIdentity{{TenantID: 7, Target: plugin.TabularNamespacePath(9, "schema", "quoted.name/ "), Exists: true}}, Total: 1, Page: 1, PageSize: 100}
	raw, _ := json.Marshal(page)
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", string(raw), 200, true},
		{"foreign engine", strings.ReplaceAll(string(raw), `"engine_id":9`, `"engine_id":10`), 200, false},
		{"foreign tenant", strings.ReplaceAll(string(raw), `"tenant_id":7`, `"tenant_id":8`), 200, false},
		{"absent child", strings.ReplaceAll(string(raw), `"exists":true`, `"exists":false`), 200, false},
		{"missing existence", strings.ReplaceAll(string(raw), `,"exists":true`, ``), 200, false},
		{"null existence", strings.ReplaceAll(string(raw), `"exists":true`, `"exists":null`), 200, false},
		{"unknown fact", strings.Replace(string(raw), `"exists":true`, `"exists":true,"attributes":{}`, 1), 200, false},
		{"wrong total", strings.Replace(string(raw), `"total":1`, `"total":2`, 1), 200, false},
		{"wrong page", strings.Replace(string(raw), `"page":1`, `"page":2`, 1), 200, false},
		{"oversize", strings.Repeat(" ", authorizationfacts.MaxNodeIdentityPageBytes+1), 200, false},
		{"redirect", `{}`, 302, false}, {"expired token", `{}`, 401, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/meta/runtime/node-identities/query" || r.Method != "POST" || r.URL.RawQuery != "" {
					t.Error("wrong child query route")
				}
				w.Header().Set("Location", "/other")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			source := &identityQueryTokenSource{}
			result, err := NewMetaClient(server.URL, source).QueryNodeIdentities(context.Background(), 7, query)
			if (err == nil) != tc.ok || calls != 1 || source.invalidations != 0 {
				t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
			}
		})
	}
}
