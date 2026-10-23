package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	execution "github.com/addp/common/execution"
	"github.com/addp/common/httptrace/tracetest"
	"github.com/addp/common/models"
	orchmodels "github.com/addp/orchestrator/internal/models"
	"github.com/google/uuid"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"gorm.io/gorm"
)

func TestOrchestrationExecutionTraceTransactions(t *testing.T) {
	runOrchestrationTraceCases(t, newOrchestratorExecutionServiceTestDB)
}

func TestIntegrationOrchestrationExecutionTraceTransactions(t *testing.T) {
	runOrchestrationTraceCases(t, postgresReliabilityDB)
}

func traceDefinition(t *testing.T, db *gorm.DB) *orchmodels.Orchestration {
	t.Helper()
	definition := &orchmodels.Orchestration{TenantID: 999999, Name: "trace-" + uuid.NewString(), Steps: orchmodels.Steps{{ID: "first", Name: "Query", Provider: "develop", TaskType: "query", TaskID: 41}}, EditorLayout: models.JSONMap{}}
	if err := db.Create(definition).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		var ids []string
		if err := db.Model(&execution.TaskExecution{}).Where("module = ? AND tenant_id = ? AND source_task_id = ?", "orchestrator", definition.TenantID, execution.NewSourceTaskIDFromUint(definition.ID)).Pluck("execution_id", &ids).Error; err != nil {
			t.Error(err)
		}
		if len(ids) > 0 {
			if err := db.Where("execution_id IN ?", ids).Delete(&execution.Event{}).Error; err != nil {
				t.Error(err)
			}
			if err := db.Where("execution_id IN ?", ids).Delete(&execution.TaskExecution{}).Error; err != nil {
				t.Error(err)
			}
		}
		if err := db.Unscoped().Delete(definition).Error; err != nil {
			t.Error(err)
		}
	})
	return definition
}

