package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
)

func metaScanClientFacts() execution.MetaScanPlanFacts {
	return execution.MetaScanPlanFacts{SchemaVersion: execution.MetaScanPlanFactsSchemaV1, ExecutionID: "23dd4f40-95b5-4cc1-a294-ded9f78d10a2", EngineID: 9, EngineVersion: "2", ExecutionPlanDigest: strings.Repeat("a", 64), ScanDepth: "basic", SourceScope: &execution.MetaScanSourceScope{Mode: "exact", Anchors: []plugin.EngineCatalogPath{plugin.TabularItemPath(9, "schema", "public", "people")}}, BasisState: authorizationfacts.AuthorizationBasisComplete}
}

type metaScanTokenProbe struct {
	calls, invalidations int
	tenant               uint
	failure              bool
}

func (p *metaScanTokenProbe) Token(_ context.Context, tenant uint) (string, error) {
	p.calls++
	p.tenant = tenant
	if p.failure {
		return "", errors.New("secret-token-endpoint")
	}
	return "test-token", nil
}
func (p *metaScanTokenProbe) InvalidateToken(uint, string) { p.invalidations++ }

func TestReadScanPlanFactsStrictSingleRequest(t *testing.T) {
	facts := metaScanClientFacts()
	raw, _ := json.Marshal(facts)
	for _, test := range []struct {
		name    string
		status  int
		body    string
		success bool
	}{
		{"complete", 200, string(raw), true},
		{"wrong identity", 200, strings.Replace(string(raw), facts.ExecutionID, "979a32a5-c900-4b57-ad24-d91a9d310888", 1), false},
		{"duplicate", 200, strings.Replace(string(raw), `"read_options":{}`, `"read_options":{},"read_options":{}`, 1), false},
		{"unknown nested", 200, strings.Replace(string(raw), `"read_options":{}`, `"read_options":{"token":"secret-body"}`, 1), false},
		{"oversized", 200, strings.Repeat(" ", execution.MaxExecutionConfigBytes) + string(raw), false},
		{"unauthorized", 401, `{"error_code":"secret-body"}`, false},
		{"missing", 404, `{"error":"secret-body"}`, false},
		{"unexpected success", 201, string(raw), false},
		{"trailing JSON", 200, string(raw) + `{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			tokens := &metaScanTokenProbe{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/meta/runtime/scan-executions/"+facts.ExecutionID+"/authorization-facts" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-Tenant-ID") != "" || r.ContentLength != 0 {
					t.Error("request escaped fixed contract")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			value, err := NewMetaClient(server.URL, tokens).WithTenantID(7).ReadScanPlanFacts(context.Background(), 7, facts.ExecutionID)
			if (err == nil) != test.success || calls != 1 || tokens.calls != 1 || tokens.tenant != 7 || tokens.invalidations != 0 {
				t.Fatalf("value=%v err=%v calls=%d token=%+v", value, err, calls, tokens)
			}
			if err != nil && strings.Contains(err.Error(), "secret-body") {
				t.Fatal("upstream body leaked")
			}
		})
	}
}

func TestReadScanPlanFactsDoesNotRedirectOrChangeTenant(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Redirect(w, r, "/other", 302) }))
	defer server.Close()
	tokens := &metaScanTokenProbe{}
	c := NewMetaClient(server.URL, tokens).WithTenantID(7)
	_, err := c.ReadScanPlanFacts(context.Background(), 7, metaScanClientFacts().ExecutionID)
	var apiErr *TenantAPIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 302 || calls != 1 {
		t.Fatal("redirect followed", err, calls)
	}
	for _, input := range []struct {
		tenant uint
		id     string
	}{{8, metaScanClientFacts().ExecutionID}, {0, metaScanClientFacts().ExecutionID}, {7, "../other"}} {
		if _, err := c.ReadScanPlanFacts(context.Background(), input.tenant, input.id); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if calls != 1 || tokens.calls != 1 {
		t.Fatal("invalid requests performed IO")
	}
	tokens.failure = true
	if _, err := c.ReadScanPlanFacts(context.Background(), 7, metaScanClientFacts().ExecutionID); err == nil || strings.Contains(err.Error(), "secret-token-endpoint") {
		t.Fatal("credential error leaked", err)
	}
}
