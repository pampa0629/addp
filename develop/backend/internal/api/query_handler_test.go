package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/addp/common/models"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/config"
	developmodels "github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

type emptyQueryTemplatePlugin struct{}

func (p *emptyQueryTemplatePlugin) Type() string         { return "develop_empty_query_template_test" }
func (p *emptyQueryTemplatePlugin) DisplayName() string  { return "Empty Query Template Test" }
func (p *emptyQueryTemplatePlugin) EngineOrigin() string { return "general" }
func (p *emptyQueryTemplatePlugin) TestConnection(context.Context, plugin.ConnectionInfo) error {
	return nil
}
func (p *emptyQueryTemplatePlugin) ValidateConnectionInfo(plugin.ConnectionInfo) error { return nil }
func (p *emptyQueryTemplatePlugin) DefaultPort() int                                   { return 0 }
func (p *emptyQueryTemplatePlugin) RequiredFields() []string                           { return nil }
func (p *emptyQueryTemplatePlugin) SensitiveFields() []string                          { return nil }
func (p *emptyQueryTemplatePlugin) Capabilities() plugin.EngineCapabilities {
	return plugin.EngineCapabilities{}
}
func (p *emptyQueryTemplatePlugin) EngineCatalogModel() plugin.EngineCatalogModelSpec {
	return plugin.DynamicSchemaCatalogModel()
}
func (p *emptyQueryTemplatePlugin) QueryLanguages() []string { return []string{"mql"} }
func (p *emptyQueryTemplatePlugin) GenerateSampleQuery(_ context.Context, _ plugin.ConnectionInfo, opts plugin.SampleQueryOptions) (string, string) {
	return `{"find":"` + opts.Path.Segments[len(opts.Path.Segments)-1].Name + `","filter":{},"limit":10}`, "mql"
}
func (p *emptyQueryTemplatePlugin) ExecuteRuntimeQuery(context.Context, plugin.ConnectionInfo, plugin.QueryRequest) (*plugin.QueryResult, error) {
	return &plugin.QueryResult{Rows: []map[string]interface{}{}}, nil
}

func TestConnectionUsesUserDerivedReadAuthorizationAndServiceTokenConsumption(t *testing.T) {
	runtimeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/health" {
			t.Fatalf("unexpected runtime request: %s %s", request.Method, request.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer runtimeServer.Close()
	runtimeURL, err := url.Parse(runtimeServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(runtimeURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	capabilitiesJSON, err := plugin.MarshalEngineCapabilities(
		plugin.NewWorkflowCapabilities("math_workflow", plugin.WorkflowRuntimeAPIAddpV1),
	)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := models.JSONString(capabilitiesJSON)

	var issuedExecutionID string
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Internal-API-Key") != "" || request.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("connection test must not send legacy internal headers")
		}
		switch request.URL.Path {
		case "/api/v1/system/auth/execution-authorizations":
			if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer addp_at_user" {
				t.Fatalf("unexpected authorization issue request")
			}
			var payload commonClient.IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Audience != "develop" || len(payload.Accesses) != 1 || payload.Accesses[0].EngineID != "12" ||
				len(payload.Accesses[0].Effects) != 1 || payload.Accesses[0].Effects[0] != "read" || payload.ExpiresIn <= 0 {
				t.Fatalf("issue payload = %#v", payload)
			}
			issuedExecutionID = payload.ExecutionID
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "77", ExecutionID: issuedExecutionID, Audience: "develop",
				Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: "12", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(time.Minute),
				ActorPrincipalID: "1", TenantID: "7", TenantMembershipID: "9", IssuedAuthorizationVersion: "3",
			})
		case "/api/v1/system/execution-authorizations/77/engine-accesses":
			if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer addp_at_service" {
				t.Fatalf("unexpected engine access request")
			}
			var payload commonClient.ExecutionEngineAccessRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ExecutionID != issuedExecutionID || payload.EngineID != "12" ||
				len(payload.RequiredEffects) != 1 || payload.RequiredEffects[0] != "read" {
				t.Fatalf("engine access payload = %#v", payload)
			}
			_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{
				AuthorizationID: "77", ExecutionID: issuedExecutionID, Audience: "develop", EngineID: "12",
				Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
				Engine: &models.Engine{
					ID: 12, EngineType: "math_workflow",
					ConnectionInfo: models.ConnectionInfo{"protocol": "http", "host": host, "port": port},
					Capabilities:   &capabilities,
				},
			})
		default:
			t.Fatalf("unexpected System path: %s", request.URL.Path)
		}
	}))
	defer systemServer.Close()

	response := testConnectionRequestForTest(newAuthorizedQueryHandlerForTest(systemServer.URL), "addp_at_user")
	if response.Code != http.StatusOK || issuedExecutionID == "" {
		t.Fatalf("status = %d, execution = %q, body = %s", response.Code, issuedExecutionID, response.Body.String())
	}
}

