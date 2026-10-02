package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	execution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"github.com/google/uuid"
)

const processFixtureTenant = 999998

// This test-binary entry runs the production supervisor in an independent process.
// It is only invoked by the PostgreSQL gate's parent test, never by a dev launcher.
func TestIntegrationPostgresSupervisorProcess(t *testing.T) {
	if os.Getenv("ADDP_ORCH_TEST_PROCESS") != "1" {
		return
	}
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Fatal("supervisor subprocess requires the PostgreSQL gate")
	}
	id := os.Getenv("ADDP_ORCH_TEST_EXECUTION")
	if _, err := uuid.Parse(id); err != nil {
		t.Fatal("invalid process fixture execution")
	}
	providerURL := os.Getenv("ADDP_ORCH_TEST_PROVIDER")
	parsed, err := url.Parse(providerURL)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		t.Fatal("invalid process fixture provider")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip == nil || !ip.IsLoopback() || parsed.Port() == "" {
		t.Fatal("process fixture provider must use a loopback port")
	}
	s := NewExecutionService(postgresReliabilityDB(t))
	item, err := s.GetExecutionByExecutionID(t.Context(), id, processFixtureTenant)
	if err != nil || item.SourceTaskName == nil || !strings.HasPrefix(*item.SourceTaskName, "process-gate-") {
		t.Fatal("process does not own the execution fixture")
	}
	e := &Executor{executionService: s,
		taskProviderResolver: taskProviderResolverWithProvider(&commonModels.TaskProvider{
			ModuleName: "quality", Backends: taskProviderBackendsForTest(providerURL), Available: true,
			TaskProviderDeclaration: commonModels.TaskProviderDeclaration{
				TaskExecuteEndpoint: "/tasks/{task_type}/{id}/execute", TaskStatusEndpoint: "/executions/{execution_id}",
				Capabilities: jsonStringPtr(taskCapabilitiesForTest("check", false, `{"type":"object","additionalProperties":false}`)),
			}}, `{"type":"object","additionalProperties":false}`),
		serviceTokens: commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "fixture-token", nil }),
	}
	config := DefaultExecutionSupervisorConfig()
	config.Concurrency, config.BatchSize = 1, 1
	config.LeaseDuration, config.HeartbeatInterval, config.PollInterval = 2*time.Second, 100*time.Millisecond, 20*time.Millisecond
	supervisor, err := NewExecutionSupervisor(s, e, config)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				row, err := s.GetExecutionByExecutionID(ctx, id, processFixtureTenant)
				if err == nil && row.Status == "failed" {
					cancel()
					return
				}
			}
		}
	}()
	supervisor.Run(ctx, nil)
	cancel()
	<-watchDone
	stored, err := s.GetExecutionByExecutionID(t.Context(), id, processFixtureTenant)
	if err != nil || stored.Status != "failed" {
		t.Fatal("replacement supervisor did not converge the fixture")
	}
}

type supervisorTestProcess struct {
	command *exec.Cmd
	done    chan struct{}
	output  bytes.Buffer
	err     error
}

func startSupervisorTestProcess(t *testing.T, executionID, providerURL string) *supervisorTestProcess {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process := &supervisorTestProcess{done: make(chan struct{})}
	process.command = exec.Command(binary, "-test.run=^TestIntegrationPostgresSupervisorProcess$", "-test.count=1", "-test.timeout=15s")
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(variable, "ADDP_ORCH_TEST_") {
			process.command.Env = append(process.command.Env, variable)
		}
	}
	process.command.Env = append(process.command.Env, "ADDP_ORCH_TEST_PROCESS=1", "ADDP_ORCH_TEST_EXECUTION="+executionID, "ADDP_ORCH_TEST_PROVIDER="+providerURL)
	process.command.Stdout, process.command.Stderr = &process.output, &process.output
	if err := process.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { process.err = process.command.Wait(); close(process.done) }()
	t.Cleanup(func() {
		select {
		case <-process.done:
			return
		default:
		}
		_ = process.command.Process.Kill()
		select {
		case <-process.done:
		case <-time.After(5 * time.Second):
			t.Error("owned supervisor test process survived cleanup")
		}
	})
	return process
}

