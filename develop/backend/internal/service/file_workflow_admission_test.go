package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
)

// Real original Owner SQL and current-machine System SDK. The System response
// is an explicit double; these observations are not real IAM/NFS or business T4.
func TestPrivateFileAdmissionUsesOriginalScopeAndNeverReadsSources(t *testing.T) {
	for _, scenario := range []string{"ready", "dependency", "forbidden", "wrong-execution", "wrong-scope", "wrong-attempt", "wrong-authorization", "rejected", "cancelled-during-observation", "changed-config", "expired-before-commit"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, item, facts, issued, _ := privateFilePendingFixture(t, false)
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/v1/system/execution-authorizations/9/develop-geopackage-admission-checks" || r.Method != "POST" || r.Header.Get("Authorization") != "Bearer addp_at_current_machine" {
					t.Error("observation used issuance, another route or stored user token", r.Method, r.URL.Path)
					w.WriteHeader(403)
					return
				}
				var req client.DevelopFileAdmissionRequest
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.ExecutionID != item.ExecutionID || req.ExpectedAttempt != 1 || req.ScopeDigest != facts.ScopeDigest {
					t.Error("original pending binding changed", req)
				}
				if scenario == "dependency" {
					w.WriteHeader(503)
					return
				}
				if scenario == "forbidden" {
					w.WriteHeader(403)
					return
				}
				observation := client.DevelopFileAdmissionObservation{AuthorizationID: issued.Authorization.ID, ExecutionID: item.ExecutionID, ExpectedAttempt: 1, ScopeDigest: issued.ScopeDigest, State: "ready", ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
				switch scenario {
				case "wrong-execution":
					observation.ExecutionID = uuid.NewString()
				case "wrong-scope":
					observation.ScopeDigest = strings.Repeat("f", 64)
				case "wrong-attempt":
					observation.ExpectedAttempt = 2
				case "wrong-authorization":
					observation.AuthorizationID = "10"
				case "rejected":
					observation.State = "rejected"
				case "cancelled-during-observation":
					if err := db.Model(item).Update("status", "cancelled").Error; err != nil {
						t.Error(err)
					}
				case "changed-config":
					if err := db.Model(item).Update("execution_config", `{"timeout":600,"changed":true}`).Error; err != nil {
						t.Error(err)
					}
				case "expired-before-commit":
					if err := db.Model(item).Update("authorization_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
						t.Error(err)
					}
				}
				_ = json.NewEncoder(w).Encode(observation)
			}))
			defer endpoint.Close()
			sdk := client.NewSystemServiceClient(endpoint.URL, staticServiceTokenSource("addp_at_current_machine"), endpoint.Client())
			executor := NewDevExecutor(nil, execution.NewTaskExecutionRepository(db), NewWorkflowEngineService(sdk), nil, nil, NewSQLEngineService(&config.Config{}, sdk, nil), nil, nil)
			service, err := NewExecutionService(executor, repository.NewExecutionRepository(db), repo, "fixture-instance")
			if err != nil {
				t.Fatal(err)
			}
			// No source or Runtime operation is an input to an eligibility observation.
			service.privateFiles.endpoint = func(context.Context, uint, uint) (plugin.ConnectionInfo, error) {
				t.Error("admission requested Runtime connection")
				return nil, fmt.Errorf("forbidden")
			}
			observeErr := service.RecoverPrivateFiles(t.Context())
			if (scenario == "ready" || scenario == "rejected") && observeErr != nil {
				t.Fatal(observeErr)
			}
			if scenario != "ready" && scenario != "rejected" && observeErr == nil {
				t.Fatal("unknown/stale observation accepted")
			}
			stored, err := repo.Read(t.Context(), 3, item.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "pending"
			wantStatus := "pending"
			if scenario == "ready" {
				wantState = "ready"
			}
			if scenario == "rejected" {
				wantState = "rejected"
				wantStatus = "failed"
			}
			if scenario == "cancelled-during-observation" {
				wantStatus = "cancelled"
			}
			var common execution.TaskExecution
			_ = db.First(&common, "execution_id = ?", item.ExecutionID).Error
			if stored.AdmissionState != wantState || common.Status != wantStatus || common.Attempt != 0 || common.StartedAt != nil || common.LeaseToken != nil {
				t.Fatal("observation fabricated a claim or terminal", stored.AdmissionState, common)
			}
			if common.ExecutionAuthorizationID == nil || *common.ExecutionAuthorizationID != 9 || stored.ScopeDigest != issued.ScopeDigest || calls.Load() != 1 {
				t.Fatal("original EA changed", common.ExecutionAuthorizationID, stored.ScopeDigest, issued.ScopeDigest, calls.Load())
			}
			var private models.FileExecution
			_ = db.First(&private, "execution_id = ?", item.ExecutionID).Error
			if private.InputDirectory != "" || private.RuntimeExecutionID != nil {
				t.Fatal("admission performed content I/O")
			}
			if scenario == "ready" {
				// A fresh observation does not cause duplicate SDK traffic or create a new EA.
				if err := service.RecoverPrivateFiles(t.Context()); err != nil || calls.Load() != 1 {
					t.Fatal("fresh qualification was reissued", err, calls.Load())
				}
				if err := db.Model(&private).Update("admission_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				if err := service.RecoverPrivateFiles(t.Context()); err != nil || calls.Load() != 2 {
					t.Fatal("expired qualification was not freshly observed", err, calls.Load())
				}
				claimed, lease, err := repository.NewExecutionRepository(db).ClaimNext("workflow", t.Context(), "ordinary-worker", time.Now(), time.Minute, nil)
				if err != nil || claimed == nil || lease == nil || claimed.ExecutionID != item.ExecutionID || claimed.Attempt != 1 {
					t.Fatal("original ready record was not claimable", claimed, err)
				}
				if err := db.Model(&private).Update("admission_expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
					t.Fatal(err)
				}
				if err := service.RecoverPrivateFiles(t.Context()); err != nil || calls.Load() != 2 {
					t.Fatal("running attempt entered pending observer", err, calls.Load())
				}
			}
		})
	}
}

