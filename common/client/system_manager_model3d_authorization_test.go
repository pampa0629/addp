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

func model3DClientScope(t *testing.T) execution.ManagerFileReadScope {
	t.Helper()
	composition, err := authorizationfacts.NewFileCompositionBoundary("3ds", []authorizationfacts.FileCompositionRef{
		{Target: plugin.FileItemPath(12, "models/scene.3ds"), Role: "model", Primary: true, Required: true},
		{Target: plugin.FileItemPath(12, "models/Texture.PNG"), Role: "texture", Required: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return execution.ManagerFileReadScope{ConfigDigest: strings.Repeat("a", 64), OwnerTaskID: "10", OwnerTaskVersion: "7", EngineVersion: "5", FileComposition: *composition}
}

func model3DClientIssued(t *testing.T) IssuedManagerFileAuthorization {
	return IssuedManagerFileAuthorization{ID: "9", ExecutionID: profileClientExecution, TenantID: "3", Audience: "manager", SourceReadScope: model3DClientScope(t), ExpiresAt: time.Now().Add(15 * time.Minute)}
}

func model3DClientRequest(t *testing.T) ManagerFileAccessRequest {
	return ManagerFileAccessRequest{ExecutionID: profileClientExecution, Attempt: 2, LeaseToken: profileClientLease, SourceReadScope: model3DClientScope(t)}
}

func model3DClientObservation() ManagerFileAccessObservation {
	tenant := uint(3)
	return ManagerFileAccessObservation{ObservedAt: time.Now(), Engine: &models.Engine{ID: 12, TenantID: &tenant, Version: 5, EngineType: "ftp", LifecycleState: "active", ConnectionInfo: models.ConnectionInfo{"host": "current.fixture"}}}
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
			var request ManagerFileAccessRequest
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
	issued, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueManagerModel3D(context.Background(), "addp_at_current_user", IssueManagerFileAuthorizationRequest{ExecutionID: profileClientExecution})
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
	for name, mutate := range map[string]func(*ManagerFileAccessObservation){
		"missing":    func(r *ManagerFileAccessObservation) { r.Engine = nil },
		"time":       func(r *ManagerFileAccessObservation) { r.ObservedAt = time.Time{} },
		"engine":     func(r *ManagerFileAccessObservation) { r.Engine.ID++ },
		"tenant":     func(r *ManagerFileAccessObservation) { id := uint(4); r.Engine.TenantID = &id },
		"shared":     func(r *ManagerFileAccessObservation) { r.Engine.TenantID = nil },
		"version":    func(r *ManagerFileAccessObservation) { r.Engine.Version++ },
		"kind":       func(r *ManagerFileAccessObservation) { r.Engine.EngineType = "minio" },
		"deleted":    func(r *ManagerFileAccessObservation) { now := time.Now(); r.Engine.DeletedAt = &now },
		"inactive":   func(r *ManagerFileAccessObservation) { r.Engine.LifecycleState = "deleting" },
		"connection": func(r *ManagerFileAccessObservation) { r.Engine.ConnectionInfo = nil },
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
	for _, mutate := range []func(*IssuedManagerFileAuthorization){
		func(r *IssuedManagerFileAuthorization) { r.ID = "09" },
		func(r *IssuedManagerFileAuthorization) { r.TenantID = "0" },
		func(r *IssuedManagerFileAuthorization) { r.Audience = "meta" },
		func(r *IssuedManagerFileAuthorization) { r.ExecutionID = profileClientLease },
		func(r *IssuedManagerFileAuthorization) { r.SourceReadScope.OwnerTaskVersion = "0" },
		func(r *IssuedManagerFileAuthorization) { r.ExpiresAt = time.Now().Add(-time.Second) },
		func(r *IssuedManagerFileAuthorization) { r.ExpiresAt = time.Now().Add(2 * time.Hour) },
	} {
		response := model3DClientIssued(t)
		mutate(&response)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201); json.NewEncoder(w).Encode(response) }))
		got, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueManagerModel3D(context.Background(), "addp_at_user", IssueManagerFileAuthorizationRequest{ExecutionID: profileClientExecution})
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
		if _, err := issuer.IssueManagerModel3D(context.Background(), credential, IssueManagerFileAuthorizationRequest{ExecutionID: profileClientExecution}); err == nil {
			t.Fatal("invalid User credential accepted")
		}
		service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource(credential), server.Client()).WithTenantID(3)
		if _, err := service.CheckManagerModel3DAccess(context.Background(), "9", model3DClientRequest(t)); err == nil {
			t.Fatal("invalid Service credential accepted")
		}
	}
	for _, req := range []IssueManagerFileAuthorizationRequest{{ExecutionID: strings.ToUpper(profileClientExecution)}, {ExecutionID: profileClientExecution, ExpiresIn: -1}, {ExecutionID: profileClientExecution, ExpiresIn: 3601}} {
		if _, err := issuer.IssueManagerModel3D(context.Background(), "addp_at_user", req); err == nil {
			t.Fatal("invalid issue input accepted")
		}
	}
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
	for _, mutate := range []func(*ManagerFileAccessRequest){func(r *ManagerFileAccessRequest) { r.Attempt = 0 }, func(r *ManagerFileAccessRequest) { r.LeaseToken = "" }, func(r *ManagerFileAccessRequest) { r.SourceReadScope.EngineVersion = "05" }} {
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
