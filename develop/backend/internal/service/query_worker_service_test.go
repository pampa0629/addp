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
			"query_type": "sql", "query": "SELECT id, name FROM addp_input.source WHERE id > :minimum_id",
			"relation_inputs": []interface{}{"source"},
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
			"query_type": "sql", "query": "SELECT * FROM addp_input.source",
			"relation_inputs": []interface{}{"source"},
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

func TestCompileRelationInputsAllowsScopedCTE(t *testing.T) {
	bindings := []relationInputBinding{{Name: "source"}}
	query := `WITH filtered AS (SELECT id FROM addp_input.source WHERE enabled) SELECT id FROM filtered`
	compiled, err := compileRelationInputs(query, bindings, map[string]string{"source": `"stage"."source_1"`})
	if err != nil {
		t.Fatalf("compile scoped CTE: %v", err)
	}
	want := `WITH filtered AS (SELECT id FROM "stage"."source_1" WHERE enabled) SELECT id FROM filtered`
	if compiled != want {
		t.Fatalf("compiled query = %q, want %q", compiled, want)
	}
}

func TestCompileRelationInputsPreservesPostgreSQLCast(t *testing.T) {
	bindings := []relationInputBinding{{Name: "source"}}
	query := `SELECT 'all'::text AS scope_type FROM addp_input.source`
	compiled, err := compileRelationInputs(query, bindings, map[string]string{"source": `"stage"."source_1"`})
	if err != nil {
		t.Fatalf("compile PostgreSQL cast: %v", err)
	}
	want := `SELECT 'all'::text AS scope_type FROM "stage"."source_1"`
	if compiled != want {
		t.Fatalf("compiled query = %q, want %q", compiled, want)
	}
}

func TestCompileRelationInputsRejectsPhysicalRelation(t *testing.T) {
	_, err := compileRelationInputs(
		`SELECT input.id FROM addp_input.source input JOIN public.secret s ON s.id = input.id`,
		[]relationInputBinding{{Name: "source"}},
		map[string]string{"source": `"stage"."source_1"`},
	)
	if err == nil || !strings.Contains(err.Error(), "只能读取 addp_input") {
		t.Fatalf("physical relation error = %v", err)
	}
}

func TestCompileRelationInputsRejectsOutOfScopeCTEReference(t *testing.T) {
	query := `SELECT * FROM (WITH hidden AS (SELECT 1 AS id) SELECT id FROM hidden) nested JOIN hidden ON true`
	_, err := compileRelationInputs(query, nil, map[string]string{})
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