func TestPreflightQueryReturnsPermissionAndConfirmationFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	queryHandler := NewQueryHandler(
		service.NewSQLEngineService(&config.Config{EncryptionKey: []byte("preflight-test-key")}, nil, nil),
		nil,
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/develop/query-preflight", bytes.NewBufferString(`{"query_type":"sql","query":"DROP TABLE activities","engine_id":12,"target_locator":"addp://engine/12/path/activities?type=table"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	setTenantAuthContextWithPermissionsForTest(context, 7, 1, []string{
		developauthorization.PermissionDevelopDataDdlExecute,
	})

	queryHandler.PreflightQuery(context)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body developmodels.QueryPreflightResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Allowed || body.Effect != "ddl" || !body.RequiresConfirmation || body.ConfirmationToken == "" || len(body.TargetObjects) != 1 {
		t.Fatalf("preflight = %#v", body)
	}
}

func TestConnectionRejectsMissingUserAccessTokenBeforeSystemCall(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()

	response := testConnectionRequestForTest(newAuthorizedQueryHandlerForTest(server.URL), "")
	if response.Code != http.StatusUnauthorized || calls != 0 ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"authentication_required"`)) {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, calls, response.Body.String())
	}
}

func TestConnectionMapsReadEffectPermissionDenialWithoutConsumingEngineAccess(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/api/v1/system/auth/execution-authorizations" {
			t.Fatalf("unexpected System path after denied issue: %s", request.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	response := testConnectionRequestForTest(newAuthorizedQueryHandlerForTest(server.URL), "addp_at_user")
	if response.Code != http.StatusForbidden || calls != 1 ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"execution_effect_permission_denied"`)) {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, calls, response.Body.String())
	}
}

func TestGetSampleQueryUsesAuthorizedEngineAndRejectsUnavailableCatalog(t *testing.T) {
	var issuedExecutionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/system/auth/execution-authorizations":
			var payload commonClient.IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if request.Header.Get("Authorization") != "Bearer addp_at_user" ||
				len(payload.Accesses) != 1 || payload.Accesses[0].EngineID != "12" ||
				len(payload.Accesses[0].Effects) != 1 || payload.Accesses[0].Effects[0] != "read" {
				t.Fatalf("sample issue request = %#v, headers=%#v", payload, request.Header)
			}
			issuedExecutionID = payload.ExecutionID
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "77", ExecutionID: payload.ExecutionID, Audience: "develop",
				Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: "12", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(time.Minute),
				ActorPrincipalID: "1", TenantID: "7", TenantMembershipID: "9", IssuedAuthorizationVersion: "3",
			})
		case "/api/v1/system/execution-authorizations/77/engine-accesses":
			if request.Header.Get("Authorization") != "Bearer addp_at_service" {
				t.Fatalf("sample consume Authorization = %q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{
				AuthorizationID: "77", ExecutionID: issuedExecutionID, Audience: "develop", EngineID: "12",
				Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
				Engine: &models.Engine{
					ID: 12, EngineType: "postgresql",
					ConnectionInfo: models.ConnectionInfo{"host": "127.0.0.1", "port": 1, "database": "business"},
				},
			})
		default:
			t.Fatalf("unexpected System path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/engines/:id/sample-query", func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		newAuthorizedQueryHandlerForTest(server.URL).GetSampleQuery(c)
	})
	request := httptest.NewRequest(http.MethodGet, "/engines/12/sample-query", nil)
	request.Header.Set("Authorization", "Bearer addp_at_user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || issuedExecutionID == "" {
		t.Fatalf("status = %d, execution = %q, body = %s", response.Code, issuedExecutionID, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error_code"] != "sample_query_unavailable" || body["query"] != "" {
		t.Fatalf("sample response = %#v", body)
	}
}

