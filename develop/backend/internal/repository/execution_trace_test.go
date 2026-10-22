package repository

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	execution "github.com/addp/common/execution"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/httptrace/tracetest"
	"github.com/addp/common/models"
	"github.com/google/uuid"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"gorm.io/gorm"
)

func TestQueryExecutionTraceTransactions(t *testing.T) {
	runQueryTraceCases(t, newQueryExecutionTestDB)
}

func seedQueryTrace(t *testing.T, db *gorm.DB) (*ExecutionRepository, *execution.TaskExecution) {
	t.Helper()
	repo, item, _ := seedExecutionEvent(t, db, execution.TaskTypeQuery)
	_, stage := httptrace.StartExecutionStage(t.Context(), httptrace.PhaseEnqueue, 0, nil, nil)
	defer stage.End(httptrace.OutcomeRejected)
	if err := execution.FreezeEnqueueTrace(item, stage.Reference()); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(item).Updates(map[string]interface{}{"execution_config": item.ExecutionConfig, "metadata": models.JSONMap{"business": "preserved", "result": "old-result", "outputs": models.JSONMap{"old": "artifact"}, "stale": "remove-me"}}).Error; err != nil {
		t.Fatal(err)
	}
	stage.End(httptrace.OutcomeSuccess)
	return repo, item
}

