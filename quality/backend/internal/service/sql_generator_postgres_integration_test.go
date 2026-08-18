package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

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
