package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

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
			if payload.Audience != "develop" || len(payload.EngineIDs) != 1 || payload.EngineIDs[0] != "12" ||
				len(payload.Effects) != 1 || payload.Effects[0] != "read" || payload.ExpiresIn <= 0 {
				t.Fatalf("issue payload = %#v", payload)
			}
			issuedExecutionID = payload.ExecutionID
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "77", ExecutionID: issuedExecutionID, Audience: "develop",
				EngineIDs: []string{"12"}, Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
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

func TestExecuteQueryUsesUserDerivedExecutionAuthorizationAndServiceTokenConsumption(t *testing.T) {
	var issuedExecutionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Internal-API-Key") != "" || request.Header.Get("X-Tenant-ID") != "" {
			t.Fatal("execution authorization path must not send legacy internal headers")
		}
		switch request.URL.Path {
		case "/api/v1/system/auth/execution-authorizations":
			if got := request.Header.Get("Authorization"); got != "Bearer addp_at_user" {
				t.Fatalf("issue Authorization = %q", got)
			}
			var payload commonClient.IssueExecutionAuthorizationRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode issue request: %v", err)
			}
			if payload.Audience != "develop" || len(payload.EngineIDs) != 1 || payload.EngineIDs[0] != "12" ||
				len(payload.Effects) != 1 || payload.Effects[0] != "read" {
				t.Fatalf("issue payload = %#v", payload)
			}
			issuedExecutionID = payload.ExecutionID
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "77", ExecutionID: payload.ExecutionID, Audience: "develop",
				EngineIDs: []string{"12"}, Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
				ActorPrincipalID: "1", TenantID: "7", TenantMembershipID: "9", IssuedAuthorizationVersion: "3",
			})
		case "/api/v1/system/execution-authorizations/77/engine-accesses":
			if got := request.Header.Get("Authorization"); got != "Bearer addp_at_service" {
				t.Fatalf("consume Authorization = %q", got)
			}
			var payload commonClient.ExecutionEngineAccessRequest
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Fatalf("decode consume request: %v", err)
			}
			if payload.ExecutionID != issuedExecutionID || payload.EngineID != "12" ||
				len(payload.RequiredEffects) != 1 || payload.RequiredEffects[0] != "read" {
				t.Fatalf("consume payload = %#v, issued execution = %q", payload, issuedExecutionID)
			}
			_ = json.NewEncoder(w).Encode(commonClient.ExecutionEngineAccess{
				AuthorizationID: "77", ExecutionID: issuedExecutionID, Audience: "develop", EngineID: "12",
				Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
				Engine: &models.Engine{ID: 12, EngineType: "mongodb"},
			})
		default:
			t.Fatalf("unexpected System path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	handler := newAuthorizedQueryHandlerForTest(server.URL)
	response := executeQueryRequestForTest(t, handler, `SELECT * FROM cities`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error_code"] != "controlled_sql_engine_unsupported" || issuedExecutionID == "" {
		t.Fatalf("body = %#v, issued execution = %q", body, issuedExecutionID)
	}
}

func TestExecuteQueryMapsEffectPermissionDenialWithoutConsumingEngineAccess(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/api/v1/system/auth/execution-authorizations" {
			t.Fatalf("unexpected System path after denied issue: %s", request.URL.Path)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	handler := newAuthorizedQueryHandlerForTest(server.URL)
	response := executeQueryRequestForTest(t, handler, `DELETE FROM cities WHERE id = 1`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if calls != 1 || !bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"execution_effect_permission_denied"`)) {
		t.Fatalf("calls = %d, body = %s", calls, response.Body.String())
	}
}

func TestExecuteQueryRejectsMultipleStatementsBeforeIssuingAuthorization(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()

	handler := newAuthorizedQueryHandlerForTest(server.URL)
	response := executeQueryRequestForTest(t, handler, `SELECT 1; DELETE FROM cities`)
	if response.Code != http.StatusBadRequest || calls != 0 ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"error_code":"sql_effect_unclassifiable"`)) {
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
				len(payload.EngineIDs) != 1 || payload.EngineIDs[0] != "12" ||
				len(payload.Effects) != 1 || payload.Effects[0] != "read" {
				t.Fatalf("sample issue request = %#v, headers=%#v", payload, request.Header)
			}
			issuedExecutionID = payload.ExecutionID
			_ = json.NewEncoder(w).Encode(commonClient.IssuedExecutionAuthorization{
				ID: "77", ExecutionID: payload.ExecutionID, Audience: "develop",
				EngineIDs: []string{"12"}, Effects: []string{"read"}, ExpiresAt: time.Now().Add(time.Minute),
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

func executeQueryRequestForTest(t *testing.T, handler *QueryHandler, sql string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/execute", func(c *gin.Context) {
		setTenantAuthContextForTest(c, 7, 1)
		handler.ExecuteQuery(c)
	})
	payload, err := json.Marshal(ExecuteQueryRequest{
		Content:         map[string]interface{}{"query_type": "sql", "query": sql},
		ExecutionConfig: map[string]interface{}{"engine_id": 12}, Timeout: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/execute", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer addp_at_user")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
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

func TestQueryRequestContentPreservesNativeLanguage(t *testing.T) {
	query, language, err := queryRequestContent(map[string]interface{}{
		"query_type": " Cypher ", "query": " MATCH (n) RETURN n LIMIT 10 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if language != "cypher" || query != "MATCH (n) RETURN n LIMIT 10" {
		t.Fatalf("queryRequestContent() = (%q, %q)", query, language)
	}
}
