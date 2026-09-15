package service

import (
	"context"
	"encoding/json"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	commonquery "github.com/addp/common/query"
	"github.com/addp/service/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricServiceBindingIsExactAndCannotOverrideCompiledFormula(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql"} {
		t.Run(engineType, func(t *testing.T) { testMetricServiceBinding(t, engineType) })
	}
}
func testMetricServiceBinding(t *testing.T, engineType string) {
	dialect, err := commonquery.NewAnalyticalDialect(engineType)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	plan := commonclient.ModelMetricPlan{ImplementationID: 3, RevisionID: 7, MetricDefinitionID: 9, MetricDefinitionRevisionID: 19, EngineID: 2, DependencyHash: strings.Repeat("a", 64), SQL: "SELECT :subject_id AS subject_id,CAST(:start_date AS date) AS bucket," + dialect.Integer("1") + " AS value WHERE :grain = 'total' AND CAST(:end_date AS date)>CAST(:start_date AS date)"}
	plan.Parameters = []commonclient.ModelMetricParameter{{Name: "subject_id", Type: datatype.FieldTypeString, Required: true}, {Name: "start_date", Type: datatype.FieldTypeDate, Required: true}, {Name: "end_date", Type: datatype.FieldTypeDate, Required: true}, {Name: "grain", Type: datatype.FieldTypeString, Required: true}}
	plan.Parameters[3].Options = []commonquery.ParameterOption{{Value: "total", Labels: map[string]string{"zh-cn": "全期", "en": "Total"}}}
	plan.Fields = []datatype.FieldInfo{{Name: "subject_id", Type: datatype.FieldTypeString}, {Name: "bucket", Type: datatype.FieldTypeDate}, {Name: "value", Type: datatype.FieldTypeBigInt}}
	plan.StableKey = []string{"subject_id", "bucket"}
	withdrawn := false
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/plan") {
			calls++
			if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/3/revisions/7/plan") {
				t.Errorf("unexpected plan route %s", r.URL.Path)
			}
			if withdrawn {
				http.Error(w, "withdrawn", http.StatusConflict)
				return
			}
			_ = json.NewEncoder(w).Encode(plan)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": 2, "engine_type": engineType})
	}))
	defer server.Close()
	tokens := commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenant uint) (string, error) {
		if tenant != 7 {
			t.Errorf("wrong tenant %d", tenant)
		}
		return "addp_at_test", nil
	})
	owner := commonclient.NewModelClient(server.URL, tokens, server.Client())
	svc := NewQueryServiceService(nil, commonclient.NewSystemClient(server.URL, tokens), nil, "")
	svc.SetModelClient(owner)
	req := &models.CreateQueryServiceRequest{ConfigType: "sql", MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 7}}
	binding, err := svc.resolveMetricSource(ctx, req, 7)
	if err != nil {
		t.Fatal(err)
	}
	if req.SqlQuery != plan.SQL || binding.RevisionID != 7 || len(req.NamedParameters) != 4 {
		t.Fatalf("bad binding: %#v %#v", req, binding)
	}
	snapshot := buildSQLDependencySnapshot(req.SqlQuery, req.OutputContract, time.Now())
	snapshot.MetricSource = binding
	service := &models.QueryService{TenantID: 7, ConfigType: "sql", EngineID: req.EngineID, SqlQuery: req.SqlQuery, NamedParameters: req.NamedParameters, DataConfig: models.JSONB{"stable_key": plan.StableKey, models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}}
	executor := &QueryExecutorService{}
	executor.SetModelClient(owner)
	input := map[string]interface{}{"subject_id": "A", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "month"}
	if err := executor.validateMetricSource(ctx, service, input); err != nil {
		t.Fatal(err)
	}
	plan.Parameters[3].Options[0].Labels["en"] = "Changed"
	if err := executor.validateMetricSource(ctx, service, input); err == nil {
		t.Fatal("changed enum labels executed without rebind")
	}
	plan.Parameters[3].Options[0].Labels["en"] = "Total"
	original := plan.DependencyHash
	plan.DependencyHash = strings.Repeat("b", 64)
	if err := executor.validateMetricSource(ctx, service, input); err == nil {
		t.Fatal("dependency drift accepted")
	}
	plan.DependencyHash = original
	withdrawn = true
	if err := executor.validateMetricSource(ctx, service, input); err == nil {
		t.Fatal("withdrawn publication executed")
	}
	withdrawn = false
	input["sql"] = "select secret"
	before := calls
	if err := executor.validateMetricSource(ctx, service, input); err == nil || calls != before {
		t.Fatal("undeclared parameter reached plan provider")
	}
	for _, kind := range []string{"sql", "engine", "parameters", "output", "data"} {
		t.Run(kind, func(t *testing.T) {
			r := &models.CreateQueryServiceRequest{ConfigType: "sql", MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 7}}
			switch kind {
			case "sql":
				r.SqlQuery = "select 1"
			case "engine":
				id := uint(4)
				r.EngineID = &id
			case "parameters":
				r.NamedParameters = req.NamedParameters
			case "output":
				r.OutputContract = req.OutputContract
			case "data":
				r.DataConfig = map[string]interface{}{"where": "true"}
			}
			if _, err := svc.resolveMetricSource(ctx, r, 7); err == nil {
				t.Fatal("compiled origin accepted caller override")
			}
		})
	}
}