func TestPrivateFileAdmissionDoesNotOpenSavedOrExpiredPurpose(t *testing.T) {
	for _, scenario := range []string{"save", "expired", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, item, _, _, _ := privateFilePendingFixture(t, scenario == "save")
			executor := NewDevExecutor(nil, execution.NewTaskExecutionRepository(db), nil, nil, nil, NewSQLEngineService(&config.Config{}, nil, nil), nil, nil)
			service, err := NewExecutionService(executor, repository.NewExecutionRepository(db), repo, "fixture-instance")
			if err != nil {
				t.Fatal(err)
			}
			service.privateFiles.admission = func(uint) fileAdmissionAuthority {
				return fileAdmissionAuthorityFunc(func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileAdmissionRequest) (*client.DevelopFileAdmissionObservation, error) {
					t.Error("closed purpose was observed")
					return nil, fmt.Errorf("must not be called")
				})
			}
			switch scenario {
			case "expired":
				_ = db.Model(item).Update("authorization_expires_at", time.Now().UTC().Add(-time.Minute)).Error
			case "cancelled":
				_ = db.Model(item).Update("status", "cancelled").Error
			}
			if err := service.RecoverPrivateFiles(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type fileAdmissionAuthorityFunc func(context.Context, client.IssuedDevelopFileAuthorization, client.DevelopFileAdmissionRequest) (*client.DevelopFileAdmissionObservation, error)

func (f fileAdmissionAuthorityFunc) CheckDevelopFileAdmission(ctx context.Context, issued client.IssuedDevelopFileAuthorization, request client.DevelopFileAdmissionRequest) (*client.DevelopFileAdmissionObservation, error) {
	return f(ctx, issued, request)
}

func TestPrivateFileAuthorityFactoriesBindOriginalTenant(t *testing.T) {
	facts, issued, lease, base := fileInputsFixture(t, false)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer addp_at_current_machine" {
			t.Error("not current tenant machine")
		}
		switch r.URL.Path {
		case "/api/v1/system/execution-authorizations/9/develop-geopackage-source-accesses":
			var request client.DevelopFileSourceAccessRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			response, err := fileInputsAuthority(base).CheckDevelopFileSourceAccess(r.Context(), issued, request)
			if err != nil {
				t.Error(err)
			}
			_ = json.NewEncoder(w).Encode(response)
		case "/api/v1/system/execution-authorizations/9/develop-geopackage-admission-checks":
			_ = json.NewEncoder(w).Encode(client.DevelopFileAdmissionObservation{AuthorizationID: issued.Authorization.ID, ExecutionID: issued.Authorization.ExecutionID, ScopeDigest: issued.ScopeDigest, ExpectedAttempt: 1, State: "ready", ObservedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)})
		default:
			t.Error("unexpected authority route", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	sdk := client.NewSystemServiceClient(server.URL, staticServiceTokenSource("addp_at_current_machine"), server.Client())
	f := newPrivateFileWorkflowExecutor(nil, NewWorkflowEngineService(sdk), "fixture-instance")
	source := client.DevelopFileSourceAccessRequest{ExecutionID: facts.ExecutionID, Attempt: 1, LeaseToken: lease.Token, ScopeDigest: facts.ScopeDigest, Source: facts.Intent.Sources[0].Target}
	if _, err := f.authority(3).CheckDevelopFileSourceAccess(t.Context(), issued, source); err != nil {
		t.Fatal("original Tenant source SDK unavailable", err)
	}
	admission := client.DevelopFileAdmissionRequest{ExecutionID: facts.ExecutionID, ExpectedAttempt: 1, ScopeDigest: facts.ScopeDigest}
	if _, err := f.admission(3).CheckDevelopFileAdmission(t.Context(), issued, admission); err != nil {
		t.Fatal("original Tenant admission SDK unavailable", err)
	}
	if requests.Load() != 2 {
		t.Fatal(requests.Load())
	}
	if _, err := f.authority(4).CheckDevelopFileSourceAccess(t.Context(), issued, source); err == nil {
		t.Fatal("cross-Tenant source accepted")
	}
	if _, err := f.admission(4).CheckDevelopFileAdmission(t.Context(), issued, admission); err == nil {
		t.Fatal("cross-Tenant admission accepted")
	}
	if requests.Load() != 2 {
		t.Fatal("wrong Tenant reached network")
	}
}
