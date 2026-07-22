package repository

import (
	"strings"
	"testing"

	"github.com/addp/system/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestEnsureBuiltinOAuthClientsReplacesFixedLoopbackPort(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatalf("attach system schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE system.oauth_clients (client_id TEXT PRIMARY KEY, name TEXT NOT NULL, client_type TEXT NOT NULL, redirect_uris TEXT NOT NULL, allowed_scopes TEXT NOT NULL, device_flow_enabled NUMERIC NOT NULL, is_active NUMERIC NOT NULL, created_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		t.Fatalf("create oauth clients table: %v", err)
	}
	legacyClient := models.OAuthClient{
		ClientID:          "addp-cli",
		Name:              "ADDP CLI",
		ClientType:        models.OAuthClientTypePublic,
		RedirectURIs:      []string{"http://127.0.0.1:8765/callback"},
		AllowedScopes:     []string{"addp.api"},
		DeviceFlowEnabled: true,
		IsActive:          true,
	}
	if err := db.Create(&legacyClient).Error; err != nil {
		t.Fatalf("create legacy oauth client: %v", err)
	}

	if err := EnsureBuiltinOAuthClients(db); err != nil {
		t.Fatalf("ensure builtin oauth client: %v", err)
	}
	var stored models.OAuthClient
	if err := db.First(&stored, "client_id = ?", "addp-cli").Error; err != nil {
		t.Fatalf("load oauth client: %v", err)
	}
	if len(stored.RedirectURIs) != 1 || stored.RedirectURIs[0] != "http://127.0.0.1/callback" {
		t.Fatalf("redirect URIs = %#v", stored.RedirectURIs)
	}
}

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

func TestScrubOAuthAuditRequestBodiesSQLRemovesHistoricalSecrets(t *testing.T) {
	for _, fragment := range []string{
		"UPDATE system.audit_logs",
		"SET request_body = '', query_params = '', error_message = ''",
		"resource_path LIKE '/api/v1/system/oauth/%'",
		"request_body <> '' OR query_params <> '' OR error_message <> ''",
	} {
		if !strings.Contains(scrubOAuthAuditRequestBodiesSQL, fragment) {
			t.Fatalf("scrubOAuthAuditRequestBodiesSQL missing %q", fragment)
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
