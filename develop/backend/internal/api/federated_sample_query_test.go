package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/protection"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

type samplePreparedGate struct {
	allowQueryProtectionGate
	deny bool
}

func (g samplePreparedGate) BeginFederatedQuery(ctx context.Context, _ uint, prepare func(context.Context) (plugin.PreparedFederatedQuery, error)) (plugin.PreparedFederatedQuery, func(), error) {
	if g.deny {
		return nil, nil, protection.ErrRequired
	}
	p, err := prepare(ctx)
	if err != nil {
		return nil, nil, err
	}
	return p, func() { _ = p.Close(context.Background()) }, nil
}

func TestSelectedFederatedSamplePreservesSourceAndUsesAuthorizationProtectionAndOneCandidate(t *testing.T) {
	for _, test := range []struct {
		name      string
		rows      bool
		denied    bool
		protected bool
		want      int
	}{
		{"nonempty", true, false, false, 200}, {"empty", false, false, false, 422},
		{"authorization_denied", true, true, false, 403}, {"protected", true, false, true, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			spark, _ := plugin.Get("spark")
			encodeCaps := func(c plugin.EngineCapabilities) *commonModels.JSONString {
				raw, _ := json.Marshal(c)
				result := commonModels.JSONString(raw)
				return &result
			}
			target := commonModels.EngineRuntimeDescriptor{ID: 90, Name: "Spark Cluster", EngineType: "spark", LifecycleState: "active", ConnectionStatus: "online", Capabilities: encodeCaps(spark.Capabilities())}
			actorCaps := plugin.NewFederatedQueryCapabilities("spark_workflow", "addp.query-runtime/v1", []string{"postgresql", "mysql"}, nil)
			actorCaps.Compute.Query.Federation.ExecutionAudience = "spark"
			actorCaps.Compute.Query.Federation.TargetEngineTypes = []string{"spark"}
			actor := commonModels.EngineRuntimeDescriptor{ID: 99, EngineType: "spark_workflow", IsBuiltin: true, LifecycleState: "active", ConnectionStatus: "online", Capabilities: encodeCaps(actorCaps)}
			rawPG, _ := dbbridge.GenerateCapabilities("postgresql")
			pgCaps := commonModels.JSONString(rawPG)
			source := commonModels.EngineRuntimeDescriptor{ID: 12, Name: "Business PostgreSQL", EngineType: "postgresql", LifecycleState: "active", ConnectionStatus: "online", Capabilities: &pgCaps}
			issued, prepares, executes, closes := 0, 0, 0, 0
			query := "SELECT *\nFROM `Business_PostgreSQL`.` schema.with dot `.` 原名``表 `\nLIMIT 10"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/system/runtime/engine-descriptors/90":
					_ = json.NewEncoder(w).Encode(target)
				case "/api/v1/system/runtime/engine-descriptors/99":
					_ = json.NewEncoder(w).Encode(actor)
				case "/api/v1/system/runtime/engine-descriptors/12":
					_ = json.NewEncoder(w).Encode(source)
				case "/api/v1/system/runtime/engine-descriptors":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []commonModels.EngineRuntimeDescriptor{actor, source}, "total": 2, "page": 1, "page_size": 100})
				case "/api/v1/meta/items/7/ancestors":
					_ = json.NewEncoder(w).Encode(commonModels.MetaItemAncestors{Item: commonModels.MetaItem{ID: 7, TenantID: 7, EngineID: 12, NodeID: 3, ItemType: "table", Name: " 原名`表 "}, Ancestors: []commonModels.MetaNode{{ID: 3, TenantID: 7, EngineID: 12, NodeType: "schema", Name: " schema.with dot ", FullName: "namespace"}}})
				case "/api/v1/system/auth/execution-authorizations":
					issued++
					var req commonClient.IssueExecutionAuthorizationRequest
					_ = json.NewDecoder(r.Body).Decode(&req)
					if req.Audience != "spark" || !reflect.DeepEqual(req.Accesses, []commonClient.ExecutionEngineAccessScope{{EngineID: "12", Effects: []string{"read"}}, {EngineID: "90", Effects: []string{"read"}}}) {
						t.Errorf("unexpected grant scope: %+v", req)
					}
					if test.denied {
						w.WriteHeader(403)
						_, _ = w.Write([]byte(`{"error_code":"execution_authorization_denied"}`))
						return
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{ID: "77", TenantID: "7", ActorPrincipalID: "1", TenantMembershipID: "9", IssuedAuthorizationVersion: "3", Audience: "spark", ExecutionID: req.ExecutionID, Accesses: req.Accesses, ExpiresAt: time.Now().Add(time.Minute)})
				case "/api/v1/queries":
					prepares++
					var req plugin.FederatedQueryRequest
					_ = json.NewDecoder(r.Body).Decode(&req)
					if req.Query != query || req.TargetEngineID != 90 || !reflect.DeepEqual(req.SourceEngineIDs, []uint{12}) {
						t.Errorf("wrong query or source: %+v", req)
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"query_id": "9a21ab1a-2900-42a5-ae91-821339b3fcdd", "sources": []map[string]interface{}{{"engine_id": 12, "engine_type": "postgresql"}}, "engine_read_scope": map[string]interface{}{"engine_ids": []uint{12}}, "read_set": &plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", " schema.with dot ", " 原名`表 ")}}})
				case "/api/v1/queries/9a21ab1a-2900-42a5-ae91-821339b3fcdd/execute":
					executes++
					rows := []map[string]interface{}{}
					if test.rows {
						rows = append(rows, map[string]interface{}{"id": 1})
					}
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"columns": []string{"id"}, "fields": []map[string]interface{}{{"name": "id", "type": "int"}}, "rows": rows})
				case "/api/v1/queries/9a21ab1a-2900-42a5-ae91-821339b3fcdd":
					closes++
					w.WriteHeader(204)
				default:
					t.Errorf("unexpected enumeration or fallback: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(endpoint.Port())
			actor.RuntimeEndpoint = &commonModels.EngineRuntimeEndpoint{Protocol: "http", Host: endpoint.Hostname(), Port: port}
			system := commonClient.NewSystemServiceClient(server.URL, staticDevelopServiceTokens("fixture"), server.Client())
			federated := service.NewFederatedQueryService(system, commonClient.NewMetaClient(server.URL, staticDevelopServiceTokens("fixture")))
			federated.SetProtectionGate(samplePreparedGate{deny: test.protected})
			sql := service.NewSQLEngineService(&config.Config{}, system, commonClient.NewSystemExecutionAuthorizationClient(server.URL, nil))
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/engines/:id/sample-query", func(c *gin.Context) {
				setTenantAuthContextForTest(c, 7, 1)
				NewQueryHandler(sql, federated).GetSampleQuery(c)
			})
			request := httptest.NewRequest("GET", "/engines/90/sample-query?locator="+url.QueryEscape("addp://engine/12/path/forged/table?type=table&item_id=7"), nil)
			request.Header.Set("Authorization", "Bearer addp_at_user")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want || issued != 1 {
				t.Fatalf("status=%d body=%s issued=%d", response.Code, response.Body.String(), issued)
			}
			if test.denied || test.protected {
				if test.protected && !strings.Contains(response.Body.String(), `"error_code":"data_protection_required"`) {
					t.Fatalf("protection refusal was misclassified: %s", response.Body.String())
				}
				if prepares+executes+closes != 0 {
					t.Fatal("refused query reached Runtime")
				}
				return
			}
			if prepares != 1 || executes != 1 || closes != 1 {
				t.Fatalf("prepare=%d execute=%d close=%d", prepares, executes, closes)
			}
			var body map[string]interface{}
			_ = json.Unmarshal(response.Body.Bytes(), &body)
			if test.rows && body["query"] != query {
				t.Fatalf("template=%+v", body)
			}
			if !test.rows && body["error_code"] != "sample_query_resource_empty" {
				t.Fatalf("empty=%+v", body)
			}
		})
	}
}
