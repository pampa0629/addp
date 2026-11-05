package service

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/protection"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestQueryResultPersistence(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	assertQueryResultPersistence(t, db)
	assertQueryResultSaveFailure(t, db, false)
}
func TestQueryResultPersistenceAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	if err := commonExecution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	assertQueryResultPersistence(t, tx)
	assertQueryResultSaveFailure(t, tx, true)
}
func assertQueryResultPersistence(t *testing.T, db *gorm.DB) {
	for _, sanitized := range []bool{false, true} {
		for _, stale := range []bool{false, true} {
			now := time.Now().UTC()
			token, owner, expires := uuid.NewString(), "result-supervisor", now.Add(time.Minute)
			principal, membership, version := int64(9), int64(19), int64(1)
			execution := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "query", Source: "develop", Status: "running", TriggerType: "manual", Attempt: 1, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, LeaseToken: &token, LeaseOwner: &owner, LeaseExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
			if err := db.Create(execution).Error; err != nil {
				t.Fatal(err)
			}
			lease := commonExecution.Lease{ExecutionID: execution.ExecutionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner}
			if stale {
				lease.Token = uuid.NewString()
			}
			cache := newQueryResultCache()
			gate := &resultReadGate{}
			if sanitized {
				gate.retained = authorizedResultFixture()
				gate.retained.Result.Rows[0]["phone"] = "138****5678"
			}
			executor := &DevExecutor{queryResults: cache, sqlEngine: &SQLEngineService{protectionGate: gate}}
			svc := &QueryExecutionService{executor: executor, queries: repository.NewExecutionRepository(db)}
			raw := authorizedResultFixture()
			preview, _, encodeErr := executor.queryResult(1, []string{"phone", "id", "small", "amount", "payload", "shape_alias"}, []datatype.FieldInfo{
				{Name: "phone", Type: datatype.FieldTypeString}, {Name: "id", Type: datatype.FieldTypeBigInt}, {Name: "small", Type: datatype.FieldTypeInt}, {Name: "amount", Type: datatype.FieldTypeDecimal, Precision: 38, Scale: 18}, {Name: "payload", Type: datatype.FieldTypeBytes}, {Name: "shape_alias", Type: datatype.FieldTypeGeometry},
			}, []map[string]interface{}{{"phone": "138****5678", "id": int64(9007199254740993), "small": int64(2), "amount": "12345678901234567890.123456789012345678", "payload": []byte{0, 255, 128}, "shape_alias": persistedQueryGeometry()}}, 1, SQLExecutionEffectRead, "table", nil)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			err := svc.completeQueryResult(t.Context(), execution, lease, preview, nil, &protection.QueryCapture{Subject: raw.Subject, Result: raw}, 1)
			if (err != nil) != stale {
				t.Fatal("completion did not enforce lease")
			}
			var stored commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			encoded := string(mustJSON(t, &stored))
			if strings.Contains(encoded, "13812345678") || strings.Contains(encoded, "protected_query_history") || strings.Contains(encoded, "authorized_query_result") {
				t.Fatal("raw query result persisted")
			}
			cachedValue, cached := cache.get(execution.ExecutionID, 1, 7, dataprotection.SubjectReference{Type: "user", ID: "9"})
			if cached && sanitized && cachedValue.value.Result.Rows[0]["phone"] != "138****5678" {
				t.Fatal("initial cache restored a raw value removed by the final protection recheck")
			}
			if cached == stale {
				t.Fatal("result delivery not fenced by committed execution")
			}
			if !stale && (!strings.Contains(encoded, `"id":"9007199254740993"`) || !strings.Contains(encoded, `"small":2`) || !strings.Contains(encoded, `"amount":"12345678901234567890.123456789012345678"`)) {
				t.Fatal("persisted query DTO lost precision")
			}
			if !stale && !strings.Contains(encoded, "138****5678") {
				t.Fatal("default preview missing from history")
			}
			if !stale {
				metadata, _ := mapValue(stored.Metadata["result"])
				summary, _ := mapValue(metadata["summary"])
				rows, _ := summary["preview_rows"].([]interface{})
				if len(rows) != 1 || rows[0].(map[string]interface{})["payload"] != base64.StdEncoding.EncodeToString([]byte{0, 255, 128}) {
					t.Fatal("binary result did not survive persistence")
				}
				geometry, ok := rows[0].(map[string]interface{})["shape_alias"].(map[string]interface{})
				if !ok || geometry["wkt"] != "POINT (1 2)" || geometry["srid"] != float64(4326) {
					t.Fatalf("geometry result did not survive persistence: %+v", geometry)
				}

			}
			cache.remove(execution.ExecutionID, lease.Token)
		}
	}
}

