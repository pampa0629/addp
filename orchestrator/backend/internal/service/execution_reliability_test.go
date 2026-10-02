package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	execution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"gorm.io/gorm"
)

func reliabilityFixture(t *testing.T, steps models.Steps) (*ExecutionService, *execution.TaskExecution, execution.Lease) {
	t.Helper()
	db := newOrchestratorExecutionServiceTestDB(t)
	definition := models.Orchestration{ID: 11, TenantID: 7, Name: "Reliability", Steps: steps, EditorLayout: commonModels.JSONMap{}}
	if err := db.Create(&definition).Error; err != nil {
		t.Fatal(err)
	}
	service := NewExecutionService(db)
	item, err := service.CreateExecutionWithContext(t.Context(), 11, 7, "manual", "orchestrator", nil, ExecutionActor{PrincipalID: 9, TenantMembershipID: 19, AuthorizationVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	_, lease, err := service.ClaimNext(t.Context(), "reliability-owner", time.Minute)
	if err != nil || lease == nil {
		t.Fatalf("claim: %v", err)
	}
	return service, item, *lease
}

func testSteps() models.Steps {
	return models.Steps{{ID: "first", Name: "First", Provider: "quality", TaskType: "check", TaskID: 41}}
}

func reliabilityExecutor(t *testing.T, service *ExecutionService, handler http.HandlerFunc) *Executor {
	return reliabilityProviderExecutor(t, service, handler, "quality", "check")
}

func reliabilityProviderExecutor(t *testing.T, service *ExecutionService, handler http.HandlerFunc, provider, taskType string) *Executor {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &Executor{executionService: service,
		taskProviderResolver: taskProviderResolverWithProvider(&commonModels.TaskProvider{ModuleName: provider, Backends: taskProviderBackendsForTest(server.URL), Available: true, TaskProviderDeclaration: commonModels.TaskProviderDeclaration{TaskExecuteEndpoint: "/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/executions/{execution_id}", Capabilities: jsonStringPtr(taskCapabilitiesForTest(taskType, false, `{"type":"object","additionalProperties":false}`))}}, `{"type":"object","additionalProperties":false}`),
		serviceTokens:        commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "token", nil })}
}

func TestAdmissionRejectsInvalidPlanWithoutCreatingExecution(t *testing.T) {
	db := newOrchestratorExecutionServiceTestDB(t)
	if err := db.Create(&models.Orchestration{ID: 11, TenantID: 7, Name: "Invalid", Steps: models.Steps{}, EditorLayout: commonModels.JSONMap{}}).Error; err != nil {
		t.Fatal(err)
	}
	s := NewExecutionService(db)
	if _, err := s.CreateExecutionWithContext(t.Context(), 11, 7, "manual", "orchestrator", nil, ExecutionActor{9, 19, 3}); err == nil {
		t.Fatal("invalid plan admitted")
	}
	var count int64
	if err := db.Model(&execution.TaskExecution{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestFrozenPlanSurvivesDefinitionDeletionAndUsesActualProgress(t *testing.T) {
	steps := append(testSteps(), models.Step{ID: "second", Name: "Second", Provider: "quality", TaskType: "check", TaskID: 42, DependsOn: []string{"first"}})
	s, item, lease := reliabilityFixture(t, steps)
	if err := s.db.Delete(&models.Orchestration{}, 11).Error; err != nil {
		t.Fatal(err)
	}
	var posts atomic.Int32
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			n := posts.Add(1)
			w.WriteHeader(202)
			fmt.Fprintf(w, `{"execution_id":"child-%d"}`, n)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/executions/")
		status := "success"
		if id == "child-2" {
			status = "failed"
		}
		fmt.Fprintf(w, `{"execution_id":%q,"status":%q,"error_details":{"message":"password=private"}}`, id, status)
	})
	for range 4 {
		if err := e.Advance(t.Context(), lease); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := s.GetExecution(t.Context(), uint(item.ID), 7)
	if err != nil {
		t.Fatal(err)
	}
	results, _ := readStepResults(stored.Metadata)
	if stored.Status != "failed" || stored.Progress != 50 || posts.Load() != 2 || extractProviderExecutionID(results["second"].Result) != "child-2" {
		t.Fatalf("status=%s progress=%d results=%+v posts=%d", stored.Status, stored.Progress, results, posts.Load())
	}
	if strings.Contains(results["second"].Error, "private") {
		t.Fatal("downstream private error leaked")
	}
}

func TestUncertainSubmissionIsNotReplayed(t *testing.T) {
	for _, response := range []string{`{}`, `invalid-json`} {
		t.Run(response, func(t *testing.T) {
			s, item, lease := reliabilityFixture(t, testSteps())
			var posts atomic.Int32
			e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts.Add(1)
				}
				w.WriteHeader(202)
				fmt.Fprint(w, response)
			})
			if err := e.Advance(t.Context(), lease); err != nil {
				t.Fatal(err)
			}
			if err := e.Advance(t.Context(), lease); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("late advance=%v", err)
			}
			stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
			if posts.Load() != 1 || stored.Status != "failed" || stored.Progress != 0 || stored.ErrorDetails["code"] != "orchestrator.execution.dispatch_uncertain" {
				t.Fatalf("posts=%d execution=%+v", posts.Load(), stored)
			}
		})
	}
}

