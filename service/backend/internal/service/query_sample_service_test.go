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

type querySampleSQLPlugin struct {
	executedQuery string
}

type querySampleFederatedPlugin struct {
	executedRequest *plugin.FederatedQueryRequest
}

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

func (p *querySampleSQLPlugin) ExecuteRuntimeQuery(_ context.Context, _ plugin.ConnectionInfo, req plugin.QueryRequest) (*plugin.QueryResult, error) {
	p.executedQuery = req.Query
	return &plugin.QueryResult{Columns: []string{"id"}, Rows: []map[string]interface{}{{"id": 1, "query": req.Query}}}, nil
}
func (*querySampleSQLPlugin) SQLDialect() string { return "postgresql" }
func (*querySampleSQLPlugin) ExecuteSQL(ctx context.Context, conn plugin.ConnectionInfo, sql string, opts plugin.QueryOptions) (*plugin.QueryResult, error) {
	return (&querySampleSQLPlugin{}).ExecuteRuntimeQuery(ctx, conn, plugin.QueryRequest{Query: sql, Options: opts})
}

func (*querySampleFederatedPlugin) Type() string         { return "service_sample_federated" }
func (*querySampleFederatedPlugin) DisplayName() string  { return "Service Sample Federated" }
func (*querySampleFederatedPlugin) EngineOrigin() string { return "builtin" }
func (*querySampleFederatedPlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (*querySampleFederatedPlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (*querySampleFederatedPlugin) DefaultPort() int                                   { return 0 }
func (*querySampleFederatedPlugin) RequiredFields() []string                           { return nil }
func (*querySampleFederatedPlugin) SensitiveFields() []string                          { return nil }
func (*querySampleFederatedPlugin) ConnectionIdentityFields() []string                 { return []string{"host"} }
func (*querySampleFederatedPlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.NewFederatedQueryCapabilities("service_sample_federated", "http", []string{"postgresql"}, nil)
}
func (*querySampleFederatedPlugin) QueryLanguages() []string { return []string{"sql"} }
func (*querySampleFederatedPlugin) ResolveSourceEngineIDs(string, []plugin.FederatedQuerySource) []uint {
	return nil
}
func (*querySampleFederatedPlugin) ResolveObjectTableReferences(string, []plugin.FederatedQuerySource) []plugin.FederatedQueryObjectTableReference {
	return nil
}
func (p *querySampleFederatedPlugin) ExecuteFederatedQuery(_ context.Context, _ plugin.ConnectionInfo, req plugin.FederatedQueryRequest) (*plugin.QueryResult, error) {
	p.executedRequest = &req
	return &plugin.QueryResult{Columns: []string{"id"}, Rows: []map[string]interface{}{{"id": 1}}}, nil
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
	if language != "sql" || query != "SELECT *\nFROM \"public\".\"orders\"" {
		t.Fatalf("sample = (%q, %q)", query, language)
	}
	const wantExecuted = "SELECT * FROM (SELECT *\nFROM \"public\".\"orders\") AS addp_page LIMIT 10"
	if enginePlugin.executedQuery != wantExecuted {
		t.Fatalf("executed query = %q, want %q", enginePlugin.executedQuery, wantExecuted)
	}
}

func TestQuerySampleServiceReturnsUnboundedFederatedSampleAndBoundsValidation(t *testing.T) {
	enginePlugin := &querySampleFederatedPlugin{}
	plugin.Register(enginePlugin)
	t.Cleanup(func() { plugin.Unregister(enginePlugin.Type()) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/runtime/engine-descriptors/90":
			_ = json.NewEncoder(w).Encode(models.EngineRuntimeDescriptor{
				ID: 90, Name: "DuckDB", EngineType: enginePlugin.Type(), LifecycleState: models.EngineLifecycleActive,
				RuntimeEndpoint: &models.EngineRuntimeEndpoint{Protocol: "http", Host: "127.0.0.1", Port: 8104},
			})
		case "/api/v1/system/runtime/engine-descriptors":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []models.EngineRuntimeDescriptor{{
					ID: 21, Name: "Business PostgreSQL", EngineType: "postgresql", LifecycleState: models.EngineLifecycleActive,
				}},
				"total": 1, "page": 1, "page_size": 100,
			})
		case "/api/v1/meta/engines/21/tree":
			_ = json.NewEncoder(w).Encode(models.MetadataTree{Items: []models.MetaItem{{
				ID: 31, EngineID: 21, ItemType: "table", Name: "orders", FullName: "public.orders",
			}}})
		case "/api/v1/system/auth/execution-authorizations":
			var request client.IssueExecutionAuthorizationRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			_ = json.NewEncoder(w).Encode(client.IssuedExecutionAuthorization{
				ID: "56", ExecutionID: request.ExecutionID, Audience: "duckdb", EngineIDs: request.EngineIDs,
				Effects: request.Effects, ExpiresAt: time.Now().Add(time.Minute), ActorPrincipalID: "1",
				TenantID: "7", TenantMembershipID: "2", IssuedAuthorizationVersion: "3", SourceType: "user",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	system := client.NewSystemServiceClient(server.URL, querySampleTokenSource{}, server.Client())
	meta := client.NewMetaClient(server.URL, querySampleTokenSource{})
	service := NewQuerySampleService(system, client.NewSystemExecutionAuthorizationClient(server.URL, server.Client()), meta)
	query, language, err := service.Generate(context.Background(), 7, "addp_at_user", 90)
	if err != nil {
		t.Fatal(err)
	}
	const want = "SELECT *\nFROM Business_PostgreSQL.public.orders"
	if language != "sql" || query != want {
		t.Fatalf("sample = (%q, %q), want (%q, sql)", query, language, want)
	}
	if enginePlugin.executedRequest == nil {
		t.Fatal("federated sample was not executed")
	}
	if enginePlugin.executedRequest.Query != want || enginePlugin.executedRequest.Options.Limit != 10 || !enginePlugin.executedRequest.Options.Spatial {
		t.Fatalf("executed request = %#v", enginePlugin.executedRequest)
	}
}
