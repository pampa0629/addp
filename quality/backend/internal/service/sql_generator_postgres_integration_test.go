package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestIntegrationPostgresQualityQueryDeadlineCancelsStatement(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	err = db.WithContext(ctx).Exec("SELECT pg_sleep(5)").Error
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("pg_sleep error = %v, context error = %v, want deadline exceeded", err, ctx.Err())
	}
	if elapsed := time.Since(startedAt); elapsed > 2*time.Second {
		t.Fatalf("deadline cancellation took %v, want under 2s", elapsed)
	}
}

func TestIntegrationPostgresMaterializationGateAssertions(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schemaName := fmt.Sprintf("quality_gate_test_%d", time.Now().UnixNano())
	quotedSchema := `"` + strings.ReplaceAll(schemaName, `"`, `""`) + `"`
	if err := db.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error })
	if err := db.Exec("CREATE TABLE " + quotedSchema + `.persons (person_id TEXT PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("create persons: %v", err)
	}
	if err := db.Exec("CREATE TABLE " + quotedSchema + `.participations (person_id TEXT, activity_id TEXT, is_actual BOOLEAN, is_signup BOOLEAN)`).Error; err != nil {
		t.Fatalf("create participations: %v", err)
	}
	if err := db.Exec("INSERT INTO " + quotedSchema + `.persons(person_id) VALUES ('p1'), ('p2')`).Error; err != nil {
		t.Fatalf("insert persons: %v", err)
	}
	if err := db.Exec("INSERT INTO " + quotedSchema + `.participations(person_id, activity_id, is_actual, is_signup) VALUES ` +
		`('p1', 'a1', TRUE, TRUE), ('p1', 'a1', TRUE, TRUE), ('missing', 'a3', FALSE, FALSE), (NULL, 'a4', TRUE, FALSE)`).Error; err != nil {
		t.Fatalf("insert participations: %v", err)
	}

	config := &materializationGateExecutionConfig{
		TableBindings: []MaterializationGateTableBinding{{Alias: "persons", LogicalTableID: 1}, {Alias: "participations", LogicalTableID: 2}},
		Assertions: MaterializationGateAssertionDocument{Assertions: []MaterializationGateAssertion{
			{AssertionKey: "00000000-0000-4000-8000-000000000001", Type: "not_null", Severity: "error", Params: json.RawMessage(`{"table":"participations","column":"person_id"}`)},
			{AssertionKey: "00000000-0000-4000-8000-000000000002", Type: "unique_key", Severity: "error", Params: json.RawMessage(`{"table":"participations","columns":["person_id","activity_id"]}`)},
			{AssertionKey: "00000000-0000-4000-8000-000000000003", Type: "foreign_key", Severity: "error", Params: json.RawMessage(`{"table":"participations","columns":["person_id"],"reference_table":"persons","reference_columns":["person_id"]}`)},
			{AssertionKey: "00000000-0000-4000-8000-000000000004", Type: "predicate_implication", Severity: "error", Params: json.RawMessage(`{"table":"participations","when":{"column":"is_actual","operator":"is_true"},"then":{"column":"is_signup","operator":"is_true"}}`)},
			{AssertionKey: "00000000-0000-4000-8000-000000000005", Type: "row_count", Severity: "error", Params: json.RawMessage(`{"table":"participations","exact":4}`)},
		}},
	}
	readContext := &commonClient.MaterializationReadContext{Items: []commonClient.MaterializationReadItem{
		{LogicalTableID: 1, BatchID: "persons-batch", EngineID: 1, StagingLocator: fmt.Sprintf("addp://engine/1/path/%s/persons?type=table", schemaName), Columns: []commonClient.MaterializationReadColumn{{Name: "person_id"}}},
		{LogicalTableID: 2, BatchID: "participations-batch", EngineID: 1, StagingLocator: fmt.Sprintf("addp://engine/1/path/%s/participations?type=table", schemaName), Columns: []commonClient.MaterializationReadColumn{{Name: "person_id"}, {Name: "activity_id"}, {Name: "is_actual"}, {Name: "is_signup"}}},
	}}
	compiled, _, _, err := compileMaterializationGate(config, readContext)
	if err != nil {
		t.Fatalf("compile materialization gate: %v", err)
	}
	wantFailed := []int64{1, 1, 1, 1, 0}
	for index, assertion := range compiled {
		var counts gateCounts
		if err := db.Raw(assertion.SQL, assertion.Args...).Scan(&counts).Error; err != nil {
			t.Fatalf("execute %s: %v", assertion.Assertion.Type, err)
		}
		if counts.TotalCount != 4 || counts.FailedCount != wantFailed[index] {
			t.Fatalf("%s counts = %#v, want total=4 failed=%d", assertion.Assertion.Type, counts, wantFailed[index])
		}
		if assertion.RowCount != nil && !gateRowCountPassed(counts.TotalCount, *assertion.RowCount) {
			t.Fatalf("row_count unexpectedly failed: %#v", counts)
		}
	}
}

func TestIntegrationPostgresSixQualityRules(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set ADDP_POSTGRES_INTEGRATION=1 to run PostgreSQL integration test")
	}
	db, err := gorm.Open(postgres.Open(qualityServiceIntegrationDSN()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	schemaName := fmt.Sprintf("quality_rule_test_%d", time.Now().UnixNano())
	quotedSchema := `"` + strings.ReplaceAll(schemaName, `"`, `""`) + `"`
	if err := db.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE").Error })
	if err := db.Exec("CREATE TABLE " + quotedSchema + `.quality_values (text_value TEXT, number_value NUMERIC)`).Error; err != nil {
		t.Fatalf("create test table: %v", err)
	}
	if err := db.Exec("INSERT INTO "+quotedSchema+`.quality_values(text_value, number_value) VALUES (?, ?), (?, ?), (?, ?), (?, ?)`,
		"A1", 5, "A1", 20, "bad-value", 7, nil, nil).Error; err != nil {
		t.Fatalf("insert test values: %v", err)
	}

	tests := []struct {
		name       string
		column     string
		rule       string
		wantTotal  int64
		wantFailed int64
	}{
		{name: "not_null", column: "text_value", rule: `{"type":"not_null","enabled":true,"severity":"error","message":"","params":{}}`, wantTotal: 4, wantFailed: 1},
		{name: "unique", column: "text_value", rule: `{"type":"unique","enabled":true,"severity":"error","message":"","params":{}}`, wantTotal: 4, wantFailed: 2},
		{name: "format", column: "text_value", rule: `{"type":"format","enabled":true,"severity":"error","message":"","params":{"pattern":"^[A-Z][0-9]$"}}`, wantTotal: 4, wantFailed: 1},
		{name: "length", column: "text_value", rule: `{"type":"length","enabled":true,"severity":"error","message":"","params":{"min":2,"max":3}}`, wantTotal: 4, wantFailed: 1},
		{name: "value_range", column: "number_value", rule: `{"type":"value_range","enabled":true,"severity":"error","message":"","params":{"min":5,"max":10}}`, wantTotal: 4, wantFailed: 1},
		{name: "allowed_values", column: "text_value", rule: `{"type":"allowed_values","enabled":true,"severity":"error","message":"","params":{"values":["A1","B2"]}}`, wantTotal: 4, wantFailed: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			compiled, err := NewSQLGenerator().GenerateCheckSQL(schemaName, "quality_values", test.column, parseTestRule(t, test.rule))
			if err != nil {
				t.Fatalf("GenerateCheckSQL: %v", err)
			}
			var counts CheckCounts
			if err := db.Raw(compiled.SQL, compiled.Args...).Scan(&counts).Error; err != nil {
				t.Fatalf("execute compiled quality SQL: %v", err)
			}
			if counts.TotalCount != test.wantTotal || counts.FailedCount != test.wantFailed {
				t.Fatalf("counts = %#v, want total=%d failed=%d", counts, test.wantTotal, test.wantFailed)
			}
		})
	}
}

func qualityServiceIntegrationDSN() string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_HOST", "localhost"),
		qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_PORT", "15432"),
		qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_USER", "addp"),
		qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_PASSWORD", "addp_password"),
		qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_DATABASE", "addp_test"),
		qualityServiceIntegrationEnv("ADDP_TEST_POSTGRES_SSLMODE", "disable"),
	)
}

func qualityServiceIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