func TestGetSampleQueryReportsSelectedResourceEmptyInRequestedLanguage(t *testing.T) {
	provider := &emptyQueryTemplatePlugin{}
	plugin.Register(provider)
	t.Cleanup(func() { plugin.Unregister(provider.Type()) })

	var issuedExecutionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/v1/system/auth/execution-authorizations":
			var payload commonClient.IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			issuedExecutionID = payload.ExecutionID
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "77", ExecutionID: issuedExecutionID, Audience: "develop",
				Accesses: []commonClient.ExecutionEngineAccessScope{{EngineID: "12", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(time.Minute),
				ActorPrincipalID: "1", TenantID: "7", TenantMembershipID: "9", IssuedAuthorizationVersion: "3",
			})
		case "/api/v1/system/execution-authorizations/77/engine-accesses":
			_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{
				AuthorizationID: "77", ExecutionID: issuedExecutionID,
				Audience: "develop", EngineID: "12", Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
				Engine: &models.Engine{ID: 12, EngineType: provider.Type(), ConnectionInfo: models.ConnectionInfo{}},
			})
		default:
			t.Fatalf("unexpected System path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	tests := []struct {
		language string
		message  string
	}{
		{language: "zh-cn", message: "所选数据项没有可用于生成查询模板的真实数据"},
		{language: "en", message: "The selected data item contains no real data for a query template."},
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.GET("/engines/:id/sample-query", func(c *gin.Context) {
				setTenantAuthContextForTest(c, 7, 1)
				newAuthorizedQueryHandlerForTest(server.URL).GetSampleQuery(c)
			})
			locator := "addp://engine/12/path/business/empty_orders?type=collection&item_id=9"
			request := httptest.NewRequest(http.MethodGet, "/engines/12/sample-query?locator="+url.QueryEscape(locator), nil)
			request.Header.Set("Authorization", "Bearer addp_at_user")
			request.Header.Set("Accept-Language", tt.language)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusUnprocessableEntity || body["error_code"] != "sample_query_resource_empty" || body["error"] != tt.message {
				t.Fatalf("status = %d, body = %#v", response.Code, body)
			}
		})
	}
}

func TestGetSampleQueryRejectsInvalidResourceLocatorBeforeAuthorization(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()

	response := sampleQueryRequestForTest(
		newAuthorizedQueryHandlerForTest(server.URL),
		"/engines/12/sample-query?locator="+url.QueryEscape("not-a-resource-locator"),
	)
	if response.Code != http.StatusBadRequest || calls != 0 ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"query_template_resource_invalid"`)) {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, calls, response.Body.String())
	}
}

func TestGetSampleQueryRejectsResourceLocatorFromAnotherEngineBeforeAuthorization(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()

	locator := "addp://engine/13/path/business/orders?type=collection&item_id=9"
	response := sampleQueryRequestForTest(
		newAuthorizedQueryHandlerForTest(server.URL),
		"/engines/12/sample-query?locator="+url.QueryEscape(locator),
	)
	if response.Code != http.StatusBadRequest || calls != 0 ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"query_template_resource_invalid"`)) {
		t.Fatalf("status = %d, calls = %d, body = %s", response.Code, calls, response.Body.String())
	}
}

func newAuthorizedQueryHandlerForTest(systemURL string) *QueryHandler {
	systemService := commonClient.NewSystemServiceClient(
		systemURL, staticDevelopServiceTokens("addp_at_service"), nil,
	)
	return NewQueryHandler(service.NewSQLEngineService(
		&config.Config{DefaultQueryTimeout: 30, MaxQueryTimeout: 300},
		systemService,
		commonClient.NewSystemExecutionAuthorizationClient(systemURL, nil),
	), nil)
}

func testConnectionRequestForTest(handler *QueryHandler, token string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/test/:id", func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		handler.TestConnection(c)
	})
	request := httptest.NewRequest(http.MethodGet, "/test/12", nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func sampleQueryRequestForTest(handler *QueryHandler, target string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/engines/:id/sample-query", func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		handler.GetSampleQuery(c)
	})
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Authorization", "Bearer addp_at_user")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
