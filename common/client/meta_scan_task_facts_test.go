package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addp/common/execution"
)

func TestReadScanTaskFactsStrictSingleRequest(t *testing.T) {
	facts := execution.MetaScanTaskFacts{SchemaVersion: execution.MetaScanTaskFactsSchemaV1, TaskID: "42", TaskDefinitionDigest: strings.Repeat("a", 64), EngineID: 9, ScopeKind: "engine", ScanDepth: "basic"}
	raw, _ := json.Marshal(facts)
	for _, tc := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{"current task", 200, string(raw), true},
		{"wrong task", 200, strings.Replace(string(raw), `"42"`, `"43"`, 1), false},
		{"unknown", 200, strings.Replace(string(raw), `"read_options":{}`, `"read_options":{"token":"secret"}`, 1), false},
		{"oversized", 200, strings.Repeat(" ", execution.MaxExecutionConfigBytes) + string(raw), false},
		{"unauthorized", 401, `{"error":"secret"}`, false},
		{"missing", 404, `{"error":"secret"}`, false},
		{"unavailable", 409, `{"error":"secret"}`, false},
		{"redirect", 307, `{"error":"secret"}`, false},
		{"unexpected success", 201, string(raw), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/v1/meta/runtime/scan-tasks/42/authorization-facts" || r.URL.RawQuery != "" || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("request escaped fixed contract")
				}
				if tc.status == 307 {
					w.Header().Set("Location", "/must-not-follow")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			tokens := &metaScanTokenProbe{}
			got, err := NewMetaClient(server.URL, tokens).WithTenantID(7).ReadScanTaskFacts(context.Background(), 7, "42")
			if (err == nil) != tc.ok || calls != 1 || tokens.calls != 1 || tokens.invalidations != 0 || tokens.tenant != 7 {
				t.Fatalf("facts=%+v err=%v calls=%d tokens=%+v", got, err, calls, tokens)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("error body leaked")
			}
		})
	}
	for _, id := range []string{"0", "01", "-1", "42/other", "9223372036854775808"} {
		tokens := &metaScanTokenProbe{}
		if _, err := NewMetaClient("http://invalid", tokens).ReadScanTaskFacts(context.Background(), 7, id); err == nil || tokens.calls != 0 {
			t.Fatal("invalid task contacted upstream", id, err)
		}
	}
}
