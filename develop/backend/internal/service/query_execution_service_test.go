package service

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

	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTableResultExecutionMetadataRecordsAllFrozenBindingsAndWriteSemantics(t *testing.T) {
	inputs := map[string]string{
		"person":     "addp://engine/9/path/business/people?type=table&item_id=12",
		"activities": "addp://engine/9/path/business/events?type=table",
		"members":    "addp://engine/9/path/business/members?type=table",
	}
	target := "addp://engine/9/path/business/participation?type=table"
	for mode, wantMode := range map[string]string{"overwrite": "replace", "append": "append"} {
		t.Run(mode, func(t *testing.T) {
			metadata := tableResultExecutionMetadata("run", inputs, target, mode, 0)
			payload, err := json.Marshal(metadata["lineage_facts"])
			if err != nil {
				t.Fatal(err)
			}
			var facts commonExecution.LineageFacts
			if err := json.Unmarshal(payload, &facts); err != nil {
				t.Fatal(err)
			}
			wantInputs := []commonExecution.LineageResourceRef{
				{Port: "input.activities", Locator: inputs["activities"]},
				{Port: "input.members", Locator: inputs["members"]},
				{Port: "input.person", Locator: inputs["person"]},
			}
			if facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion || !reflect.DeepEqual(facts.Inputs, wantInputs) {
				t.Fatalf("inputs: %#v", facts)
			}
			if len(facts.Outputs) != 1 || facts.Outputs[0].Locator != target || facts.Outputs[0].WriteMode != wantMode {
				t.Fatalf("outputs: %#v", facts.Outputs)
			}
			wantOperation := commonExecution.LineageOperation{Kind: "derive", Operator: "develop", InputPorts: []string{"input.activities", "input.members", "input.person"}, OutputPorts: []string{"target"}}
			if len(facts.Operations) != 1 || !reflect.DeepEqual(facts.Operations[0], wantOperation) {
				t.Fatalf("operations: %#v", facts.Operations)
			}
			outputs := metadata["outputs"].(commonModels.JSONMap)
			if outputs["target_locator"] != target || outputs["row_count"] != int64(0) {
				t.Fatalf("outputs lost: %#v", outputs)
			}
		})
	}
}

