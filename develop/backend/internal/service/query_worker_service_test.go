package service

import (
	"strings"
	"testing"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/develop/backend/internal/models"
)

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