// PostgreSQL exercises the original JSONB NUL rejection. SQLite injects the
// same completion failure without requiring an external service for Go T1.
func assertQueryResultSaveFailure(t *testing.T, db *gorm.DB, postgres bool) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprintf("save_failure_stale_%t", stale), func(t *testing.T) {
			now := time.Now().UTC()
			token, owner, expires := uuid.NewString(), "result-worker", now.Add(time.Minute)
			principal, membership, version := int64(9), int64(19), int64(1)
			execution := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "query", Source: "develop", Status: "running", TriggerType: "manual", Attempt: 1, ActorPrincipalID: &principal, ActorTenantMembershipID: &membership, IssuedAuthorizationVersion: &version, LeaseToken: &token, LeaseOwner: &owner, LeaseExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
			if err := db.Create(execution).Error; err != nil {
				t.Fatal(err)
			}
			lease := commonExecution.Lease{ExecutionID: execution.ExecutionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner}
			if stale {
				lease.Token = uuid.NewString()
			}
			if !postgres {
				const callback = "test:result_save_failure"
				if err := db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					fields, ok := tx.Statement.Dest.(map[string]interface{})
					if ok && fields["status"] == "success" {
						tx.AddError(errors.New("PRIVATE result save failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer db.Callback().Update().Remove(callback)
			}
			cache := newQueryResultCache()
			raw := authorizedResultFixture()
			executor := &DevExecutor{queryResults: cache, sqlEngine: &SQLEngineService{protectionGate: &resultReadGate{}}}
			svc := &QueryExecutionService{executor: executor, queries: repository.NewExecutionRepository(db)}
			err := svc.completeQueryResult(t.Context(), execution, lease, commonModels.JSONMap{"rows": []map[string]interface{}{{"payload": "PRIVATE\x00"}}}, nil, &protection.QueryCapture{Subject: raw.Subject, Result: raw}, 1)
			if (err != nil) != stale {
				t.Fatalf("save failure completion: %v", err)
			}
			var stored commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if _, ok := cache.get(execution.ExecutionID, 1, 7, raw.Subject); ok {
				t.Fatal("failed save retained result cache")
			}
			if stored.Metadata["result"] != nil {
				t.Fatal("failed result persisted")
			}
			if stale {
				if stored.Status != "running" || len(stored.ErrorDetails) > 0 {
					t.Fatal("stale lease changed execution")
				}
				return
			}
			observed := commonExecution.Observe(&stored)
			diagnostic, _ := observed.ErrorDetails["diagnostic"].(*commonExecution.FailureDiagnostic)
			if stored.Status != "failed" || stored.ErrorDetails["code"] != "develop.query.result_invalid" || diagnostic == nil || diagnostic.Stage != "result" || diagnostic.Reason != "result_invalid" {
				t.Fatalf("missing save failure diagnostic: %+v", stored.ErrorDetails)
			}
			encoded := string(mustJSON(t, observed.ErrorDetails))
			if strings.Contains(encoded, "PRIVATE") || observed.ErrorDetails["diagnostic"] == nil {
				t.Fatal("unsafe or missing observed diagnostic")
			}
		})
	}
}

func persistedQueryGeometry() []byte {
	raw, _ := hex.DecodeString("0101000020e6100000000000000000f03f0000000000000040")
	return raw
}