func runQueryTraceCases(t *testing.T, database func(*testing.T) *gorm.DB) {
	t.Run("query_trace_reload_dispatch_and_metadata", func(t *testing.T) {
		recorder := tracetest.Start(t, "develop")
		db := database(t)
		repo, item := seedQueryTrace(t, db)
		claimed, lease := claimExecutionEvent(t, repo, "query")
		claimRef, err := execution.LatestTrace(claimed)
		if err != nil || claimRef == nil || claimed.Attempt != 1 {
			t.Fatalf("committed query claim association: %v", err)
		}
		if err := NewExecutionRepository(db).RecordQueryDispatch(t.Context(), claimed, *lease); err != nil {
			t.Fatal(err)
		}
		var before execution.TaskExecution
		if err := db.First(&before, item.ID).Error; err != nil {
			t.Fatal(err)
		}
		dispatchRef, err := execution.LatestTrace(&before)
		if err != nil || dispatchRef == nil || dispatchRef.TraceID == claimRef.TraceID || before.Metadata["business"] != "preserved" || before.Metadata["result"] != "old-result" {
			t.Fatalf("dispatch association: %v", err)
		}
		if err := NewExecutionRepository(db).CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now(), map[string]interface{}{"metadata": models.JSONMap{"result": "safe-result"}}); err != nil {
			t.Fatal(err)
		}
		var stored execution.TaskExecution
		if err := db.First(&stored, item.ID).Error; err != nil || stored.Metadata["result"] != "safe-result" {
			t.Fatalf("terminal replacement result: %v", err)
		}
		for _, old := range []string{"business", "outputs", "stale"} {
			if _, present := stored.Metadata[old]; present {
				t.Fatal("terminal replacement retained old business metadata")
			}
		}
		terminalRef, err := execution.LatestTrace(&stored)
		if err != nil || terminalRef == nil || terminalRef.TraceID == dispatchRef.TraceID {
			t.Fatalf("terminal association: %v", err)
		}
		observation, _ := json.Marshal(execution.Observe(&stored))
		if bytes.Contains(observation, []byte("observability")) || bytes.Contains(observation, []byte(terminalRef.TraceID)) {
			t.Fatal("Tenant observation exposed private query trace facts")
		}
		phases := map[string]*tracepb.Span{}
		for _, span := range recorder.Finish() {
			phases[span.Name] = span
		}
		for phase, count := range map[string]int{"enqueue": 0, "claim": 1, "dispatch": 2, "terminal": 2} {
			span := phases["execution."+phase]
			if span == nil || len(span.Links) != count || querySpanOutcome(span) != "success" {
				t.Fatalf("query phase %s did not use persisted causal links", phase)
			}
		}
	})
	for _, mode := range []string{"omitted", "replace", "nil", "typed_nil", "plain_typed_nil", "empty", "nil_without_tracing", "invalid_string", "invalid_bytes"} {
		t.Run("query_trace_terminal_metadata_"+mode, func(t *testing.T) {
			var recorder *tracetest.Recorder
			if mode == "nil_without_tracing" {
				t.Setenv("ADDP_OBSERVABILITY_TRACES_ENABLED", "false")
			} else {
				recorder = tracetest.Start(t, "develop")
			}
			db := database(t)
			repo, item := seedQueryTrace(t, db)
			claimed, lease := claimExecutionEvent(t, repo, "query")
			prior, _ := execution.LatestTrace(claimed)
			fields := map[string]interface{}{}
			switch mode {
			case "replace":
				fields["metadata"] = models.JSONMap{"result": "new-result", execution.ObservabilityKey: models.JSONMap{"untrusted": "cannot-replace-authoritative-fact"}}
			case "nil", "nil_without_tracing":
				fields["metadata"] = nil
			case "typed_nil":
				fields["metadata"] = models.JSONMap(nil)
			case "plain_typed_nil":
				fields["metadata"] = map[string]interface{}(nil)
			case "empty":
				fields["metadata"] = models.JSONMap{}
			case "invalid_string":
				fields["metadata"] = `{"result":"not-an-object-value"}`
			case "invalid_bytes":
				fields["metadata"] = []byte(`{"result":"not-an-object-value"}`)
			}
			err := repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now().UTC(), fields)
			invalid := strings.HasPrefix(mode, "invalid_")
			if invalid {
				if err == nil || err.Error() != "Develop query completion metadata must be a JSON object or nil" {
					t.Fatalf("unsupported metadata did not fail the internal object contract: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var stored execution.TaskExecution
			if err := db.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			if invalid || mode == "omitted" {
				if stored.Metadata["business"] != "preserved" || stored.Metadata["result"] != "old-result" || stored.Metadata["outputs"] == nil || stored.Metadata["stale"] != "remove-me" {
					t.Fatal("omitted or rejected metadata changed existing business facts")
				}
			} else {
				for _, old := range []string{"business", "outputs", "stale"} {
					if _, present := stored.Metadata[old]; present {
						t.Fatal("explicit replacement or clearing retained obsolete business metadata")
					}
				}
				if mode == "replace" {
					if stored.Metadata["result"] != "new-result" {
						t.Fatal("terminal business replacement was lost")
					}
				} else if _, present := stored.Metadata["result"]; present {
					t.Fatal("explicit clearing retained old result")
				}
			}
			latest, refErr := execution.LatestTrace(&stored)
			if invalid {
				if stored.Status != "running" || prior == nil || latest == nil || *prior != *latest {
					t.Fatal("invalid metadata escaped the original terminal transaction")
				}
			} else if stored.Status != "success" {
				t.Fatal("valid replacement did not complete the execution")
			} else if mode == "nil_without_tracing" {
				if len(stored.Metadata) != 0 || latest != nil {
					t.Fatal("cleared metadata without tracing retained data or a private observation")
				}
			} else if refErr != nil || latest == nil || prior == nil || latest.TraceID == prior.TraceID {
				t.Fatal("business replacement lost the authoritative terminal observation")
			}
			if recorder != nil {
				terminalFound := false
				for _, span := range recorder.Finish() {
					if span.Name == "execution.terminal" {
						terminalFound = true
						want := "success"
						if invalid {
							want = "rejected"
						}
						if querySpanOutcome(span) != want {
							t.Fatal("terminal phase outcome did not match the transaction")
						}
					}
				}
				if !terminalFound {
					t.Fatal("terminal observation missing")
				}
			}
		})
	}
	for _, action := range []string{"claim", "terminal", "recovery"} {
		t.Run("query_trace_rollback_"+action, func(t *testing.T) {
			recorder := tracetest.Start(t, "develop")
			db := database(t)
			repo, item := seedQueryTrace(t, db)
			var claimed *execution.TaskExecution
			var lease *execution.Lease
			var prior *execution.TraceReference
			if action != "claim" {
				claimed, lease = claimExecutionEvent(t, repo, "query")
				prior, _ = execution.LatestTrace(claimed)
			}
			if action == "recovery" {
				result := db.Model(item).Update("lease_expires_at", time.Now().UTC().Add(-time.Minute))
				if result.Error != nil || result.RowsAffected != 1 {
					t.Fatalf("expire exact query fixture: rows=%d err=%v", result.RowsAffected, result.Error)
				}
				expired, err := execution.FindExpiredForUpdate(t.Context(), db, execution.ExpiredOptions{Module: "develop", TaskType: "query", Now: time.Now().UTC(), Limit: 1})
				if err != nil || len(expired) != 1 || expired[0].ExecutionID != item.ExecutionID {
					t.Fatalf("recovery fixture must be genuinely expired: count=%d err=%v", len(expired), err)
				}
			}
			callback := "trace:event-reject:" + uuid.NewString()
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*execution.Event); ok {
					tx.AddError(errors.New("event refused"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			var err error
			switch action {
			case "claim":
				_, _, err = repo.ClaimNext("query", t.Context(), "trace-owner", time.Now(), time.Minute, nil)
			case "terminal":
				err = repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now(), nil)
			case "recovery":
				_, err = repo.RecoverExpired(t.Context(), time.Now(), 1)
			}
			if err == nil {
				t.Fatal("failed event did not roll back query phase")
			}
			var stored execution.TaskExecution
			if err := db.First(&stored, item.ID).Error; err != nil {
				t.Fatal(err)
			}
			latest, _ := execution.LatestTrace(&stored)
			if action == "claim" && (stored.Status != "pending" || latest != nil) || action != "claim" && stored.Status != "running" {
				t.Fatal("query association escaped rejected transaction")
			}
			if action != "claim" && (prior == nil || latest == nil || *prior != *latest) {
				t.Fatal("rejected query phase replaced its prior persisted association")
			}
			want := "execution.terminal"
			if action == "claim" {
				want = "execution.claim"
			}
			found := false
			for _, span := range recorder.Finish() {
				if span.Name == want {
					found = true
					if querySpanOutcome(span) != "rejected" {
						t.Fatal("uncommitted query phase was marked successful")
					}
				}
			}
			if !found {
				t.Fatal("rejected query phase missing")
			}
		})
	}
	for _, malformed := range []string{"enqueue", "latest"} {
		t.Run("query_trace_malformed_"+malformed+"_disables_optional_stages", func(t *testing.T) {
			recorder := tracetest.Start(t, "develop")
			db := database(t)
			repo, item := seedQueryTrace(t, db)
			bad := models.JSONMap{"schema": execution.TraceSchema, malformed: "invalid-reference"}
			field := "execution_config"
			value := item.ExecutionConfig
			if malformed == "enqueue" {
				value[execution.ObservabilityKey] = bad
			} else {
				field = "metadata"
				value = models.JSONMap{execution.ObservabilityKey: bad, "business": "preserved"}
			}
			if err := db.Model(item).Update(field, value).Error; err != nil {
				t.Fatal(err)
			}
			claimed, lease := claimExecutionEvent(t, repo, "query")
			if err := repo.RecordQueryDispatch(t.Context(), claimed, *lease); err != nil {
				t.Fatalf("malformed optional reference blocked dispatch: %v", err)
			}
			if err := repo.CompleteWithLease(t.Context(), claimed, *lease, "success", time.Now().UTC(), map[string]interface{}{"metadata": models.JSONMap{"result": "completed"}}); err != nil {
				t.Fatalf("malformed optional reference blocked completion: %v", err)
			}
			var stored execution.TaskExecution
			if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "success" || stored.Metadata["result"] != "completed" {
				t.Fatalf("business execution changed by malformed observation: %v", err)
			}
			for _, old := range []string{"business", "outputs", "stale"} {
				if _, present := stored.Metadata[old]; present {
					t.Fatal("malformed private namespace prevented the original business replacement")
				}
			}
			preserved := stored.ExecutionConfig[execution.ObservabilityKey]
			if malformed == "latest" {
				preserved = stored.Metadata[execution.ObservabilityKey]
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
	t.Run("other_execution_types_have_no_async_trace", func(t *testing.T) {
		recorder := tracetest.Start(t, "develop")
		db := database(t)
		for _, kind := range []string{"workflow", "script"} {
			repo, _, _ := seedExecutionEvent(t, db, kind)
			item, lease := claimExecutionEvent(t, repo, kind)
			if err := repo.CompleteWithLease(t.Context(), item, *lease, "success", time.Now(), nil); err != nil {
				t.Fatal(err)
			}
		}
		if len(recorder.Finish()) != 0 {
			t.Fatal("unapproved Runtime execution received async spans")
		}
	})
}

func querySpanOutcome(span *tracepb.Span) string {
	for _, value := range span.Attributes {
		if value.Key == "addp.execution.outcome" {
			return value.Value.GetStringValue()
		}
	}
	return ""
}