func runOrchestrationTraceCases(t *testing.T, database func(*testing.T) *gorm.DB) {
	t.Run("reload_dispatch_and_terminal_preserve_business_metadata", func(t *testing.T) {
		recorder := tracetest.Start(t, "orchestrator")
		checkpoints := tracetest.CaptureCheckpoints(t)
		db := database(t)
		definition := traceDefinition(t, db)
		service := NewExecutionService(db)
		item, err := service.CreateExecutionWithContext(t.Context(), definition.ID, definition.TenantID, "manual", "orchestrator", nil, ExecutionActor{9, 19, 3})
		if err != nil {
			t.Fatal(err)
		}
		enqueue, err := execution.EnqueueTrace(item)
		if err != nil || enqueue == nil {
			t.Fatalf("enqueue association: %v", err)
		}
		if err := db.Model(item).Update("metadata", models.JSONMap{"business": "retained"}).Error; err != nil {
			t.Fatal(err)
		}
		claimed, lease, err := NewExecutionService(db).ClaimNext(t.Context(), "trace-owner", time.Minute)
		if err != nil || claimed == nil || lease == nil || claimed.ExecutionID != item.ExecutionID {
			t.Fatalf("claim: %v", err)
		}
		claim, err := execution.LatestTrace(claimed)
		if err != nil || claim == nil || claim.TraceID == enqueue.TraceID {
			t.Fatalf("claim association: %v", err)
		}
		if _, err := readExecutionPlan(claimed.ExecutionConfig); err != nil {
			t.Fatalf("Common trace namespace invalidated frozen plan: %v", err)
		}
		var propagation string
		childID := uuid.NewString()
		executor := reliabilityProviderExecutor(t, NewExecutionService(db), func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				propagation = r.Header.Get("traceparent")
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprintf(w, `{"execution_id":%q}`, childID)
				return
			}
			fmt.Fprintf(w, `{"execution_id":%q,"status":"success"}`, childID)
		}, "develop", "query")
		if err := executor.Advance(t.Context(), *lease); err != nil {
			t.Fatal(err)
		}
		if len(propagation) != 55 {
			t.Fatal("bounded query submission did not propagate its real HTTP trace")
		}
		stored, err := service.GetExecution(t.Context(), uint(item.ID), definition.TenantID)
		if err != nil {
			t.Fatal(err)
		}
		dispatch, err := execution.LatestTrace(stored)
		if err != nil || dispatch == nil || dispatch.TraceID == claim.TraceID {
			t.Fatalf("dispatch association: %v", err)
		}
		for range 2 {
			if err := executor.Advance(t.Context(), *lease); err != nil {
				t.Fatal(err)
			}
		}
		stored, err = service.GetExecution(t.Context(), uint(item.ID), definition.TenantID)
		if err != nil || stored.Status != "success" || stored.Metadata["business"] != "retained" || stored.Metadata["step_results"] == nil {
			t.Fatalf("terminal lost durable state: %v", err)
		}
		frozen, _ := execution.EnqueueTrace(stored)
		if frozen == nil || *frozen != *enqueue {
			t.Fatal("enqueue association changed after reload")
		}
		payload, _ := json.Marshal(execution.Observe(stored))
		if bytes.Contains(payload, []byte("observability")) || bytes.Contains(payload, []byte(enqueue.TraceID)) {
			t.Fatal("Tenant observation leaked private trace association")
		}
		phases := map[string]*tracepb.Span{}
		for _, span := range recorder.Finish() {
			if strings.HasPrefix(span.Name, "execution.") {
				phases[span.Name] = span
				if len(span.ParentSpanId) != 0 {
					t.Fatal("execution phase held a parent instead of a Link")
				}
			}
		}
		for phase, links := range map[string]int{"enqueue": 0, "claim": 1, "dispatch": 2, "terminal": 2} {
			span := phases["execution."+phase]
			if span == nil || len(span.Links) != links || spanOutcome(span) != "success" {
				t.Fatalf("phase %s missing or not committed: %#v", phase, span)
			}
		}
		if hex.EncodeToString(phases["execution.terminal"].Links[1].TraceId) != dispatch.TraceID {
			t.Fatal("terminal did not link the persisted latest dispatch after reload")
		}
		logs := checkpoints()
		if len(logs) != 4 {
			t.Fatalf("owner checkpoint count=%d, want four real stages", len(logs))
		}
		seen := map[string]bool{}
		for _, log := range logs {
			phase, _ := log["phase"].(string)
			span := phases["execution."+phase]
			if seen[phase] || span == nil || log["execution_id"] != item.ExecutionID || log["outcome"] != "success" || log["trace_id"] != hex.EncodeToString(span.TraceId) || log["span_id"] != hex.EncodeToString(span.SpanId) {
				t.Fatal("owner checkpoint did not bind its true phase and execution")
			}
			seen[phase] = true
		}
	})
	t.Run("accepted_dispatch_checkpoint_rejected_is_not_task_failure", func(t *testing.T) {
		recorder := tracetest.Start(t, "orchestrator")
		db := database(t)
		definition := traceDefinition(t, db)
		service := NewExecutionService(db)
		item, err := service.CreateExecutionWithContext(t.Context(), definition.ID, definition.TenantID, "manual", "orchestrator", nil, ExecutionActor{9, 19, 3})
		if err != nil {
			t.Fatal(err)
		}
		_, lease, err := service.ClaimNext(t.Context(), "trace-owner", time.Minute)
		if err != nil || lease == nil {
			t.Fatalf("claim: %v", err)
		}
		posts, refused := 0, 0
		childID := uuid.NewString()
		executor := reliabilityProviderExecutor(t, service, func(w http.ResponseWriter, r *http.Request) {
			posts++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprintf(w, `{"execution_id":%q}`, childID)
		}, "develop", "query")
		callback := "trace:waiting-checkpoint-reject:" + uuid.NewString()
		if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			fields, ok := tx.Statement.Dest.(map[string]interface{})
			if !ok {
				return
			}
			metadata, ok := fields["metadata"].(models.JSONMap)
			if !ok {
				return
			}
			steps, _ := json.Marshal(metadata["step_results"])
			if bytes.Contains(steps, []byte(`"phase":"waiting"`)) {
				refused++
				tx.AddError(errors.New("waiting checkpoint refused"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
		if err := executor.Advance(t.Context(), *lease); err == nil {
			t.Fatal("accepted submission hid its uncommitted waiting checkpoint")
		}
		stored, err := service.GetExecution(t.Context(), uint(item.ID), definition.TenantID)
		if err != nil {
			t.Fatal(err)
		}
		results, _ := readStepResults(stored.Metadata)
		if posts != 1 || refused != 1 || stored.Status != "running" || results["first"].Phase != "dispatching" || extractProviderExecutionID(results["first"].Result) != "" {
			t.Fatal("uncommitted waiting facts escaped the original transaction")
		}
		// Existing fail-closed behavior must remain: an uncertain accepted request
		// is never replayed on the next advance.
		if err := executor.Advance(t.Context(), *lease); err != nil {
			t.Fatal(err)
		}
		if posts != 1 {
			t.Fatal("accepted dispatch was replayed after checkpoint rejection")
		}
		found := false
		for _, span := range recorder.Finish() {
			if span.Name == "execution.dispatch" {
				found = true
				if spanOutcome(span) != "rejected" || span.Status.GetCode() == tracepb.Status_STATUS_CODE_ERROR {
					t.Fatal("checkpoint rejection was classified as a task failure")
				}
			}
		}
		if !found {
			t.Fatal("accepted dispatch phase missing")
		}
	})
	for _, malformed := range []string{"enqueue", "latest"} {
		t.Run("malformed_"+malformed+"_disables_optional_stages", func(t *testing.T) {
			recorder := tracetest.Start(t, "orchestrator")
			db := database(t)
			definition := traceDefinition(t, db)
			service := NewExecutionService(db)
			item, err := service.CreateExecutionWithContext(t.Context(), definition.ID, definition.TenantID, "manual", "orchestrator", nil, ExecutionActor{9, 19, 3})
			if err != nil {
				t.Fatal(err)
			}
			bad := models.JSONMap{"schema": execution.TraceSchema, malformed: "invalid-reference"}
			field := "execution_config"
			if malformed == "enqueue" {
				item.ExecutionConfig[execution.ObservabilityKey] = bad
			} else {
				field = "metadata"
				item.Metadata = models.JSONMap{execution.ObservabilityKey: bad, "business": "retained"}
			}
			value := item.ExecutionConfig
			if field == "metadata" {
				value = item.Metadata
			}
			if err := db.Model(item).Update(field, value).Error; err != nil {
				t.Fatal(err)
			}
			_, lease, err := service.ClaimNext(t.Context(), "trace-owner", time.Minute)
			if err != nil || lease == nil {
				t.Fatalf("malformed optional reference blocked claim: %v", err)
			}
			childID := uuid.NewString()
			executor := reliabilityProviderExecutor(t, service, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"execution_id":%q,"status":"success"}`, childID)
			}, "develop", "query")
			for range 3 {
				if err := executor.Advance(t.Context(), *lease); err != nil {
					t.Fatalf("malformed optional reference blocked execution: %v", err)
				}
			}
			stored, err := service.GetExecution(t.Context(), uint(item.ID), definition.TenantID)
			if err != nil || stored.Status != "success" {
				t.Fatalf("business execution did not complete: %v", err)
			}
			preserved := stored.ExecutionConfig[execution.ObservabilityKey]
			if malformed == "latest" {
				preserved = stored.Metadata[execution.ObservabilityKey]
				if stored.Metadata["business"] != "retained" {
					t.Fatal("malformed observation removed unrelated owner metadata")
				}
			}
			want, _ := json.Marshal(bad)
			got, _ := json.Marshal(preserved)
			if !bytes.Equal(want, got) {
				t.Fatal("malformed authoritative namespace was overwritten")
			}
			for _, span := range recorder.Finish() {
				if strings.HasPrefix(span.Name, "execution.") && span.Name != "execution.enqueue" {
					t.Fatal("malformed reference was treated as missing and generated a new phase")
				}
			}
		})
	}
	for _, operation := range []string{"claim", "terminal", "step_failure"} {
		t.Run("rollback_"+operation, func(t *testing.T) {
			recorder := tracetest.Start(t, "orchestrator")
			checkpoints := tracetest.CaptureCheckpoints(t)
			db := database(t)
			definition := traceDefinition(t, db)
			service := NewExecutionService(db)
			item, err := service.CreateExecutionWithContext(t.Context(), definition.ID, definition.TenantID, "manual", "orchestrator", nil, ExecutionActor{9, 19, 3})
			if err != nil {
				t.Fatal(err)
			}
			var lease *execution.Lease
			if operation != "claim" {
				_, lease, err = service.ClaimNext(t.Context(), "trace-owner", time.Minute)
				if err != nil || lease == nil {
					t.Fatal(err)
				}
			}
			callback := "trace:event-failure:" + uuid.NewString()
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*execution.Event); ok {
					tx.AddError(errors.New("event persistence refused"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			switch operation {
			case "claim":
				_, _, err = service.ClaimNext(t.Context(), "trace-owner", time.Minute)
			case "terminal":
				err = service.FinishExecution(t.Context(), *lease, "success", "")
			case "step_failure":
				err = (&Executor{executionService: service}).failStep(t.Context(), *lease, "first", orchmodels.StepResults{}, orchmodels.StepResult{StartedAt: time.Now()}, "orchestrator.execution.child_failed")
			}
			if err == nil {
				t.Fatal("injected event failure did not roll back phase")
			}
			stored, err := service.GetExecution(context.Background(), uint(item.ID), definition.TenantID)
			if err != nil {
				t.Fatal(err)
			}
			latest, _ := execution.LatestTrace(stored)
			if operation == "claim" && (stored.Status != "pending" || stored.Attempt != 0 || latest != nil) || operation != "claim" && stored.Status != "running" {
				t.Fatal("phase association escaped owner transaction rollback")
			}
			wanted := "execution.terminal"
			if operation == "claim" {
				wanted = "execution.claim"
			}
			found := false
			for _, span := range recorder.Finish() {
				if span.Name == wanted {
					found = true
					if spanOutcome(span) != "rejected" {
						t.Fatal("uncommitted owner phase was reported as success")
					}
				}
			}
			if !found {
				t.Fatal("rejected transaction phase missing")
			}
			count := 0
			for _, log := range checkpoints() {
				if "execution."+log["phase"].(string) == wanted {
					count++
					if log["execution_id"] != item.ExecutionID || log["outcome"] != "rejected" {
						t.Fatal("uncommitted transaction emitted a success checkpoint")
					}
				}
			}
			if count != 1 {
				t.Fatal("rejected transaction checkpoint missing or duplicated")
			}
		})
	}
}

func spanOutcome(span *tracepb.Span) string {
	for _, value := range span.Attributes {
		if value.Key == "addp.execution.outcome" {
			return value.Value.GetStringValue()
		}
	}
	return ""
}
