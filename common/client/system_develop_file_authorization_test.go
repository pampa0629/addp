package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/common/models"
)

const developFileClientExecution = "fa6c2d0e-8b34-4a16-b41c-d645619ea782"
const developFileClientLease = "05b4b5f6-3343-41c4-97d3-a0aa527e42dc"
const developFileClientApproval = "ca5846c6-1fbb-40ae-bfda-7ed0e7be08f4"

// These are mocked wire tests, not implemented System routes or IAM evidence.
func developFileClientFixture(t *testing.T, write bool) (execution.DevelopFileIntent, IssuedDevelopFileAuthorization) {
	t.Helper()
	var target *execution.DevelopFileTargetIntent
	if write {
		target = &execution.DevelopFileTargetIntent{NodeID: "save", Target: plugin.FileItemPath(12, "outputs/result.gpkg"), Mode: "fail"}
	}
	intent, err := execution.NewDevelopFileIntent(json.RawMessage(`{"task":"load-difference","number":9007199254740993}`), 1, 45,
		[]execution.DevelopFileSourceIntent{{NodeID: "load", Target: plugin.FileItemPath(11, "sources/A.gpkg")}}, target)
	if err != nil {
		t.Fatal(err)
	}
	version := ""
	if write {
		version = "4"
	}
	scope, err := execution.NewDevelopBoundFileScope(*intent, []execution.DevelopFileSourceReference{{Target: intent.Sources[0].Target,
		ItemFingerprint: strings.Repeat("a", 64), BoundaryHash: strings.Repeat("b", 64), EngineVersion: "2"}}, version)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := scope.Digest()
	return *intent, IssuedDevelopFileAuthorization{Authorization: IssuedExecutionAuthorization{ID: "9", ExecutionID: developFileClientExecution,
		Audience: "develop", TenantID: "3", ActorPrincipalID: "31", TenantMembershipID: "41", IssuedAuthorizationVersion: "5", SourceType: "user",
		Accesses: []ExecutionEngineAccessScope{{EngineID: "11", Effects: []string{"read"}}}, ExpiresAt: time.Now().Add(15 * time.Minute)}, Scope: *scope, ScopeDigest: digest}
}

func developFileClientAdmission(issued IssuedDevelopFileAuthorization) DevelopFileAdmissionObservation {
	value := DevelopFileAdmissionObservation{AuthorizationID: issued.Authorization.ID, ExecutionID: developFileClientExecution,
		ScopeDigest: issued.ScopeDigest, ExpectedAttempt: 1, State: "ready", ObservedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Minute)}
	if issued.Scope.Target != nil {
		value.ApprovalID = developFileClientApproval
	}
	return value
}

func developFileClientSource(issued IssuedDevelopFileAuthorization) DevelopFileSourceAccessObservation {
	tenant := uint(3)
	return DevelopFileSourceAccessObservation{AuthorizationID: "9", ExecutionID: developFileClientExecution, Attempt: 1, ScopeDigest: issued.ScopeDigest,
		Source: issued.Scope.Sources[0].Target, ObservedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(20 * time.Second), BusinessDeadline: time.Now().Add(40 * time.Second),
		Engine: &models.Engine{ID: 11, TenantID: &tenant, EngineType: "nfs", Version: 2, LifecycleState: models.EngineLifecycleActive,
			ConnectionInfo: models.ConnectionInfo{"server": "fixture-only", "export_path": "/owned"}}}
}

func developFileClientAccessRequest(issued IssuedDevelopFileAuthorization) DevelopFileSourceAccessRequest {
	return DevelopFileSourceAccessRequest{ExecutionID: developFileClientExecution, Attempt: 1, LeaseToken: developFileClientLease,
		ScopeDigest: issued.ScopeDigest, Source: issued.Scope.Sources[0].Target}
}

