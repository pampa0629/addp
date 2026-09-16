package service

import (
	"context"
	"encoding/json"
	commonclient "github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/mysql"
	"github.com/addp/common/engine/plugins/postgresql"
	commonmodels "github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
	queryplan "github.com/addp/common/query/plan"
	"github.com/addp/service/internal/models"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricServiceBindingIsExactAndCannotOverrideCompiledFormula(t *testing.T) {
	for _, engineType := range []string{"postgresql", "mysql", "metric_test_extension"} {
		t.Run(engineType, func(t *testing.T) { testMetricServiceBinding(t, engineType) })
	}
}
func testMetricServiceBinding(t *testing.T, engineType string) {
	ctx := context.Background()
	plan, engine := metricServiceFixture(t, engineType)
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
		_ = json.NewEncoder(w).Encode(engine)
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
	req := &models.CreateQueryServiceRequest{ConfigType: "analytical", MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 7}}
	binding, err := svc.resolveMetricSource(ctx, req, 7)
	if err != nil {
		t.Fatal(err)
	}
	if req.SqlQuery != "" || binding.RevisionID != 7 || len(req.NamedParameters) != 0 {
		t.Fatalf("bad binding: %#v %#v", req, binding)
	}
	snapshot := &models.QueryServiceDependencySnapshot{CapturedAt: time.Now(), MetricSource: binding}
	snapshot.DependencyHash = queryServiceDependencyHash(snapshot)
	service := &models.QueryService{TenantID: 7, ConfigType: "analytical", DataConfig: models.JSONB{models.QueryServiceSourceSnapshotKey: queryServiceSnapshotPayload(snapshot)}}
	executor := &QueryExecutorService{}
	executor.SetModelClient(owner)
	input := map[string]interface{}{"subject_id": "A", "start_date": "2026-01-01", "end_date": "2027-01-01", "grain": "month"}
	if err := executor.validateMetricSource(ctx, service, input); err != nil {
		t.Fatal(err)
	}
	plan.ParameterLabels["grain"][0].Labels["en"] = "Changed"
	if err := executor.validateMetricSource(ctx, service, input); err == nil {
		t.Fatal("changed enum labels executed without rebind")
	}
	plan.ParameterLabels["grain"][0].Labels["en"] = "Total"
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
			r := &models.CreateQueryServiceRequest{ConfigType: "analytical", MetricSource: &models.MetricSourceRequest{ImplementationID: 3, RevisionID: 7}}
			switch kind {
			case "sql":
				r.SqlQuery = "select 1"
			case "engine":
				id := uint(4)
				r.EngineID = &id
			case "parameters":
				r.NamedParameters = []models.QueryServiceNamedParameter{{Name: "unexpected"}}
			case "output":
				r.OutputContract = &models.QueryServiceOutputContract{}
			case "data":
				r.DataConfig = map[string]interface{}{"where": "true"}
			}
			if _, err := svc.resolveMetricSource(ctx, r, 7); err == nil {
				t.Fatal("compiled origin accepted caller override")
			}
		})
	}
}