func TestIntegrationPostgresSupervisorAbruptExit(t *testing.T) {
	db := postgresReliabilityDB(t)
	s := NewExecutionService(db)
	for _, phase := range []string{"dispatching", "waiting"} {
		t.Run(phase, func(t *testing.T) {
			var active int64
			if err := db.Model(&execution.TaskExecution{}).Where("module=? AND status IN ?", "orchestrator", []string{"pending", "running"}).Count(&active).Error; err != nil || active != 0 {
				t.Fatal("process gate requires an empty Orchestrator test queue")
			}
			steps := append(testSteps(), models.Step{ID: "second", Name: "Second", Provider: "quality", TaskType: "check", TaskID: 42, DependsOn: []string{"first"}})
			definition := models.Orchestration{TenantID: processFixtureTenant, Name: "process-gate-" + uuid.NewString(), Steps: steps, EditorLayout: commonModels.JSONMap{}}
			if err := db.Create(&definition).Error; err != nil {
				t.Fatal(err)
			}
			var fixtureID string
			t.Cleanup(func() {
				if fixtureID != "" {
					for _, model := range []interface{}{&execution.Event{}, &execution.TaskExecution{}} {
						if err := db.Where("execution_id=? AND tenant_id=?", fixtureID, processFixtureTenant).Delete(model).Error; err != nil {
							t.Error(err)
						}
						var remaining int64
						if err := db.Model(model).Where("execution_id=? AND tenant_id=?", fixtureID, processFixtureTenant).Count(&remaining).Error; err != nil || remaining != 0 {
							t.Errorf("process fixture cleanup remaining=%d err=%v", remaining, err)
						}
					}
				}
				if err := db.Unscoped().Delete(&definition).Error; err != nil {
					t.Error(err)
				}
				var remaining int64
				if err := db.Unscoped().Model(&models.Orchestration{}).Where("id=?", definition.ID).Count(&remaining).Error; err != nil || remaining != 0 {
					t.Errorf("definition cleanup remaining=%d err=%v", remaining, err)
				}
			})
			item, err := s.CreateExecutionWithContext(t.Context(), definition.ID, definition.TenantID, "manual", "orchestrator", nil, ExecutionActor{9, 19, 3})
			if err != nil {
				t.Fatal(err)
			}
			fixtureID = item.ExecutionID
			completedChildID, childID := uuid.NewString(), uuid.NewString()
			var posts atomic.Int32
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					body, err := io.ReadAll(r.Body)
					var request map[string]interface{}
					if err != nil || json.Unmarshal(body, &request) != nil || request["parent_execution_id"] != fixtureID || request["source"] != "orchestrator" {
						w.WriteHeader(400)
						return
					}
					posts.Add(1) // The downstream has accepted, even when the response is lost.
					if r.URL.Path == "/tasks/check/41/execute" {
						w.WriteHeader(202)
						fmt.Fprintf(w, `{"execution_id":%q}`, completedChildID)
						return
					}
					if r.URL.Path != "/tasks/check/42/execute" {
						w.WriteHeader(400)
						return
					}
					if phase == "dispatching" {
						select {
						case <-r.Context().Done():
						case <-release:
						}
						return
					}
					w.WriteHeader(202)
					fmt.Fprintf(w, `{"execution_id":%q}`, childID)
					return
				}
				if r.URL.Path == "/executions/"+completedChildID {
					fmt.Fprintf(w, `{"execution_id":%q,"status":"success"}`, completedChildID)
					return
				}
				fmt.Fprintf(w, `{"execution_id":%q,"status":"running"}`, childID)
			}))
			t.Cleanup(func() { close(release); server.Close() })
			original := startSupervisorTestProcess(t, fixtureID, server.URL)
			var owned *execution.TaskExecution
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case <-original.done:
					t.Fatalf("original supervisor exited before injection: %v\n%s", original.err, original.output.String())
				default:
				}
				row, err := s.GetExecutionByExecutionID(t.Context(), fixtureID, processFixtureTenant)
				if err != nil {
					t.Fatal(err)
				}
				results, err := readStepResults(row.Metadata)
				// Require a real heartbeat before interruption, not just the initial claim.
				if err == nil && posts.Load() == 2 && results["first"].Status == "success" && results["second"].Phase == phase && row.StartedAt != nil && row.LeaseExpiresAt != nil && row.LeaseExpiresAt.After(row.StartedAt.Add(2200*time.Millisecond)) {
					owned = row
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if owned == nil {
				t.Fatal("supervisor did not persist the requested phase and renew its lease")
			}
			stale, err := execution.LeaseFromExecution(*owned)
			if err != nil {
				t.Fatal(err)
			}
			if err := original.command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-original.done:
				var exitError *exec.ExitError
				if !errors.As(original.err, &exitError) || exitError.ExitCode() != -1 {
					t.Fatal("original supervisor was not terminated by a signal")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("original process did not exit")
			}
			replacement := startSupervisorTestProcess(t, fixtureID, server.URL)
			select {
			case <-replacement.done:
				if replacement.err != nil {
					t.Fatalf("replacement supervisor failed: %v\n%s", replacement.err, replacement.output.String())
				}
			case <-time.After(12 * time.Second):
				t.Fatal("replacement supervisor did not exit after convergence")
			}
			stored, err := s.GetExecutionByExecutionID(t.Context(), fixtureID, processFixtureTenant)
			if err != nil {
				t.Fatal(err)
			}
			code := "orchestrator.execution.lease_expired"
			if phase == "dispatching" {
				code = "orchestrator.execution.dispatch_uncertain"
			}
			results, err := readStepResults(stored.Metadata)
			if err != nil || stored.Status != "failed" || stored.ErrorDetails["code"] != code || stored.Progress != 50 || stored.Attempt != 1 || results["first"].Status != "success" || results["second"].Phase != phase || posts.Load() != 2 {
				t.Fatalf("incorrect abrupt-exit convergence: status=%s code=%v progress=%d attempt=%d phase=%s posts=%d", stored.Status, stored.ErrorDetails["code"], stored.Progress, stored.Attempt, results["second"].Phase, posts.Load())
			}
			if extractProviderExecutionID(results["first"].Result) != completedChildID {
				t.Fatal("recovery lost the completed step's child execution")
			}
			if phase == "waiting" && extractProviderExecutionID(results["second"].Result) != childID {
				t.Fatal("recovery lost the known child execution")
			}
			if phase == "dispatching" && extractProviderExecutionID(results["second"].Result) != "" {
				t.Fatal("recovery invented an unknown child identity")
			}
			if err := s.UpdateStepResults(t.Context(), stale, models.StepResults{}, "first"); !errors.Is(err, commonapi.ErrConflict) {
				t.Fatalf("stale process can still write: %v", err)
			}
			if count, err := s.RecoverExpired(t.Context(), time.Now().UTC(), 100); err != nil || count != 0 {
				t.Fatalf("duplicate recovery: count=%d err=%v", count, err)
			}
			var events []execution.Event
			if err := db.Where("execution_id=?", fixtureID).Order("id").Find(&events).Error; err != nil {
				t.Fatal(err)
			}
			terminalCount := 0
			for _, event := range events {
				switch event.Kind {
				case "completed", "failed", "timeout", "cancelled":
					terminalCount++
				}
			}
			if len(events) < 3 || events[0].Kind != "started" || events[len(events)-1].Kind != "failed" || terminalCount != 1 {
				t.Fatal("abrupt exit did not produce exactly one terminal failure event")
			}
		})
	}
}