func TestPersistedAuthorizationRehydratesDevelopQueryWithoutCredentialMaterial(t *testing.T) {
	now := time.Now().UTC()
	authorizationID, principalID, membershipID, version := int64(71), int64(11), int64(13), int64(17)
	expiresAt := now.Add(time.Minute)
	execution := &commonExecution.TaskExecution{
		TenantID: 7, Source: commonExecution.ModuleDevelop,
		ExecutionAuthorizationID: &authorizationID, AuthorizationExpiresAt: &expiresAt,
		ActorPrincipalID: &principalID, ActorTenantMembershipID: &membershipID,
		IssuedAuthorizationVersion: &version,
	}
	queryService := &QueryExecutionService{executor: &DevExecutor{
		sqlEngine: NewSQLEngineService(&config.Config{DefaultQueryTimeout: 30, MaxQueryTimeout: 300}, nil, nil),
	}}
	authorization, err := queryService.persistedAuthorization(context.Background(), execution, &models.DevTask{
		DevType: commonExecution.TaskTypeQuery, Timeout: 30,
		Content:         models.DevTaskContent{"query_type": "sql", "query": "SELECT 1"},
		ExecutionConfig: models.DevTaskContent{"engine_id": 12},
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorization.AuthorizationID != authorizationID || authorization.ActorPrincipalID != principalID ||
		len(authorization.EngineIDs) != 1 || authorization.EngineIDs[0] != 12 ||
		len(authorization.Effects) != 1 || authorization.Effects[0] != SQLExecutionEffectRead {
		t.Fatalf("authorization = %#v", authorization)
	}
	expired := now.Add(-time.Second)
	execution.AuthorizationExpiresAt = &expired
	if _, err := queryService.persistedAuthorization(context.Background(), execution, &models.DevTask{
		DevType: commonExecution.TaskTypeQuery, Timeout: 30,
		Content:         models.DevTaskContent{"query_type": "sql", "query": "SELECT 1"},
		ExecutionConfig: models.DevTaskContent{"engine_id": 12},
	}); err == nil {
		t.Fatal("expired persisted authorization was accepted")
	}
}

func TestQueryCompletionCollectsOnlyCommittedLineage(t *testing.T) {
	for _, scenario := range []string{"success", "notification_failure", "stale_lease", "write_failure", "cancelled", "ordinary_query"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := executiontest.EnsureSQLiteStore(db); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			token, owner := uuid.NewString(), "query-supervisor"
			expires := now.Add(time.Minute)
			execution := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "query", Source: "orchestrator", Status: "running", TriggerType: "manual", Attempt: 1, LeaseToken: &token, LeaseOwner: &owner, LeaseExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
			if err := db.Create(execution).Error; err != nil {
				t.Fatal(err)
			}
			lease := commonExecution.Lease{ExecutionID: execution.ExecutionID, TenantID: 7, Attempt: 1, Token: token, Owner: owner}
			if scenario == "stale_lease" {
				lease.Token = uuid.NewString()
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/meta/lineage/executions/"+execution.ExecutionID+"/collect" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				var stored commonExecution.TaskExecution
				if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
					t.Error(err)
				}
				if stored.Status != "success" || stored.Metadata["lineage_facts"] == nil {
					t.Errorf("notification preceded committed facts: %#v", stored)
				}
				if scenario == "notification_failure" {
					http.Error(w, "unavailable", 503)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"observed":1,"skipped":0}`))
			}))
			defer server.Close()
			worker := &QueryExecutionService{executor: &DevExecutor{metaClient: commonClient.NewMetaClient(server.URL, staticServiceTokenSource("addp_at_develop"))}, queries: repository.NewQueryExecutionRepository(db)}
			metadata := tableResultExecutionMetadata(execution.ExecutionID,
				map[string]string{"source": "addp://engine/9/path/public/source?type=table"},
				"addp://engine/9/path/public/result?type=table", "overwrite", 3)
			if scenario == "ordinary_query" {
				metadata = queryExecutionMetadata(commonModels.JSONMap{"rows": []interface{}{}})
			}
			if scenario == "write_failure" || scenario == "cancelled" {
				cause := errors.New("write rolled back")
				if scenario == "cancelled" {
					cause = context.Canceled
				}
				err = worker.completeFailure(context.Background(), execution, lease, now, cause, "develop.query.write_failed")
			} else {
				err = worker.completeSuccess(context.Background(), execution, lease, now, metadata, nil)
			}
			if (err != nil) != (scenario == "stale_lease") {
				t.Fatalf("completion error: %v", err)
			}
			wantCalls := 0
			if scenario == "success" || scenario == "notification_failure" {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("collector calls = %d, want %d", calls, wantCalls)
			}
			if wantCalls == 1 {
				if err := worker.completeSuccess(context.Background(), execution, lease, now, metadata, nil); err == nil {
					t.Fatal("repeated completion accepted a released lease")
				}
				if calls != wantCalls {
					t.Fatal("repeated completion notified the collector")
				}
			}
			var stored commonExecution.TaskExecution
			if err := db.Where("execution_id = ?", execution.ExecutionID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if wantCalls == 1 && (stored.Status != "success" || stored.Metadata["lineage_facts"] == nil) {
				t.Fatal("committed facts lost")
			}
			if (scenario == "write_failure" || scenario == "cancelled" || scenario == "stale_lease") && stored.Metadata["lineage_facts"] != nil {
				t.Fatal("unsuccessful execution published facts")
			}
		})
	}
}

func TestCompileExistingTableResultQueryQuotesRuntimeLocators(t *testing.T) {
	task := &models.DevTask{
		DevType: commonExecution.TaskTypeQuery,
		Content: models.DevTaskContent{
			"query_type": "sql", "query": "SELECT id, name FROM source WHERE id > :minimum_id",
			"query_parameters": []interface{}{
				map[string]interface{}{"name": "source", "type": "relation"},
				map[string]interface{}{"name": "minimum_id", "type": "integer", "default": 0},
			},
		},
		ExecutionConfig: models.DevTaskContent{"engine_id": 9},
	}
	compiled, err := compileExistingTableResultQuery(
		task,
		map[string]string{"source": "addp://engine/9/path/materialized/source_stage?type=table"},
		"addp://engine/9/path/materialized/write_stage?type=table",
		"postgresql",
	)
	if err != nil {
		t.Fatalf("compileExistingTableResultQuery: %v", err)
	}
	query := compiled.Content["query"].(string)
	want := `INSERT INTO "materialized"."write_stage" SELECT id, name FROM "materialized"."source_stage" WHERE id > :minimum_id`
	if query != want {
		t.Fatalf("compiled query = %q, want %q", query, want)
	}
	if task.Content["query"] == query {
		t.Fatal("compiler mutated the frozen source query")
	}
}

func TestCompileExistingTableResultQueryAcceptsDeclaredOpenGaussRelationCapability(t *testing.T) {
	task := &models.DevTask{
		DevType: commonExecution.TaskTypeQuery,
		Content: models.DevTaskContent{
			"query_type": "sql", "query": "SELECT id FROM source",
			"query_parameters": []interface{}{map[string]interface{}{"name": "source", "type": "relation"}},
		},
	}
	compiled, err := compileExistingTableResultQuery(
		task,
		map[string]string{"source": "addp://engine/9/path/business/source?type=table"},
		"addp://engine/9/path/business/result?type=table",
		"opengauss",
	)
	if err != nil {
		t.Fatalf("compile openGauss relation query: %v", err)
	}
	if got := compiled.Content["query"]; got != `INSERT INTO "business"."result" SELECT id FROM "business"."source"` {
		t.Fatalf("compiled query = %q", got)
	}
}

func TestRelationQueryDialectRejectsEngineWithoutDeclaredCapability(t *testing.T) {
	_, err := relationQueryDialectForEngine("mysql")
	if err == nil || !strings.Contains(err.Error(), "未声明 relation") {
		t.Fatalf("relation dialect error = %v", err)
	}
}

func TestCompileExistingTableResultQueryRejectsCrossEngineTarget(t *testing.T) {
	task := &models.DevTask{
		Content: models.DevTaskContent{
			"query_type": "sql", "query": "SELECT * FROM source",
			"query_parameters": []interface{}{map[string]interface{}{"name": "source", "type": "relation"}},
		},
	}
	_, err := compileExistingTableResultQuery(
		task,
		map[string]string{"source": "addp://engine/9/path/public/source?type=table"},
		"addp://engine/10/path/public/result?type=table",
		"postgresql",
	)
	if err == nil || !strings.Contains(err.Error(), "同引擎") {
		t.Fatalf("compile error = %v", err)
	}
}

func TestRelationRuntimeInputsReadDirectQueryParameterBindings(t *testing.T) {
	relationLocators, targetLocator, err := relationRuntimeInputs(
		models.DevTaskContent{
			"query_parameters": []interface{}{map[string]interface{}{"name": "source", "type": "relation"}},
		},
		models.DevTaskContent{
			"runtime_inputs": map[string]interface{}{
				"source":         map[string]interface{}{"locator": "addp://engine/9/path/public/source?type=table"},
				"target_locator": "addp://engine/9/path/public/result?type=table",
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if relationLocators["source"] != "addp://engine/9/path/public/source?type=table" ||
		targetLocator != "addp://engine/9/path/public/result?type=table" {
		t.Fatalf("relationLocators = %#v, targetLocator = %q", relationLocators, targetLocator)
	}
}

func TestCompileRelationParametersAllowsScopedCTE(t *testing.T) {
	bindings := []relationParameterBinding{{Name: "source"}}
	query := `WITH filtered AS (SELECT id FROM source WHERE enabled) SELECT id FROM filtered`
	compiled, err := compileRelationParameters(query, bindings, map[string]string{"source": `"stage"."source_1"`})
	if err != nil {
		t.Fatalf("compile scoped CTE: %v", err)
	}
	want := `WITH filtered AS (SELECT id FROM "stage"."source_1" WHERE enabled) SELECT id FROM filtered`
	if compiled != want {
		t.Fatalf("compiled query = %q, want %q", compiled, want)
	}
}

func TestCompileRelationParametersPreservesPostgreSQLCast(t *testing.T) {
	bindings := []relationParameterBinding{{Name: "source"}}
	query := `SELECT 'all'::text AS scope_type FROM source`
	compiled, err := compileRelationParameters(query, bindings, map[string]string{"source": `"stage"."source_1"`})
	if err != nil {
		t.Fatalf("compile PostgreSQL cast: %v", err)
	}
	want := `SELECT 'all'::text AS scope_type FROM "stage"."source_1"`
	if compiled != want {
		t.Fatalf("compiled query = %q, want %q", compiled, want)
	}
}

func TestCompileRelationParametersRejectsPhysicalRelation(t *testing.T) {
	_, err := compileRelationParameters(
		`SELECT source.id FROM source source JOIN public.secret s ON s.id = source.id`,
		[]relationParameterBinding{{Name: "source"}},
		map[string]string{"source": `"stage"."source_1"`},
	)
	if err == nil || !strings.Contains(err.Error(), "不允许 schema 限定关系") {
		t.Fatalf("physical relation error = %v", err)
	}
}

func TestCompileRelationParametersRejectsCTENameCollision(t *testing.T) {
	_, err := compileRelationParameters(
		`WITH source AS (SELECT 1 AS id) SELECT id FROM source`,
		[]relationParameterBinding{{Name: "source"}},
		map[string]string{"source": `"stage"."source_1"`},
	)
	if err == nil || !strings.Contains(err.Error(), "CTE 名称与 relation 查询参数重名") {
		t.Fatalf("CTE collision error = %v", err)
	}
}

func TestCompileRelationParametersRejectsOutOfScopeCTEReference(t *testing.T) {
	query := `SELECT * FROM (WITH hidden AS (SELECT 1 AS id) SELECT id FROM hidden) nested JOIN hidden ON true`
	_, err := compileRelationParameters(query, nil, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "禁止物理关系: hidden") {
		t.Fatalf("out-of-scope CTE error = %v", err)
	}
}

func TestDevQueryTaskFromExecutionUsesFrozenSnapshot(t *testing.T) {
	execution := &commonExecution.TaskExecution{
		TenantID: 7, TaskType: commonExecution.TaskTypeQuery,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id": 9, "timeout": 45,
			"content":            commonModels.JSONMap{"query_type": "sql", "query": "SELECT :limit", "query_parameters": []interface{}{map[string]interface{}{"name": "limit", "type": "integer", "default": 1}}},
			"runtime_parameters": commonModels.JSONMap{"limit": 3},
		},
	}
	task, err := devQueryTaskFromExecution(execution)
	if err != nil {
		t.Fatalf("devQueryTaskFromExecution: %v", err)
	}
	if task.Timeout != 45 || task.GetEngineID() == nil || *task.GetEngineID() != 9 || task.RuntimeParameters["limit"] != 3 {
		t.Fatalf("task snapshot = %#v", task)
	}
}

func TestExistingResultRejectsSelfInputRegardlessOfCatalogHints(t *testing.T) {
	task := &models.DevTask{Content: models.DevTaskContent{"query_type": "sql", "query": "SELECT id FROM source", "query_parameters": []interface{}{map[string]interface{}{"name": "source", "type": "relation"}}}}
	for _, target := range []string{"addp://engine/9/path/public/source?type=table", "addp://engine/9/path/public/source?type=table&item_id=9"} {
		_, err := compileExistingTableResultQuery(task, map[string]string{"source": "addp://engine/9/path/public/source?type=table"}, target, "postgresql")
		if err == nil {
			t.Fatalf("self input accepted: %s", target)
		}
	}
}
