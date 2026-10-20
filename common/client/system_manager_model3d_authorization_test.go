package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
)

func model3DClientScope(t *testing.T) execution.ManagerModel3DReadScope {
	t.Helper()
	composition, err := authorizationfacts.NewFileCompositionBoundary("3ds", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(12, "models/scene.3ds"), Role: "model", Primary: true, Required: true},
		{Target: plugin.FileItemPath(12, "models/Texture.PNG"), Role: "texture", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return execution.ManagerModel3DReadScope{ConfigDigest: strings.Repeat("a", 64), OwnerTaskID: "10", OwnerTaskVersion: "7", EngineVersion: "5", FileComposition: *composition}
}

func model3DClientIssued(t *testing.T) IssuedManagerModel3DAuthorization {
	return IssuedManagerModel3DAuthorization{ID: "9", ExecutionID: profileClientExecution, TenantID: "3", Audience: "manager", SourceReadScope: model3DClientScope(t), ExpiresAt: time.Now().Add(15 * time.Minute)}
}

func model3DClientRequest(t *testing.T) ManagerModel3DAccessRequest {
	return ManagerModel3DAccessRequest{ExecutionID: profileClientExecution, Attempt: 2, LeaseToken: profileClientLease, SourceReadScope: model3DClientScope(t)}
}

func model3DClientObservation() ManagerModel3DAccessObservation {
	tenant := uint(3)
	return ManagerModel3DAccessObservation{ObservedAt: time.Now(), Engine: &models.Engine{ID: 12, TenantID: &tenant, Version: 5, EngineType: "ftp", LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "current.fixture"}}}
}

func TestManagerModel3DClientUsesExactUserAndCurrentServiceContracts(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" || r.Header.Get("X-Internal-API-Key") != "" {
			t.Error("expanded request")
		}
		switch r.URL.Path {
		case "/api/v1/system/auth/execution-authorizations/manager-model3d-glb":
			if r.Header.Get("Authorization") != "Bearer addp_at_current_user" {
				t.Error("wrong issuer credential")
			}
			var request map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 1 || request["execution_id"] == nil {
				t.Error("self reported issuance source or Actor")
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(model3DClientIssued(t))
		case "/api/v1/system/execution-authorizations/9/manager-model3d-glb-accesses":
			if r.Header.Get("Authorization") != "Bearer addp_at_current_service" {
				t.Error("saved User credential used")
			}
			var request ManagerModel3DAccessRequest
			if json.NewDecoder(r.Body).Decode(&request) != nil || !reflect.DeepEqual(request, model3DClientRequest(t)) {
				t.Error("missing frozen execution binding")
			}
			response := model3DClientObservation()
			response.Engine.ConnectionInfo["observation"] = calls
			json.NewEncoder(w).Encode(response)
		default:
			t.Error("unexpected path")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	issued, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueManagerModel3D(context.Background(), "addp_at_current_user", IssueManagerModel3DAuthorizationRequest{ExecutionID: profileClientExecution})
	if err != nil || !issued.Matches(3, model3DClientScope(t)) || issued.Matches(4, model3DClientScope(t)) {
		t.Fatalf("issuance rejected: %v", err)
	}
	changed := model3DClientScope(t)
	changed.EngineVersion = "6"
	if issued.Matches(3, changed) {
		t.Fatal("different Engine version matched")
	}
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_current_service"), server.Client()).WithTenantID(3)
	for i := 0; i < 2; i++ {
		observed, err := service.CheckManagerModel3DAccess(context.Background(), issued.ID, model3DClientRequest(t))
		if err != nil || observed.Engine.ConnectionInfo["observation"] != float64(i+2) {
			t.Fatalf("current connection observation rejected/cached: %v", err)
		}
	}
	if calls != 3 {
		t.Fatal("current authorization cached")
	}
}

func TestManagerModel3DClientRejectsForeignStaleOrExpandedObservations(t *testing.T) {
	for name, mutate := range map[string]func(*ManagerModel3DAccessObservation){
		"missing":    func(r *ManagerModel3DAccessObservation) { r.Engine = nil },
		"time":       func(r *ManagerModel3DAccessObservation) { r.ObservedAt = time.Time{} },
		"engine":     func(r *ManagerModel3DAccessObservation) { r.Engine.ID++ },
		"tenant":     func(r *ManagerModel3DAccessObservation) { id := uint(4); r.Engine.TenantID = &id },
		"shared":     func(r *ManagerModel3DAccessObservation) { r.Engine.TenantID = nil },
		"version":    func(r *ManagerModel3DAccessObservation) { r.Engine.Version++ },
		"kind":       func(r *ManagerModel3DAccessObservation) { r.Engine.EngineType = "nfs" },
		"deleted":    func(r *ManagerModel3DAccessObservation) { now := time.Now(); r.Engine.DeletedAt = &now },
		"inactive":   func(r *ManagerModel3DAccessObservation) { r.Engine.LifecycleState = "deleting" },
		"connection": func(r *ManagerModel3DAccessObservation) { r.Engine.ConnectionInfo = nil },
	} {
		t.Run(name, func(t *testing.T) {
			response := model3DClientObservation()
			mutate(&response)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(response) }))
			defer server.Close()
			got, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3).CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t))
			if err == nil || got != nil {
				t.Fatal("invalid current observation accepted")
			}
		})
	}
	valid, _ := json.Marshal(model3DClientObservation())
	for _, body := range []string{`null`, `{}`, string(valid) + `{}`, strings.Replace(string(valid), `"engine":`, `"allow":true,"engine":`, 1), strings.Replace(string(valid), `"engine":`, `"engine":null,"engine":`, 1), strings.Replace(string(valid), `"version":5`, `"version":4,"version":5`, 1), strings.Repeat("x", 513<<10), "{\"private\":\"\xff\"}"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
		got, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3).CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t))
		server.Close()
		if err == nil || got != nil || strings.Contains(err.Error(), "private") {
			t.Fatal("ambiguous/expanded response accepted or leaked")
		}
	}
}