func TestDevelopFileClientPublicationChecksPhaseAndTarget(t *testing.T) {
	_, issued := developFileClientFixture(t, true)
	for _, scenario := range []struct {
		name, stage                         string
		engine, wrongEngine, wrongOperation bool
		wantOK                              bool
	}{
		{"prepare", DevelopFilePublicationPrepare, true, false, false, true},
		{"wrong target", DevelopFilePublicationPrepare, true, true, false, false},
		{"wrong operation", DevelopFilePublicationPrepare, true, false, true, false},
		{"release", DevelopFilePublicationRelease, false, false, false, true},
		{"release connection leak", DevelopFilePublicationRelease, true, false, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := DevelopFilePublicationRequest{ExecutionID: developFileClientExecution, Attempt: 1,
				LeaseToken: developFileClientLease, ScopeDigest: issued.ScopeDigest, ApprovalID: developFileClientApproval,
				OperationID: "0b33cbf0-5c67-4c99-8ac3-5573d505630f", Stage: scenario.stage}
			if scenario.stage == DevelopFilePublicationRelease {
				request.PreparationDigest = strings.Repeat("d", 64)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var received DevelopFilePublicationRequest
				if r.Method != "POST" || r.URL.Path != "/api/v1/system/execution-authorizations/9/develop-geopackage-publications" ||
					r.Header.Get("Authorization") != "Bearer addp_at_service" || json.NewDecoder(r.Body).Decode(&received) != nil || received != request {
					t.Error("publication request lost its machine or execution binding")
				}
				now := time.Now()
				result := DevelopFilePublicationObservation{AuthorizationID: "9", ExecutionID: request.ExecutionID, Attempt: request.Attempt,
					ScopeDigest: request.ScopeDigest, ApprovalID: request.ApprovalID, OperationID: request.OperationID,
					Stage: request.Stage, PreparationDigest: request.PreparationDigest, ObservedAt: now.Add(-time.Second),
					ExpiresAt: now.Add(15 * time.Second), BusinessDeadline: now.Add(30 * time.Second)}
				if scenario.engine {
					tenant := uint(3)
					result.Engine = &models.Engine{ID: 12, TenantID: &tenant, EngineType: "nfs", Version: 4,
						LifecycleState: models.EngineLifecycleActive, ConnectionInfo: models.ConnectionInfo{"server": "target-only"}}
					if scenario.wrongEngine {
						result.Engine.ID = 11
					}
				}
				if scenario.wrongOperation {
					result.OperationID = developFileClientExecution
				}
				_ = json.NewEncoder(w).Encode(result)
			}))
			defer server.Close()
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
			value, err := service.AuthorizeDevelopFilePublication(t.Context(), issued, request)
			if (err == nil) != scenario.wantOK || (value != nil) != scenario.wantOK || calls.Load() != 1 {
				t.Fatal("invalid result or command replay", err, calls.Load())
			}
		})
	}
}

func TestDevelopFileClientSeparatesUserIssuanceAndFreshMachineChecks(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-only", true: "save"}[write], func(t *testing.T) {
			intent, expected := developFileClientFixture(t, write)
			var checks atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("X-Tenant-ID") != "" {
					t.Error("expanded fixed-purpose request")
				}
				switch r.URL.Path {
				case "/api/v1/system/auth/execution-authorizations/develop-geopackage-workflows":
					var fields map[string]json.RawMessage
					if r.Header.Get("Authorization") != "Bearer addp_at_user" || json.NewDecoder(r.Body).Decode(&fields) != nil || len(fields) != 2 || fields["execution_id"] == nil || fields["intent_digest"] == nil {
						t.Error("issuance supplied identity/hash facts or wrong credential")
					}
					w.WriteHeader(201)
					_ = json.NewEncoder(w).Encode(expected)
				case "/api/v1/system/execution-authorizations/9/develop-geopackage-admission-checks":
					checks.Add(1)
					if r.Header.Get("Authorization") != "Bearer addp_at_service" {
						t.Error("machine used a saved User credential")
					}
					_ = json.NewEncoder(w).Encode(developFileClientAdmission(expected))
				case "/api/v1/system/execution-authorizations/9/develop-geopackage-source-accesses":
					if r.Header.Get("Authorization") != "Bearer addp_at_service" {
						t.Error("wrong source credential")
					}
					var request DevelopFileSourceAccessRequest
					if json.NewDecoder(r.Body).Decode(&request) != nil || request.LeaseToken != developFileClientLease || request.ScopeDigest != expected.ScopeDigest {
						t.Error("lost execution binding")
					}
					_ = json.NewEncoder(w).Encode(developFileClientSource(expected))
				default:
					t.Error("unexpected endpoint", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			issued, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueDevelopFile(t.Context(), "addp_at_user", developFileClientExecution, intent)
			if err != nil {
				t.Fatal(err)
			}
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
			request := DevelopFileAdmissionRequest{ExecutionID: developFileClientExecution, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest}
			for i := 0; i < 2; i++ {
				if value, err := service.CheckDevelopFileAdmission(t.Context(), *issued, request); err != nil || value.State != "ready" {
					t.Fatal(value, err)
				}
			}
			if checks.Load() != 2 {
				t.Fatal("admission observation cached")
			}
			if value, err := service.CheckDevelopFileSourceAccess(t.Context(), *issued, developFileClientAccessRequest(*issued)); err != nil || value.Engine.ID != 11 {
				t.Fatal(value, err)
			}
		})
	}
}

