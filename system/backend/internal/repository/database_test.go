package repository

import (
	"strings"
	"testing"
)

func TestDropDeprecatedEngineColumnsSQLRemovesLegacyEngineFields(t *testing.T) {
	requiredFragments := []string{
		"DROP INDEX system.idx_engines_identifier",
		"column_name = 'unique_identifier'",
		"ALTER TABLE system.engines DROP COLUMN unique_identifier",
		"column_name = 'extension_api_config'",
		"ALTER TABLE system.engines DROP COLUMN extension_api_config",
		"column_name = 'health_check_config'",
		"ALTER TABLE system.engines DROP COLUMN health_check_config",
	}

	for _, fragment := range requiredFragments {
		if !strings.Contains(dropDeprecatedEngineColumnsSQL, fragment) {
			t.Fatalf("dropDeprecatedEngineColumnsSQL missing %q", fragment)
		}
	}
}

func TestRemoveBuiltinMathWorkflowExampleSQLDeletesOnlyBuiltinMathWorkflow(t *testing.T) {
	requiredFragments := []string{
		"DELETE FROM system.engines",
		"lower(engine_type) = 'math_workflow'",
		"AND is_builtin = true",
	}

	for _, fragment := range requiredFragments {
		if !strings.Contains(removeBuiltinMathWorkflowExampleSQL, fragment) {
			t.Fatalf("removeBuiltinMathWorkflowExampleSQL missing %q", fragment)
		}
	}
}

func TestRenameGeoPythonWorkflowEngineTypeSQLUsesSingleNewIdentity(t *testing.T) {
	requiredFragments := []string{
		"lower(engine_type) = 'python_workflow'",
		"lower(engine_type) = 'geopython_workflow'",
		"RAISE EXCEPTION",
		"engine_type = 'geopython_workflow'",
		"name = 'GeoPython 工作流引擎'",
	}

	for _, fragment := range requiredFragments {
		if !strings.Contains(renameGeoPythonWorkflowEngineTypeSQL, fragment) {
			t.Fatalf("renameGeoPythonWorkflowEngineTypeSQL missing %q", fragment)
		}
	}
}