func TestManagerModel3DClientRejectsInvalidIssuanceAndInputs(t *testing.T) {
	for _, mutate := range []func(*IssuedManagerModel3DAuthorization){
		func(r *IssuedManagerModel3DAuthorization) { r.ID = "09" },
		func(r *IssuedManagerModel3DAuthorization) { r.TenantID = "0" },
		func(r *IssuedManagerModel3DAuthorization) { r.Audience = "meta" },
		func(r *IssuedManagerModel3DAuthorization) { r.ExecutionID = profileClientLease },
		func(r *IssuedManagerModel3DAuthorization) { r.SourceReadScope.OwnerTaskVersion = "0" },
		func(r *IssuedManagerModel3DAuthorization) { r.ExpiresAt = time.Now().Add(-time.Second) },
		func(r *IssuedManagerModel3DAuthorization) { r.ExpiresAt = time.Now().Add(2 * time.Hour) },
	} {
		response := model3DClientIssued(t)
		mutate(&response)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); json.NewEncoder(w).Encode(response) }))
		got, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueManagerModel3D(context.Background(), "addp_at_user", IssueManagerModel3DAuthorizationRequest{ExecutionID: profileClientExecution})
		server.Close()
		if err == nil || got != nil {
			t.Fatal("invalid issuance accepted")
		}
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	issuer := NewSystemExecutionAuthorizationClient(server.URL, server.Client())
	for _, credential := range []string{"", "addp_dat_delegate", "addp_at_", "addp_at_bad\nvalue"} {
		if _, err := issuer.IssueManagerModel3D(context.Background(), credential, IssueManagerModel3DAuthorizationRequest{ExecutionID: profileClientExecution}); err == nil {
			t.Fatal("invalid User credential accepted")
		}
		service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource(credential), server.Client()).WithTenantID(3)
		if _, err := service.CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t)); err == nil {
			t.Fatal("invalid Service credential accepted")
		}
	}
	for _, req := range []IssueManagerModel3DAuthorizationRequest{{ExecutionID: strings.ToUpper(profileClientExecution)}, {ExecutionID: profileClientExecution, ExpiresIn: -1}, {ExecutionID: profileClientExecution, ExpiresIn: 3601}} {
		if _, err := issuer.IssueManagerModel3D(context.Background(), "addp_at_user", req); err == nil {
			t.Fatal("invalid issue input accepted")
		}
	}
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
	for _, mutate := range []func(*ManagerModel3DAccessRequest){func(r *ManagerModel3DAccessRequest) { r.Attempt = 0 }, func(r *ManagerModel3DAccessRequest) { r.LeaseToken = "" }, func(r *ManagerModel3DAccessRequest) { r.SourceReadScope.EngineVersion = "05" }} {
		request := model3DClientRequest(t)
		mutate(&request)
		if _, err := service.CheckManagerModel3DAccess(context.Background(), "9", request); err == nil {
			t.Fatal("invalid consume input accepted")
		}
	}
	if _, err := service.CheckManagerModel3DAccess(context.Background(), "09", model3DClientRequest(t)); err == nil {
		t.Fatal("invalid authorization ID accepted")
	}
	if _, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t)); err == nil {
		t.Fatal("unbound tenant accepted")
	}
	if calls != 0 {
		t.Fatal("invalid input reached network")
	}
}

func TestManagerModel3DClientSinglePostNoRedirectAndSafeErrors(t *testing.T) {
	for _, status := range []int{400, 401, 403, 409, 500} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.WriteHeader(status)
			w.Write([]byte(`{"error":"private upstream password","error_code":"unsafe_private_code"}`))
		}))
		got, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3).CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t))
		server.Close()
		var apiError *SystemAPIError
		if got != nil || !errors.As(err, &apiError) || apiError.StatusCode != status || calls != 1 || apiError.ErrorMessage != "" || apiError.ResponseBody != "" || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "addp_at_") {
			t.Fatal("unsafe or retried failure")
		}
	}
	destinationCalls := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls++ }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 307) }))
	defer server.Close()
	transport := server.Client()
	_, err := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), transport).WithTenantID(3).CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t))
	if err == nil || destinationCalls != 0 || transport.CheckRedirect != nil {
		t.Fatal("redirect followed or caller transport mutated")
	}
}
