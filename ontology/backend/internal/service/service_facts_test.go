package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/semantic"
)

func outdoorRuleFixture(d semantic.Definition) semantic.Definition {
	d.Properties = []semantic.Property{
		{ID: "activity_status", ClassID: "activity", Key: "status", Name: "活动状态", Kind: semantic.String},
		{ID: "activity_date_eligible", ClassID: "activity", Key: "date_eligible", Name: "活动日期不晚于观察日", Kind: semantic.Boolean},
	}
	d.Rules = []semantic.Rule{{
		ID: "statistically_valid_activity", ClassID: "activity",
		Expression: `date_eligible && status != '拟定中' && status != '已取消'`,
		Basis:      "《Outdoor业务语义》统计有效活动及《Outdoor数据治理与指标实现规范》治理事实口径",
		Inputs: []semantic.Input{
			{Variable: "date_eligible", PropertyID: "activity_date_eligible", OnAbsent: semantic.AbsenceFalse},
			{Variable: "status", PropertyID: "activity_status", OnAbsent: semantic.AbsenceUnknown},
		},
		ServiceBinding: &semantic.ServiceBinding{
			ServiceName: "private-activity", ServiceID: 42, ServiceVersion: strings.Repeat("a", 64),
			IDParameter: "activity_id", IDField: "activity_id", TimeZone: "Asia/Shanghai",
			Inputs: []semantic.ServiceInput{
				{Variable: "date_eligible", Field: "activity_date", Conversion: semantic.ServiceDateOnOrBefore},
				{Variable: "status", Field: "activity_status", Conversion: semantic.ServiceString},
			},
		},
	}}
	return d
}