func TestSubmissionRedirectCannotReplayPost(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var posts atomic.Int32
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		w.Header().Set("Location", "/redirected")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if posts.Load() != 1 || stored.ErrorDetails["code"] != "orchestrator.execution.dispatch_uncertain" {
		t.Fatalf("replayed redirect: posts=%d error=%v", posts.Load(), stored.ErrorDetails)
	}
}

func TestLeaseTupleFencesProgressAndSubmission(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var calls atomic.Int32
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	for _, mutate := range []func(*execution.Lease){func(l *execution.Lease) { l.Token = "stale" }, func(l *execution.Lease) { l.Attempt++ }, func(l *execution.Lease) { l.Owner = "other" }, func(l *execution.Lease) { l.TenantID++ }} {
		stale := lease
		mutate(&stale)
		if err := e.Advance(t.Context(), stale); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("advance stale=%v", err)
		}
		if err := s.UpdateStepResults(t.Context(), stale, models.StepResults{"first": {Status: "success"}}, "first"); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("progress stale=%v", err)
		}
		if err := s.FinishExecution(t.Context(), stale, "success", ""); !errors.Is(err, commonapi.ErrConflict) {
			t.Fatalf("finish stale=%v", err)
		}
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if calls.Load() != 0 || stored.Progress != 0 || stored.Status != "running" {
		t.Fatal("stale lease changed execution or sent HTTP")
	}
}

func TestExpiredIntentFailsWithoutResubmission(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var calls atomic.Int32
	results := models.StepResults{"first": {Status: "running", Phase: "dispatching", StartedAt: time.Now().UTC()}}
	if err := s.UpdateStepResults(t.Context(), lease, results, "first"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&execution.TaskExecution{}).Where("id=?", item.ID).Update("lease_expires_at", time.Now().UTC().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	if err := e.Advance(t.Context(), lease); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("expired advance=%v", err)
	}
	n, err := s.RecoverExpired(t.Context(), time.Now().UTC(), 100)
	if err != nil || n != 1 {
		t.Fatalf("recover %d %v", n, err)
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if calls.Load() != 0 || stored.Status != "failed" || stored.ErrorDetails["code"] != "orchestrator.execution.dispatch_uncertain" || stored.Metadata["step_results"] == nil {
		t.Fatalf("stored=%+v calls=%d", stored, calls.Load())
	}
}

func TestWaitingChildIsDurableAndTimeoutDoesNotCancelIt(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var calls atomic.Int32
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(202)
		fmt.Fprint(w, `{"execution_id":"child"}`)
	})
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	results, _ := readStepResults(stored.Metadata)
	if results["first"].Phase != "waiting" || extractProviderExecutionID(results["first"].Result) != "child" || calls.Load() != 1 {
		t.Fatal("child identity not durable before poll")
	}
	result := results["first"]
	result.StartedAt = time.Now().Add(-time.Hour)
	results["first"] = result
	if err := s.UpdateStepResults(t.Context(), lease, results, "first"); err != nil {
		t.Fatal(err)
	}
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	stored, _ = s.GetExecution(t.Context(), uint(item.ID), 7)
	if calls.Load() != 1 || stored.ErrorDetails["code"] != "orchestrator.execution.step_timeout" {
		t.Fatalf("timeout sent extra call: %d %v", calls.Load(), stored.ErrorDetails)
	}
}

