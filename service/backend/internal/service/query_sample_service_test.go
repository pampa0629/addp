package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

type querySampleTokenSource struct{}

func (querySampleTokenSource) Token(context.Context, uint) (string, error) {
	return "addp_sat_service", nil
}

func (querySampleTokenSource) PlatformToken(context.Context) (string, error) {
	return "addp_sat_service_platform", nil
}

type querySampleSQLPlugin struct{}

func (*querySampleSQLPlugin) Type() string                                                { return "service_sample_sql" }
func (*querySampleSQLPlugin) DisplayName() string                                         { return "Service Sample SQL" }
func (*querySampleSQLPlugin) EngineOrigin() string                                        { return "general" }
func (*querySampleSQLPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error { return nil }
func (*querySampleSQLPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error          { return nil }
func (*querySampleSQLPlugin) DefaultPort() int                                            { return 0 }
func (*querySampleSQLPlugin) RequiredFields() []string                                    { return nil }
func (*querySampleSQLPlugin) SensitiveFields() []string                                   { return nil }
func (*querySampleSQLPlugin) ConnectionIdentityFields() []string                          { return []string{"host"} }
func (*querySampleSQLPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewTabularCapabilities("service_sample_sql", "tabular", plugin.TabularCapabilityOptions{DefaultLanguage: "sql"})
}
func (*querySampleSQLPlugin) CatalogModel() plugin.CatalogModelSpec {
	return plugin.TabularCatalogModel(plugin.CatalogTermSchema)
}
func (*querySampleSQLPlugin) ListChildren(_ context.Context, _ plugin.ConnectionInfo, parent plugin.CatalogPath, _ plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	if plugin.IsCatalogRootPath(parent) {
		return []plugin.CatalogEntry{{Name: "public", Role: plugin.CatalogRoleBranch, Path: plugin.TabularNamespacePath(parent.EngineID, plugin.CatalogTermSchema, "public")}}, nil
	}
	rowCount := int64(3)
	return []plugin.CatalogEntry{{Name: "orders", Role: plugin.CatalogRoleLeaf, Path: plugin.TabularItemPath(parent.EngineID, plugin.CatalogTermSchema, "public", "orders"), Table: &datatype.TableInfo{RowCount: &rowCount}}}, nil
}
func (*querySampleSQLPlugin) ResolvePath(context.Context, plugin.ConnectionInfo, plugin.CatalogPath) (*plugin.CatalogEntry, error) {
	return nil, nil
}
func (*querySampleSQLPlugin) QueryLanguages() []string { return []string{"sql"} }
func (*querySampleSQLPlugin) GenerateSampleQuery(context.Context, plugin.ConnectionInfo, plugin.SampleQueryOptions) (string, string) {
	return "", "sql"
}
func (*querySampleSQLPlugin) ExecuteRuntimeQuery(_ context.Context, _ plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	return &plugin.QueryResult{Columns: []string{"id"}, Rows: []map[string]interface{}{{"id": 1, "query": req.Query}}}, nil
}
func (*querySampleSQLPlugin) SQLDialect() string { return "postgresql" }
func (*querySampleSQLPlugin) ExecuteSQL(ctx context.Context, conn plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return (&querySampleSQLPlugin{}).ExecuteRuntimeQuery(ctx, conn, plugin.QueryRequest{Query: sql, Options: opts})
}

func TestQuerySampleServiceExecutesDirectSampleThroughAuthorization(t *testing.T) {
	enginePlugin := &querySampleSQLPlugin{}
	plugin.Register(enginePlugin)
	t.Cleanup(func() { plugin.Unregister(enginePlugin.Type()) })

	var issued, consumed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-descriptors/21":
			_ = json.NewEncoder(w).Encode(models.EngineRuntimeDescriptor{
				ID: 21, Name: "Business SQL", EngineType: enginePlugin.Type(), LifecycleState: models.EngineLifecycleActive,
			})
		case "/api/v1/system/auth/execution-authorizations":
			issued = true
			var request client.IssueExecutionAuthorizationRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			_ = json.NewEncoder(w).Encode(client.IssuedExecutionAuthorization{
				ID: "55", ExecutionID: request.ExecutionID, Audience: "service", EngineIDs: request.EngineIDs,
				Effects: request.Effects, ExpiresAt: time.Now().Add(time.Minute), ActorPrincipalID: "1",
				TenantID: "7", TenantMembershipID: "2", IssuedAuthorizationVersion: "3", SourceType: "user",
			})
		case "/api/v1/system/execution-authorizations/55/engine-accesses":
			consumed = true
			var request client.ExecutionEngineAccessRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			_ = json.NewEncoder(w).Encode(client.ExecutionEngineAccess{
				AuthorizationID: "55", ExecutionID: request.ExecutionID, Audience: "service", EngineID: "21",
				Effects: request.RequiredEffects, ExpiresAt: time.Now().Add(time.Minute),
				Engine: &models.Engine{ID: 21, EngineType: enginePlugin.Type(), ConnectionInfo: models.ConnectionInfo{"host": "business"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	system := client.NewSystemServiceClient(server.URL, querySampleTokenSource{}, server.Client())
	service := NewQuerySampleService(system, client.NewSystemExecutionAuthorizationClient(server.URL, server.Client()), nil)
	query, language, err := service.Generate(context.Background(), 7, "addp_at_user", 21)
	if err != nil {
		t.Fatal(err)
	}
	if !issued || !consumed {
		t.Fatalf("authorization flow issued=%v consumed=%v", issued, consumed)
	}
	if language != "sql" || query != "SELECT *\nFROM \"public\".\"orders\"\nLIMIT 10" {
		t.Fatalf("sample = (%q, %q)", query, language)
	}
}