func testPublishedOutdoorRule(t *testing.T, s *RevisionService, actor models.Actor, d semantic.Definition) {
	t.Helper()
	ctx := context.Background()
	rule := d.Rules[0]
	b := rule.ServiceBinding
	active, err := s.ListClasses(ctx, actor, d.Scope.OntologyID)
	if err != nil {
		t.Fatal(err)
	}
	class, err := s.ClassContext(ctx, actor, d.Scope.OntologyID, rule.ClassID, active.Revision, active.Generation, active.ActivationVersion)
	if err != nil || len(class.Rules) != 1 || !class.Rules[0].BusinessEvaluable {
		t.Fatalf("rule evaluation availability not projected: %v", err)
	}
	encoded, err := json.Marshal(class)
	if err != nil || strings.Contains(string(encoded), b.ServiceName) || strings.Contains(string(encoded), "service_binding") {
		t.Fatalf("semantic context exposed a physical binding: %v", err)
	}
	t.Run("delegated_owner_fact_request", func(t *testing.T) {
		call := authorization.OntologyRuleCall{AgentRunID: "run", ToolCallID: "call", PrincipalID: fmt.Sprint(actor.PrincipalID),
			Evaluation: authorization.OntologyRuleEvaluation{OntologyID: d.Scope.OntologyID, Revision: active.Revision, Generation: active.Generation,
				ActivationVersion: active.ActivationVersion, RuleID: rule.ID, ObjectID: "fixture"}}
		for _, status := range []int{http.StatusOK, http.StatusForbidden} {
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request authorization.OntologyRuleFactRequest
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/service/runtime/ontology-facts/private-activity" ||
					r.Header.Get("Authorization") != "Bearer addp_at_ontology_machine" || json.NewDecoder(r.Body).Decode(&request) != nil ||
					request.Call != call || request.ServiceID != b.ServiceID || request.ServiceVersion != b.ServiceVersion || request.IDParameter != b.IDParameter ||
					!slices.Equal(request.Fields, []string{"activity_id", "activity_date", "activity_status"}) {
					t.Error("delegated fact request lost its binding or machine identity")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprintf(w, `{"fact_status":"verified","service_version":%q,"page":{"limit":2,"has_more":false},"data":[{"activity_id":"fixture","activity_status":"报名截止","activity_date":"2000-01-02"}]}`, b.ServiceVersion)
			}))
			reader, err := NewServiceFactReader(owner.URL)
			if err != nil {
				t.Fatal(err)
			}
			reader.SetServiceTokens(fixtureOntologyFactTokens{})
			s.SetServiceFactReader(reader)
			got, err := s.EvaluateBusiness(WithOntologyRuleCall(ctx, call), actor, active.OntologyID, rule.ID, active.Revision, active.Generation, active.ActivationVersion, "fixture", "")
			owner.Close()
			s.SetServiceFactReader(nil)
			if status == http.StatusForbidden && err != ErrOntologyFactPermission || status == http.StatusOK && (err != nil || got.Decision.Outcome != semantic.Matched) {
				t.Fatalf("delegated fact evaluation: %d %+v %v", status, got, err)
			}
		}
	})
	for _, tc := range []struct {
		name, factStatus, rows string
		hasMore                bool
		versionMismatch        bool
		outcome                semantic.Outcome
	}{
		{"historical", "verified", `[{"activity_id":"fixture","activity_status":"报名截止","activity_date":"2000-01-02T00:00:00Z"}]`, false, false, semantic.Matched},
		{"draft", "verified", `[{"activity_id":"fixture","activity_status":"拟定中","activity_date":"2000-01-02"}]`, false, false, semantic.NotMatched},
		{"cancelled", "verified", `[{"activity_id":"fixture","activity_status":"已取消","activity_date":"2000-01-02"}]`, false, false, semantic.NotMatched},
		{"null_date", "verified", `[{"activity_id":"fixture","activity_status":"已发布","activity_date":null}]`, false, false, semantic.NotMatched},
		{"future_date", "verified", `[{"activity_id":"fixture","activity_status":"已发布","activity_date":"2999-01-02"}]`, false, false, semantic.NotMatched},
		{"protected_date", "unknown", `[]`, false, false, semantic.Undetermined},
		{"masked_draft", "unknown", `[{"activity_id":"fixture","activity_status":"draft","activity_date":"2000-01-02"}]`, false, false, semantic.Undetermined},
		{"missing_date", "verified", `[{"activity_id":"fixture","activity_status":"已发布"}]`, false, false, semantic.Undetermined},
		{"other_id", "verified", `[{"activity_id":"other","activity_status":"已发布","activity_date":"2000-01-02"}]`, false, false, semantic.Undetermined},
		{"duplicate", "verified", `[{"activity_id":"fixture","activity_status":"已发布","activity_date":"2000-01-02"},{"activity_id":"fixture","activity_status":"已发布","activity_date":"2000-01-02"}]`, false, false, semantic.Undetermined},
		{"more", "verified", `[{"activity_id":"fixture","activity_status":"已发布","activity_date":"2000-01-02"}]`, true, false, semantic.Undetermined},
		{"drift", "verified", `[{"activity_id":"fixture","activity_status":"已发布","activity_date":"2000-01-02"}]`, false, true, semantic.Undetermined},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Parameters             map[string]string `json:"parameters"`
					Select                 []string          `json:"select"`
					Page                   map[string]int    `json:"page"`
					RequireUntransformed   bool              `json:"require_untransformed"`
					ExpectedServiceID      uint64            `json:"expected_service_id"`
					ExpectedServiceVersion string            `json:"expected_service_version"`
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/query/private-activity/query" || r.Header.Get("Authorization") != "Bearer addp_at_fixture" ||
					json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Parameters) != 1 || request.Parameters["activity_id"] != "fixture" ||
					!slices.Equal(request.Select, []string{"activity_id", "activity_date", "activity_status"}) || request.Page["limit"] != 2 ||
					!request.RequireUntransformed || request.ExpectedServiceID != b.ServiceID || request.ExpectedServiceVersion != b.ServiceVersion {
					t.Error("the request lost the published binding or current user identity")
					w.WriteHeader(http.StatusForbidden)
					return
				}
				version := b.ServiceVersion
				if tc.versionMismatch {
					version = strings.Repeat("b", 64)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"fact_status":%q,"service_version":%q,"page":{"limit":2,"has_more":%t},"data":%s}`, tc.factStatus, version, tc.hasMore, tc.rows)
			}))
			defer owner.Close()
			reader, err := NewServiceFactReader(owner.URL)
			if err != nil {
				t.Fatal(err)
			}
			s.SetServiceFactReader(reader)
			t.Cleanup(func() { s.SetServiceFactReader(nil) })
			got, err := s.EvaluateBusiness(ctx, actor, active.OntologyID, rule.ID, active.Revision, active.Generation, active.ActivationVersion, "fixture", "addp_at_fixture")
			if err != nil || got.Decision.Outcome != tc.outcome || got.Decision.Mode != "business" || got.Digest != active.Digest ||
				got.ServiceVersion != b.ServiceVersion || got.ObservationDate == "" {
				t.Fatalf("business evaluation: %+v %v", got, err)
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "2000-01-02") || strings.Contains(string(data), "报名截止") || strings.Contains(string(data), `"draft"`) {
				t.Fatal("raw Service values leaked to the rule caller")
			}
		})
	}
}

type fixtureOntologyFactTokens struct{}

func (fixtureOntologyFactTokens) TenantServiceAccessToken(context.Context, uint) (string, error) {
	return "addp_at_ontology_machine", nil
}