func TestDevelopFileClientRejectsUnboundOrExpandedIssuance(t *testing.T) {
	for _, fault := range []string{"execution", "actor", "service-source", "engine-write", "expired", "scope-digest", "hidden-source", "hidden-target", "unknown-field", "duplicate-key"} {
		t.Run(fault, func(t *testing.T) {
			intent, response := developFileClientFixture(t, false)
			switch fault {
			case "execution":
				response.Authorization.ExecutionID = developFileClientLease
			case "actor":
				response.Authorization.ActorPrincipalID = "031"
			case "service-source":
				response.Authorization.SourceType = "service_definition"
			case "engine-write":
				response.Authorization.Accesses[0].Effects = []string{"write"}
			case "expired":
				response.Authorization.ExpiresAt = time.Now().Add(-time.Second)
			case "scope-digest":
				response.ScopeDigest = strings.Repeat("a", 64)
			case "hidden-source":
				response.Scope.Sources[0].Target.Segments[2].Name = "hidden.gpkg"
				response.ScopeDigest, _ = response.Scope.Digest()
			case "hidden-target":
				response.Scope.Target = &execution.DevelopBoundFileTarget{Target: plugin.FileItemPath(12, "outputs/hidden.gpkg"), Mode: "fail", EngineVersion: "2"}
				response.ScopeDigest, _ = response.Scope.Digest()
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(201)
				encoded, _ := json.Marshal(response)
				if fault == "unknown-field" {
					encoded = append([]byte(`{"extra":true,`), encoded[1:]...)
				}
				if fault == "duplicate-key" {
					encoded = append([]byte(`{"scope_digest":"duplicate",`), encoded[1:]...)
				}
				_, _ = w.Write(encoded)
			}))
			defer server.Close()
			if got, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueDevelopFile(t.Context(), "addp_at_user", developFileClientExecution, intent); got != nil || err == nil {
				t.Fatal("invalid System seal accepted", got, err)
			}
		})
	}
}

func TestDevelopFileClientReadOnlyAdmissionCannotAcquireWriteApproval(t *testing.T) {
	for _, fault := range []string{"awaiting", "approval", "unknown-state", "wrong-attempt", "wrong-scope", "expired", "extended", "engine-payload"} {
		t.Run(fault, func(t *testing.T) {
			_, issued := developFileClientFixture(t, false)
			observation := developFileClientAdmission(issued)
			switch fault {
			case "awaiting":
				observation.State = "awaiting_approval"
			case "approval":
				observation.ApprovalID = developFileClientApproval
			case "unknown-state":
				observation.State = "maybe"
			case "wrong-attempt":
				observation.ExpectedAttempt++
			case "wrong-scope":
				observation.ScopeDigest = strings.Repeat("a", 64)
			case "expired":
				observation.ExpiresAt = time.Now().Add(-time.Second)
			case "extended":
				observation.ExpiresAt = issued.Authorization.ExpiresAt.Add(time.Second)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				encoded, _ := json.Marshal(observation)
				if fault == "engine-payload" {
					encoded = append([]byte(`{"engine":{"connection_info":{}},`), encoded[1:]...)
				}
				_, _ = w.Write(encoded)
			}))
			defer server.Close()
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
			if value, err := service.CheckDevelopFileAdmission(t.Context(), issued, DevelopFileAdmissionRequest{ExecutionID: developFileClientExecution, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest}); value != nil || err == nil {
				t.Fatal("invalid read-only admission accepted", value, err)
			}
		})
	}
}

