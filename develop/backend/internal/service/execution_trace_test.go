package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	execution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	"github.com/addp/common/httptrace"
	"github.com/addp/common/httptrace/tracetest"
	"github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryTraceAdmission(t *testing.T) {
	runQueryTraceAdmission(t, "")
}

func TestQueryTraceAdmissionAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	runQueryTraceAdmission(t, dsn)
}

func runQueryTraceAdmission(t *testing.T, dsn string) {
	for _, scenario := range []string{"query_enabled", "query_disabled", "workflow_excluded", "create_rejected"} {
		t.Run(scenario, func(t *testing.T) {
			var recorder *tracetest.Recorder
			if scenario == "query_disabled" {
				t.Setenv("ADDP_OBSERVABILITY_TRACES_ENABLED", "false")
			} else {
				recorder = tracetest.Start(t, "develop")
			}
			var db *gorm.DB
			var err error
			if dsn == "" {
				db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			} else {
				db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
			}
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			if dsn == "" {
				err = executiontest.EnsureSQLiteStore(db)
			} else {
				db = db.Begin()
				if db.Error != nil {
					t.Fatal(db.Error)
				}
				t.Cleanup(func() { _ = db.Rollback().Error })
				err = execution.EnsureStore(db)
			}
			if err != nil {
				t.Fatal(err)
			}
			kind := execution.TaskTypeQuery
			if scenario == "workflow_excluded" {
				kind = execution.TaskTypeWorkflow
			}
			fake := models.JSONMap{"schema": execution.TraceSchema, "enqueue": models.JSONMap{"trace_id": strings.Repeat("1", 32), "span_id": strings.Repeat("2", 16), "module_name": "develop", "role": "backend", "instance_id": "payload-identity", "started_at": time.Now().UTC().Format(time.RFC3339Nano)}}
			input := models.JSONMap{"observability": fake, "content": models.JSONMap{"query": "private-query-sentinel"}, "engine_id": 9}
			item := &execution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: kind, Source: "develop", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 1, ExecutionConfig: input, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if scenario == "create_rejected" {
				callback := "trace:admission-reject:" + uuid.NewString()
				if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
					if _, ok := tx.Statement.Dest.(*execution.TaskExecution); ok {
						tx.AddError(errors.New("enqueue persistence rejected"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			}
			repo := execution.NewTaskExecutionRepository(db)
			err = createDevelopExecution(t.Context(), repo, item)
			if scenario == "create_rejected" {
				if err == nil {
					t.Fatal("rejected owner create was persisted")
				}
				for _, span := range recorder.Finish() {
					if span.Name == "execution.enqueue" {
						for _, attr := range span.Attributes {
							if attr.Key == "addp.execution.outcome" && attr.Value.GetStringValue() != "rejected" {
								t.Fatal("failed enqueue commit was reported as success")
							}
						}
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repo.GetByExecutionID(t.Context(), item.ExecutionID, 7)
			if err != nil {
				t.Fatal(err)
			}
			ref, err := execution.EnqueueTrace(stored)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "query_enabled" {
				if ref == nil || ref.TraceID == strings.Repeat("1", 32) || ref.InstanceID == "payload-identity" {
					t.Fatal("enqueue accepted caller-supplied trace identity")
				}
			} else if ref != nil {
				t.Fatal("disabled or excluded task retained caller trace identity")
			}
			if input["observability"] == nil || stored.ExecutionConfig["content"] == nil {
				t.Fatal("namespace stripping mutated caller config or removed task snapshot")
			}
			if recorder != nil {
				spans := recorder.Finish()
				if scenario == "workflow_excluded" && len(spans) != 0 {
					t.Fatal("workflow implicitly received an execution span")
				}
				payload, _ := json.Marshal(spans)
				if bytes.Contains(payload, []byte("private-query-sentinel")) || bytes.Contains(payload, []byte(item.ExecutionID)) {
					t.Fatal("enqueue exported SQL or business execution identity")
				}
			}
		})
	}
	t.Run("checkpoint_storage_failure_does_not_block_query", func(t *testing.T) {
		testQueryTraceCheckpointFailure(t, dsn)
	})
}

type traceQueryProvider struct {
	t     *testing.T
	calls int
}

func (*traceQueryProvider) IsRuntime(context.Context, uint, uint) bool { return true }
func (*traceQueryProvider) ReferencedEngineIDs(context.Context, uint, uint, string) ([]uint, error) {
	return []uint{9}, nil
}
func (p *traceQueryProvider) ExecuteQuery(ctx context.Context, tenant, engine uint, id uuid.UUID, authorization int64, query string, timeout, limit int, sources []uint) (*FederatedQueryResult, error) {
	p.calls++
	if httptrace.CurrentTraceReference(ctx) != nil {
		p.t.Error("query inherited a live execution phase")
	}
	return &FederatedQueryResult{Columns: []string{"value"}, Rows: []map[string]interface{}{{"value": 1}}, RowCount: 1}, nil
}

// The actual QueryExecutionService entry, authorization rehydration, original
// progress fence, query provider route and terminal commit remain unchanged when
// only the optional checkpoint UPDATE fails.
func testQueryTraceCheckpointFailure(t *testing.T, dsn string) {
	recorder := tracetest.Start(t, "develop")
	var db *gorm.DB
	var err error
	if dsn == "" {
		db, err = gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	} else {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	}
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if dsn == "" {
		err = executiontest.EnsureSQLiteStore(db)
	} else {
		db = db.Begin()
		if db.Error != nil {
			t.Fatal(db.Error)
		}
		t.Cleanup(func() { _ = db.Rollback().Error })
		err = execution.EnsureStore(db)
	}
	if err != nil {
		t.Fatal(err)
	}
	principal, membership, version, authorization := int64(9), int64(19), int64(3), int64(71)
	expires := time.Now().UTC().Add(time.Minute)
	item := &execution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "query", Source: "develop", Status: "pending", ExecutionBoundary: "bounded", TriggerType: "manual", MaxAttempts: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, ExecutionAuthorizationID: &authorization, AuthorizationExpiresAt: &expires,
		ExecutionConfig: models.JSONMap{"engine_id": 9, "timeout": 30, "query_result_limit": 10, "display_language": "zh-cn", "content": models.JSONMap{"query_type": "sql", "query": "SELECT 1 AS trace_private_query_sentinel"}},
	}
	if err := createDevelopExecution(t.Context(), execution.NewTaskExecutionRepository(db), item); err != nil {
		t.Fatal(err)
	}
	queue := repository.NewExecutionRepository(db)
	claimed, lease, err := queue.ClaimNext("query", t.Context(), "trace-query-owner", time.Now().UTC(), time.Minute, nil)
	if err != nil || claimed == nil || lease == nil {
		t.Fatalf("query claim: %v", err)
	}
	checkpointFailures := 0
	callback := "trace:checkpoint-reject:" + uuid.NewString()
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		fields, ok := tx.Statement.Dest.(map[string]interface{})
		if !ok || len(fields) > 2 {
			return
		}
		if _, hasMetadata := fields["metadata"]; hasMetadata {
			checkpointFailures++
			tx.AddError(errors.New("optional trace checkpoint storage refused"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callback) })
	provider := &traceQueryProvider{t: t}
	executor := &DevExecutor{sqlEngine: &SQLEngineService{}, federatedQuery: provider, taskExecutionRepo: execution.NewTaskExecutionRepository(db)}
	service := &QueryExecutionService{executor: executor, queries: queue}
	if err := service.Execute(execution.ContextWithLease(t.Context(), *lease), claimed, *lease); err != nil {
		t.Fatalf("optional checkpoint prevented the existing query path: %v", err)
	}
	var stored execution.TaskExecution
	if err := db.First(&stored, item.ID).Error; err != nil || stored.Status != "success" || provider.calls != 1 || checkpointFailures != 1 {
		t.Fatalf("query did not finish normally: status=%s provider_calls=%d checkpoint_failures=%d err=%v", stored.Status, provider.calls, checkpointFailures, err)
	}
	rejected := false
	for _, span := range recorder.Finish() {
		if span.Name == "execution.dispatch" {
			for _, attr := range span.Attributes {
				if attr.Key == "addp.execution.outcome" && attr.Value.GetStringValue() == "rejected" {
					rejected = true
				}
			}
		}
		payload, _ := json.Marshal(span)
		if bytes.Contains(payload, []byte("trace_private_query_sentinel")) {
			t.Fatal("query SQL was exported as a span attribute")
		}
	}
	if !rejected {
		t.Fatal("uncommitted checkpoint was not reported as rejected")
	}
}
