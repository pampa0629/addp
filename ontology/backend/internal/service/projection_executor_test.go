package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	"github.com/addp/ontology/internal/models"
	"github.com/addp/ontology/internal/repository"
	"github.com/google/uuid"
)

type projectionTokenSource struct{ t *testing.T }

func (s projectionTokenSource) Token(_ context.Context, tenant uint) (string, error) {
	if tenant != 101 {
		s.t.Errorf("unexpected tenant %d", tenant)
	}
	return "addp_at_runtime_fixture", nil
}
func (s projectionTokenSource) PlatformToken(context.Context) (string, error) {
	return "", errors.New("platform token forbidden")
}

func TestProjectionSystemAuthorizerUsesTenantRuntimeAndExactLease(t *testing.T) {
	authorizationID := int64(91)
	w := &repository.ProjectionWork{
		Execution:  execution.TaskExecution{ExecutionAuthorizationID: &authorizationID, TenantID: 101},
		Revision:   models.Revision{TenantID: 101, OntologyID: "outdoor_beijing", Revision: 3, Digest: strings.Repeat("a", 64)},
		Projection: models.Projection{Generation: uuid.NewString()},
	}
	l := execution.Lease{ExecutionID: uuid.NewString(), TenantID: 101, Attempt: 1, Token: uuid.NewString(), Owner: "fixture"}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/system/execution-authorizations/91/internal-task-accesses" || r.Header.Get("Authorization") != "Bearer addp_at_runtime_fixture" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("incorrect runtime authorization request")
		}
		var req client.InternalTaskAccessRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ExecutionID != l.ExecutionID || req.Attempt != l.Attempt || req.LeaseToken != l.Token || req.InternalTask != w.Boundary() {
			t.Error("changed execution boundary")
		}
		if calls.Load() == 2 {
			rw.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(rw).Encode(client.InternalTaskAccess{AuthorizationID: "91", ExecutionID: l.ExecutionID, TenantID: "101", Audience: execution.AudienceOntology, Attempt: l.Attempt, InternalTask: w.Boundary(), ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer server.Close()
	a, err := NewSystemProjectionAuthorizer(client.NewSystemServiceClient(server.URL, projectionTokenSource{t}, server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authorize(context.Background(), w, l); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authorize(context.Background(), w, l); err == nil || calls.Load() != 2 {
		t.Fatal("authorization response cached or rejection ignored")
	}
	if _, err := a.Authorize(context.Background(), nil, l); err == nil || calls.Load() != 2 {
		t.Fatal("missing work sent to System")
	}
}

func TestProjectionRuntimeRejectsMissingDependencies(t *testing.T) {
	if _, err := NewSystemProjectionAuthorizer(nil); err == nil {
		t.Fatal("nil System client accepted")
	}
	if _, err := NewProjectionExecutor(nil, testProjectionGraph{}, testProjectionAuthorizer(testProjectionReceipt)); err == nil {
		t.Fatal("nil repository accepted")
	}
	if _, err := NewProjectionSupervisor(nil, nil, ""); err == nil {
		t.Fatal("missing supervisor dependencies accepted")
	}
	var s ProjectionSupervisor
	if err := s.Run(context.Background(), nil); err == nil {
		t.Fatal("implicit readiness accepted")
	}
}