func TestDevelopFileClientBindsCurrentExactSourceAndTenant(t *testing.T) {
	for _, fault := range []string{"version", "tenant", "source", "engine-type", "inactive", "expired", "missing-engine", "missing-business-deadline", "window-after-business-deadline", "business-deadline-after-ea", "expired-business-deadline"} {
		t.Run(fault, func(t *testing.T) {
			_, issued := developFileClientFixture(t, false)
			response := developFileClientSource(issued)
			switch fault {
			case "version":
				response.Engine.Version++
			case "tenant":
				other := uint(4)
				response.Engine.TenantID = &other
			case "source":
				response.Source = plugin.FileItemPath(11, "sources/neighbor.gpkg")
			case "engine-type":
				response.Engine.EngineType = "ftp"
			case "inactive":
				response.Engine.LifecycleState = models.EngineLifecycleDisabled
			case "expired":
				response.ExpiresAt = time.Now().Add(-time.Second)
			case "missing-engine":
				response.Engine = nil
			case "missing-business-deadline":
				response.BusinessDeadline = time.Time{}
			case "window-after-business-deadline":
				response.BusinessDeadline = response.ExpiresAt.Add(-time.Second)
			case "business-deadline-after-ea":
				response.BusinessDeadline = issued.Authorization.ExpiresAt.Add(time.Second)
			case "expired-business-deadline":
				response.BusinessDeadline = time.Now().Add(-time.Second)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(response) }))
			defer server.Close()
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
			if value, err := service.CheckDevelopFileSourceAccess(t.Context(), issued, developFileClientAccessRequest(issued)); value != nil || err == nil {
				t.Fatal("unbound source engine accepted", value, err)
			}
		})
	}
}

func TestDevelopFileClientRejectsBadLocalBindingsBeforeNetwork(t *testing.T) {
	_, issued := developFileClientFixture(t, false)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
	for _, mutate := range []func(*DevelopFileSourceAccessRequest){
		func(r *DevelopFileSourceAccessRequest) { r.Attempt++ },
		func(r *DevelopFileSourceAccessRequest) { r.LeaseToken = "not-a-lease" },
		func(r *DevelopFileSourceAccessRequest) { r.ScopeDigest = "other" },
		func(r *DevelopFileSourceAccessRequest) { r.Source = plugin.FileItemPath(11, "sources/neighbor.gpkg") },
	} {
		r := developFileClientAccessRequest(issued)
		mutate(&r)
		if _, err := service.CheckDevelopFileSourceAccess(t.Context(), issued, r); err == nil {
			t.Fatal("invalid local binding accepted")
		}
	}
	if _, err := service.WithTenantID(4).CheckDevelopFileSourceAccess(t.Context(), issued, developFileClientAccessRequest(issued)); err == nil || calls.Load() != 0 {
		t.Fatal("wrong Tenant or binding performed network I/O")
	}
}

func TestDevelopFileClientNoRedirectRetryOrUpstreamBodyLeak(t *testing.T) {
	for _, status := range []int{302, 401, 403, 409, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			_, issued := developFileClientFixture(t, false)
			var original, redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				original.Add(1)
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"private-credential-detail","error_code":"source_access_denied"}`))
			}))
			defer server.Close()
			service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
			value, err := service.CheckDevelopFileSourceAccess(context.Background(), issued, developFileClientAccessRequest(issued))
			if value != nil || err == nil || strings.Contains(err.Error(), "private-credential-detail") || original.Load() != 1 || redirected.Load() != 0 {
				t.Fatal("failure replayed, followed redirect, leaked or became Allow", value, err)
			}
		})
	}
}

func TestDevelopFileClientFreezesIntentBeforeIssuanceHTTP(t *testing.T) {
	intent, expected := developFileClientFixture(t, true)
	changed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		intent.Sources[0].Target.Segments[2].Name = "caller-changed.gpkg"
		intent.Target.Target.Segments[2].Name = "caller-changed-output.gpkg"
		close(changed)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()
	value, err := NewSystemExecutionAuthorizationClient(server.URL, server.Client()).IssueDevelopFile(t.Context(), "addp_at_user", developFileClientExecution, intent)
	<-changed
	if err != nil || value == nil || value.Scope.Sources[0].Target.StringPath() != "sources/A.gpkg" || value.Scope.Target.Target.StringPath() != "outputs/result.gpkg" {
		t.Fatal("caller mutation altered the issuance boundary", value, err)
	}
}

func TestDevelopFileClientFreezesSourceScopeBeforeConsumptionHTTP(t *testing.T) {
	_, issued := developFileClientFixture(t, false)
	expected := *issued.Clone()
	changed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issued.Scope.Sources[0].Target.Segments[2].Name = "caller-changed.gpkg"
		close(changed)
		_ = json.NewEncoder(w).Encode(developFileClientSource(expected))
	}))
	defer server.Close()
	service := NewSystemServiceClient(server.URL, staticSystemServiceTokenSource("addp_at_service"), server.Client()).WithTenantID(3)
	value, err := service.CheckDevelopFileSourceAccess(t.Context(), issued, developFileClientAccessRequest(issued))
	<-changed
	if err != nil || value == nil || value.Source.StringPath() != "sources/A.gpkg" {
		t.Fatal("caller mutation changed the exact source response check", value, err)
	}
}
