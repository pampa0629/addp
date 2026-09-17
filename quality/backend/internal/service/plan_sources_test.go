package service

import (
	"context"
	"encoding/json"
	"errors"
	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStandardSourceConstraintsAndRevision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"element_id":3,"element_revision_id":31,"revision_no":3,"quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[{"rule_key":"00000000-0000-4000-8000-000000000001","type":"length","severity":"error","enabled":true,"message":"","params":{"min":1,"max":64}}]}}`))
	}))
	defer server.Close()
	svc := &RuleService{standardClient: commonClient.NewStandardClient(server.URL, qualityCatalogTokenSource("service-token"), server.Client())}
	source := &RuleSource{ElementID: 3, ElementRevisionID: 31, RuleKey: "00000000-0000-4000-8000-000000000001"}
	rule := PlanRule{RuleKey: "00000000-0000-4000-8000-000000000002", Type: "length", Severity: "warning", Source: source, Params: json.RawMessage(`{"column":"id","table":"customers","constraint":{"max":64,"min":1}}`)}
	if err := svc.validateStandardSource(context.Background(), 7, rule); err != nil {
		t.Fatal(err)
	}
	rule.Params = json.RawMessage(`{"column":"id","table":"customers","constraint":{"max":65,"min":1}}`)
	if err := svc.validateStandardSource(context.Background(), 7, rule); !errors.Is(err, commonAPI.ErrBadRequest) {
		t.Fatalf("modified source constraint accepted: %v", err)
	}
	source.ElementRevisionID = 30
	if err := svc.validateStandardSource(context.Background(), 7, rule); !errors.Is(err, commonAPI.ErrConflict) {
		t.Fatalf("stale source accepted: %v", err)
	}
}

func TestElementCandidatesAllowBrowsingAndForwardPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/standard/elements" || r.URL.Query().Get("keyword") != "" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("page_size") != "30" || r.URL.Query().Get("status") != "published" {
			t.Errorf("unexpected candidate request: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":3,"code":"member_status","current_revision":{"id":31,"revision_no":3,"status":"published","name":"Member status","compiled_quality_rules":{"schema_version":"addp.quality.rules/v1","rules":[]}}}],"total":31}`))
	}))
	defer server.Close()
	svc := NewRuleService(nil, commonClient.NewStandardClient(server.URL, qualityCatalogTokenSource("service-token"), server.Client()))
	items, total, err := svc.ListElementCandidates(context.Background(), 7, "  ", 2, 30)
	if err != nil || total != 31 || len(items) != 1 || items[0].RevisionID != 31 {
		t.Fatalf("browse = %#v, %d, %v", items, total, err)
	}
}
