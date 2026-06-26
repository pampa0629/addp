package repository

import (
	"strings"
	"testing"
)

func TestNormalizeDevTaskContentStatementsRemoveLegacyFieldsWithoutMigration(t *testing.T) {
	statements := normalizeDevTaskContentStatements()
	joined := make([]string, 0, len(statements))
	for _, stmt := range statements {
		joined = append(joined, stmt.sql)
	}
	sqlText := strings.Join(joined, "\n")

	requiredDeletes := []string{
		"content = content - 'sql'",
		"content = content - 'workflow_def'",
		"content = content - 'nodes' - 'edges'",
		"content = content - 'input_data'",
	}
	for _, fragment := range requiredDeletes {
		if !strings.Contains(sqlText, fragment) {
			t.Fatalf("normalization SQL missing legacy field removal %q", fragment)
		}
	}

	forbiddenMigrations := []string{
		"content->'sql'",
		"content->'workflow_def'",
		"content->'input_data'",
		"jsonb_build_object",
	}
	for _, fragment := range forbiddenMigrations {
		if strings.Contains(sqlText, fragment) {
			t.Fatalf("normalization SQL must not migrate legacy field with %q", fragment)
		}
	}
}