func TestUnicodeStepIdentityDoesNotBreakExecutionEvents(t *testing.T) {
	steps := testSteps()
	steps[0].ID = "扫描步骤"
	s, item, lease := reliabilityFixture(t, steps)
	if err := s.UpdateStepResults(t.Context(), lease, models.StepResults{"扫描步骤": {Status: "running", Phase: "dispatching"}}, "扫描步骤"); err != nil {
		t.Fatal(err)
	}
	page, err := execution.NewTaskExecutionRepository(s.db).ListEvents(t.Context(), item.ExecutionID, 7, 0, 100)
	if err != nil || len(page.Items) != 2 || page.Items[1].StepID != "扫描步骤" {
		t.Fatalf("events=%+v err=%v", page, err)
	}
}

func TestNestedOrchestrationReleasesSingleAdvanceSlot(t *testing.T) {
	s, parent, lease := reliabilityFixture(t, models.Steps{{ID: "first", Name: "Nested", Provider: "orchestrator", TaskType: "orchestration", TaskID: 41}})
	childDef := models.Orchestration{ID: 12, TenantID: 7, Name: "Child", Steps: models.Steps{{ID: "leaf", Name: "Leaf", Provider: "orchestrator", TaskType: "orchestration", TaskID: 42}}, EditorLayout: commonModels.JSONMap{}}
	if err := s.db.Create(&childDef).Error; err != nil {
		t.Fatal(err)
	}
	var childID string
	var posts atomic.Int32
	e := reliabilityProviderExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posts.Add(1)
			if strings.Contains(r.URL.Path, "/41/") {
				var payload struct {
					Parent string `json:"parent_execution_id"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				child, err := s.CreateExecutionWithContext(r.Context(), 12, 7, "manual", "orchestrator", &payload.Parent, ExecutionActor{})
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				childID = child.ExecutionID
				w.WriteHeader(202)
				fmt.Fprintf(w, `{"execution_id":%q}`, childID)
				return
			}
			w.WriteHeader(202)
			fmt.Fprint(w, `{"execution_id":"leaf-execution"}`)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/executions/")
		status := "success"
		if id != "leaf-execution" {
			child, err := s.GetExecutionByExecutionID(r.Context(), id, 7)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			status = child.Status
		}
		fmt.Fprintf(w, `{"execution_id":%q,"status":%q}`, id, status)
	}, "orchestrator", "orchestration")
	config := DefaultExecutionSupervisorConfig()
	config.Concurrency = 1
	config.BatchSize = 1
	supervisor, err := NewExecutionSupervisor(s, e, config)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.owner = lease.Owner
	for range 20 {
		supervisor.pass(t.Context())
	}
	stored, _ := s.GetExecution(t.Context(), uint(parent.ID), 7)
	child, err := s.GetExecutionByExecutionID(t.Context(), childID, 7)
	if err != nil || stored.Status != "success" || child.Status != "success" || posts.Load() != 2 {
		t.Fatalf("parent=%s child=%+v err=%v posts=%d", stored.Status, child, err, posts.Load())
	}
}

func TestSupervisorShutdownCancelsLocalWaitAndKeepsChildFact(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	waiting := make(chan struct{})
	requestStopped := make(chan struct{})
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(202)
			fmt.Fprint(w, `{"execution_id":"child"}`)
			return
		}
		close(waiting)
		<-r.Context().Done()
		close(requestStopped)
	})
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	config := DefaultExecutionSupervisorConfig()
	config.PollInterval = time.Millisecond
	supervisor, err := NewExecutionSupervisor(s, e, config)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.owner = lease.Owner
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("wait did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	select {
	case <-requestStopped:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP wait survived shutdown")
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	results, _ := readStepResults(stored.Metadata)
	if stored.Status != "failed" || stored.ErrorDetails["code"] != "orchestrator.execution.coordinator_stopped" || extractProviderExecutionID(results["first"].Result) != "child" {
		t.Fatalf("shutdown state=%+v", stored)
	}
}

func TestRenewalFailureStopsLocalAdvance(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	waiting := make(chan struct{})
	requestStopped := make(chan struct{})
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(202)
			fmt.Fprint(w, `{"execution_id":"child"}`)
			return
		}
		close(waiting)
		<-r.Context().Done()
		close(requestStopped)
	})
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Callback().Update().Before("gorm:update").Register("test:reject-renewal", func(tx *gorm.DB) {
		if fields, ok := tx.Statement.Dest.(map[string]interface{}); ok {
			if _, renewing := fields["lease_expires_at"]; renewing && len(fields) == 2 {
				tx.AddError(errors.New("renewal unavailable"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Callback().Update().Remove("test:reject-renewal") })
	config := DefaultExecutionSupervisorConfig()
	config.PollInterval = time.Millisecond
	config.HeartbeatInterval = 100 * time.Millisecond
	supervisor, err := NewExecutionSupervisor(s, e, config)
	if err != nil {
		t.Fatal(err)
	}
	supervisor.owner = lease.Owner
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { supervisor.Run(ctx, nil); close(done) }()
	select {
	case <-waiting:
	case <-time.After(3 * time.Second):
		t.Fatal("observation did not start")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("renewal failure did not stop supervisor")
	}
	select {
	case <-requestStopped:
	case <-time.After(3 * time.Second):
		t.Fatal("request outlived ownership failure")
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if stored.Status != "failed" || stored.ErrorDetails["code"] != "orchestrator.execution.coordinator_stopped" {
		t.Fatalf("renewal failure left active execution: %+v", stored)
	}
}

func TestLeaseDeadlineCancelsInFlightSubmissionWithoutReplay(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var posts atomic.Int32
	stopped := make(chan struct{})
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		<-r.Context().Done()
		close(stopped)
	})
	if err := s.db.Model(&execution.TaskExecution{}).Where("id=?", item.ID).Update("lease_expires_at", time.Now().UTC().Add(150*time.Millisecond)).Error; err != nil {
		t.Fatal(err)
	}
	supervisor, err := NewExecutionSupervisor(s, e, DefaultExecutionSupervisorConfig())
	if err != nil {
		t.Fatal(err)
	}
	supervisor.owner = lease.Owner
	supervisor.pass(t.Context())
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP submission outlived lease")
	}
	supervisor.pass(t.Context())
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if posts.Load() != 1 || stored.Status != "failed" || stored.ErrorDetails["code"] != "orchestrator.execution.dispatch_uncertain" {
		t.Fatalf("expiry posts=%d state=%+v", posts.Load(), stored)
	}
}

func TestProviderFailureIsDistinguishedFromInvalidInputAndSubmission(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var calls atomic.Int32
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	e.taskProviderResolver.loadProvider = func(context.Context, string) (*commonModels.TaskProvider, error) {
		return nil, ErrTaskProviderUnavailable
	}
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if calls.Load() != 0 || stored.ErrorDetails["code"] != "orchestrator.execution.provider_unavailable" || execution.FailureCategory(stored.ErrorDetails) != "owner_unavailable" {
		t.Fatalf("failure=%v calls=%d", stored.ErrorDetails, calls.Load())
	}
}

func TestDownstreamDenialHasSafePermissionCause(t *testing.T) {
	s, item, lease := reliabilityFixture(t, testSteps())
	var calls atomic.Int32
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"password=private"}`)
	})
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	stored, _ := s.GetExecution(t.Context(), uint(item.ID), 7)
	if calls.Load() != 1 || stored.ErrorDetails["code"] != "orchestrator.execution.dispatch_denied" || execution.FailureCategory(stored.ErrorDetails) != "permission_denied" || strings.Contains(stored.ErrorDetails["message"].(string), "private") {
		t.Fatalf("failure=%v calls=%d", stored.ErrorDetails, calls.Load())
	}
}

func TestFrozenTaskIdentityPreservesIntegerPrecisionAfterStorage(t *testing.T) {
	const taskID uint = 9007199254740993
	steps := testSteps()
	steps[0].TaskID = taskID
	s, _, lease := reliabilityFixture(t, steps)
	var path string
	e := reliabilityExecutor(t, s, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(202)
		fmt.Fprint(w, `{"execution_id":"child"}`)
	})
	if err := e.Advance(t.Context(), lease); err != nil {
		t.Fatal(err)
	}
	if path != "/tasks/check/9007199254740993/execute" {
		t.Fatalf("frozen reference changed: %s", path)
	}
}