func metricServiceFixture(t *testing.T, engineType string) (commonclient.ModelMetricPlan, commonmodels.EngineRuntimeDescriptor) {
	t.Helper()
	var provider interface {
		plugin.EnginePlugin
		plugin.AnalyticalCompilerProvider
	} = &postgresql.PostgreSQLPlugin{}
	if engineType == "mysql" {
		provider = &mysql.MySQLPlugin{}
	} else if engineType == "metric_test_extension" {
		provider = &metricExtensionProvider{PostgreSQLPlugin: &postgresql.PostgreSQLPlugin{}}
	}
	if _, err := plugin.Get(engineType); err != nil {
		plugin.Register(provider)
		t.Cleanup(func() { plugin.Unregister(engineType) })
	}
	fields := []datatype.FieldInfo{{Name: "subject_id", Type: datatype.FieldTypeString}, {Name: "bucket", Type: datatype.FieldTypeDate}, {Name: "value", Type: datatype.FieldTypeBigInt}}
	params := []queryplan.Parameter{{Name: "subject_id", Type: datatype.FieldTypeString, Required: true}, {Name: "start_date", Type: datatype.FieldTypeDate, Required: true}, {Name: "end_date", Type: datatype.FieldTypeDate, Required: true}, {Name: "grain", Type: datatype.FieldTypeString, Required: true, Allowed: []queryplan.Literal{{Type: datatype.FieldTypeString, Text: "total"}}}}
	p := queryplan.Plan{SchemaVersion: queryplan.SchemaVersion, SemanticProfile: queryplan.SemanticProfile, Parameters: params, Root: "result", Output: queryplan.OutputContract{Fields: fields, StableKey: []string{"subject_id", "bucket"}}, Nodes: []queryplan.Node{
		{ID: "unit", Op: "constant_rows", ConstantRows: &queryplan.ConstantRows{Fields: []datatype.FieldInfo{{Name: "unit", Type: datatype.FieldTypeInt}}, Rows: [][]queryplan.Literal{{{Type: datatype.FieldTypeInt, Text: "1"}}}}},
		{ID: "result", Op: "project", Project: &queryplan.Project{Input: "unit", Columns: []queryplan.Projection{{Name: "subject_id", Expr: queryplan.Expr{Op: "parameter", Parameter: "subject_id"}}, {Name: "bucket", Expr: queryplan.Expr{Op: "parameter", Parameter: "start_date"}}, {Name: "value", Expr: queryplan.Expr{Op: "literal", Literal: &queryplan.Literal{Type: datatype.FieldTypeBigInt, Text: "1"}}}}}},
	}}
	p.Nodes = append(p.Nodes, queryplan.Node{ID: "valid", Op: "filter", Filter: &queryplan.Filter{Input: "result", Predicate: queryplan.Expr{Op: "and", Args: []queryplan.Expr{
		{Op: "lt", Args: []queryplan.Expr{{Op: "parameter", Parameter: "start_date"}, {Op: "parameter", Parameter: "end_date"}}},
		{Op: "eq", Args: []queryplan.Expr{{Op: "parameter", Parameter: "grain"}, {Op: "literal", Literal: &queryplan.Literal{Type: datatype.FieldTypeString, Text: "total"}}}},
	}}}})
	p.Root = "valid"
	capability := plugin.AnalyticalCapability{Supported: true, PlanVersions: []string{queryplan.SchemaVersion}, SemanticProfiles: []string{queryplan.SemanticProfile}}
	pkg, err := plugin.NewAnalyticalPlanPackage(plugin.CompileRequest{Plan: p, Instance: plugin.AnalyticalInstance{EngineID: 2, Capability: capability}}, provider.AnalyticalCompiler())
	if err != nil {
		t.Fatal(err)
	}
	caps := provider.Capabilities()
	caps.Compute.Query.Analytical = &capability
	engine := commonmodels.EngineRuntimeDescriptor{ID: 2, EngineType: engineType, LifecycleState: commonmodels.EngineLifecycleActive, ConnectionStatus: commonmodels.EngineConnectionOnline, Capabilities: querySampleCapabilities(t, caps)}
	return commonclient.ModelMetricPlan{ImplementationID: 3, RevisionID: 7, MetricDefinitionID: 9, MetricDefinitionRevisionID: 19, DependencyHash: strings.Repeat("a", 64), ExecutionPlan: pkg, ParameterLabels: map[string][]commonquery.ParameterOption{"grain": {{Value: "total", Labels: map[string]string{"en": "Total", "zh-cn": "全期"}}}}}, engine
}

// A test registration demonstrates that owner dispatch has no engine allowlist;
// it does not certify or enable a new production database.
type metricExtensionProvider struct{ *postgresql.PostgreSQLPlugin }

func (*metricExtensionProvider) Type() string { return "metric_test_extension" }

func TestMetricParameterPresentationContract(t *testing.T) {
	plan, _ := metricServiceFixture(t, "postgresql")
	plan.ParameterPresentation = map[string]commonquery.ParameterPresentation{}
	for _, parameter := range plan.ExecutionPlan.Plan.Parameters {
		plan.ParameterPresentation[parameter.Name] = commonquery.ParameterPresentation{
			Labels:       map[string]string{"zh-cn": "名称", "en": "Name"},
			Descriptions: map[string]string{"zh-cn": "说明", "en": "Help"},
		}
	}
	if err := plan.Validate(); err != nil {
		t.Fatal(err)
	}
	source := plan.ParameterPresentation["subject_id"]
	delete(plan.ParameterPresentation, "subject_id")
	if plan.Validate() == nil {
		t.Fatal("incomplete presentation accepted")
	}
	plan.ParameterPresentation["unknown"] = source
	if plan.Validate() == nil {
		t.Fatal("undeclared parameter accepted")
	}
	delete(plan.ParameterPresentation, "unknown")
	plan.ParameterPresentation["subject_id"] = source
	delete(source.Labels, "en")
	if plan.Validate() == nil {
		t.Fatal("missing translated label accepted")
	}
}
