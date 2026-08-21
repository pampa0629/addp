package migration

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedMigrationCatalog(t *testing.T) {
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("ReadCatalog() error = %v", err)
	}
	if catalog.LatestVersion != 68 {
		t.Fatalf("LatestVersion = %d, want 68", catalog.LatestVersion)
	}
}

func TestDuckDBPlatformRuntimeMigrationPublishesRegistrationAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000068_iam_duckdb_platform_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 68: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'platform.duckdb_runtime'",
		"'system.runtime_registry.update'",
		"'addp-duckdb'",
		"INSERT INTO system.role_assignments",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 68 missing %q", fragment)
		}
	}
}

func TestStandardReferenceRuntimeMigrationPublishesDeletionGuardBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000065_iam_standard_reference_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 65: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'model.standard_reference.update'",
		"'tenant.standard_runtime'",
		"'addp-standard'",
		"INSERT INTO system.tenant_memberships",
		"INSERT INTO system.role_assignments",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 65 missing %q", fragment)
		}
	}
}

func TestQualityExecutionAuthorizationMigrationPublishesCompleteBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000064_iam_quality_execution_authorization.up.sql")
	if err != nil {
		t.Fatalf("read migration 64: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'addp-quality'",
		"'tenant.governance_manager'",
		"'develop.data_read.execute'",
		"'system.execution_authorization.create'",
		"'tenant.quality_runtime'",
		"'system.execution_authorization.execute'",
		"ON CONFLICT (role_id, permission_id) DO NOTHING",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 64 missing %q", fragment)
		}
	}
}

func TestModelTenantRuntimeMigrationPublishesTenantReferenceReadBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000063_iam_model_tenant_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 63: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.model_runtime'",
		"'standard.dimension_hierarchy.read'",
		"'standard.domain.read'",
		"'standard.element.read'",
		"'standard.metric.read'",
		"INSERT INTO system.tenant_memberships",
		"INSERT INTO system.role_assignments",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 63 missing %q", fragment)
		}
	}
}

func TestDataArchitectStandardReferenceMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000062_iam_data_architect_standard_references.up.sql")
	if err != nil {
		t.Fatalf("read migration 62: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.data_architect'", "'standard.dimension_hierarchy.read'", "'standard.domain.read'",
		"'standard.element.read'", "'standard.metric.read'", "ON CONFLICT (role_id, permission_id) DO NOTHING",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 62 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "INSERT INTO system.role_assignments") {
		t.Fatal("migration 62 must not assign roles to principals")
	}
}

func TestModelTenantScopeMigrationMovesAuthorizationToDataArchitect(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000061_iam_model_tenant_scope.up.sql")
	if err != nil {
		t.Fatalf("read migration 61: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.data_architect'",
		"'tenant.governance_manager'",
		"permission.permission_key LIKE 'model.%'",
		"DELETE FROM system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 61 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "INSERT INTO system.role_assignments") {
		t.Fatal("migration 61 must not assign the Data Architect role to any principal")
	}
}

func TestDataArchitectRoleMigrationPublishesDWLayerAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000060_iam_data_architect_role.up.sql")
	if err != nil {
		t.Fatalf("read migration 60: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.data_architect'",
		"ARRAY['tenant']::text[]",
		"ARRAY['user']::text[]",
		"'model.dw_layer.create'",
		"'model.dw_layer.delete'",
		"'model.dw_layer.read'",
		"'model.dw_layer.update'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 60 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "INSERT INTO system.role_assignments") {
		t.Fatal("migration 60 must not assign the Data Architect role to any principal")
	}
}

func TestWorkflowRuntimeServicePrincipalsMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000056_iam_workflow_runtime_service_principals.up.sql")
	if err != nil {
		t.Fatalf("read migration 56: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'addp-geopython'", "'addp-model3d'", "'addp-pointcloud'", "'addp-spark'",
		"'system.runtime_registry.update'", "'manager.derived_artifact.create'",
		"INSERT INTO system.tenant_memberships",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 56 missing %q", fragment)
		}
	}
}

func TestRemainingServiceRuntimesMigrationPublishesOAuthAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000055_iam_remaining_service_runtimes.up.sql")
	if err != nil {
		t.Fatalf("read migration 55: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'platform.gateway_runtime'",
		"'platform.model_runtime'",
		"'platform.quality_runtime'",
		"'platform.standard_runtime'",
		"'system.api_key.read'",
		"ARRAY['platform', 'tenant']::text[]",
		"'addp-gateway'",
		"'addp-model'",
		"'addp-standard'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 55 missing %q", fragment)
		}
	}
}

func TestPlatformRuntimeRolesMigrationPublishesOAuthRegistrationRoles(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000054_iam_platform_runtime_roles.up.sql")
	if err != nil {
		t.Fatalf("read migration 54: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'platform.monitor_runtime', 'addp-monitor'",
		"'platform.service_runtime', 'addp-service'",
		"'platform.transfer_runtime', 'addp-transfer'",
		"'system.runtime_registry.update'",
		"INSERT INTO system.role_assignments",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 54 missing %q", fragment)
		}
	}
}

func TestQueryParameterCapabilitiesMigrationBackfillsSupportedEngines(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000049_query_parameter_capabilities.up.sql")
	if err != nil {
		t.Fatalf("read migration 49: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'{compute,query,parameters}'",
		"engine_type IN ('postgresql', 'mysql', 'doris', 'clickhouse', 'mongodb', 'neo4j')",
		`"languages":["sql"]`,
		`"languages":["mql"]`,
		`"languages":["cypher"]`,
		`"types":["string","integer","number","boolean"]`,
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 49 missing %q", fragment)
		}
	}
}

func TestRuntimeEngineDescriptorConsumersMigrationPublishesAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000048_iam_runtime_engine_descriptor_consumers.up.sql")
	if err != nil {
		t.Fatalf("read migration 48: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.manager_runtime'",
		"'tenant.meta_runtime'",
		"'tenant.transfer_runtime'",
		"'system.engine_descriptor.read'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 48 missing %q", fragment)
		}
	}
}

func TestManagerTransferRuntimeMigrationPublishesAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000066_iam_manager_transfer_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 66: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.manager_runtime'",
		"'transfer.task.create'",
		"'transfer.task.execute'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 66 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "'transfer.task.read'") {
		t.Fatal("migration 66 must not contain the read permission introduced by migration 67")
	}
}

func TestManagerTransferReadMigrationPublishesAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000067_iam_manager_transfer_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 67: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.manager_runtime'",
		"'transfer.task.read'",
		"INSERT INTO system.role_permissions",
		"ON CONFLICT (role_id, permission_id) DO NOTHING",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 67 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "'transfer.task.create'") || strings.Contains(sql, "'transfer.task.execute'") {
		t.Fatal("migration 67 must contain only the read permission introduced at version 67")
	}
}

func TestManagerTransferMigrationChecksumsRemainImmutable(t *testing.T) {
	expected := map[string]string{
		"sql/000066_iam_manager_transfer_runtime.up.sql": "a3fe083cd62b9ab05c75eeed74d0ce0d0233485ce3f819973f12d4eeb66c5d15",
		"sql/000067_iam_manager_transfer_read.up.sql":    "e57358874ef50612a737e12986b609f73524515b551c3d8c8fc918f141f843f2",
	}
	for filename, want := range expected {
		data, err := fs.ReadFile(EmbeddedSQL, filename)
		if err != nil {
			t.Fatalf("read %s: %v", filename, err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			t.Fatalf("%s checksum = %s, want %s", filename, got, want)
		}
	}
}

func TestInferenceRuntimeDescriptorConsumersMigrationPublishesAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000059_iam_inference_runtime_descriptor_consumers.up.sql")
	if err != nil {
		t.Fatalf("read migration 59: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'system.engine_descriptor.read'",
		"'tenant.agent_runtime'",
		"'tenant.copilot_runtime'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 59 missing %q", fragment)
		}
	}
}

func TestManagedUserMFAResetMigrationPublishesSecurityAdministratorAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000047_iam_managed_user_mfa_reset.up.sql")
	if err != nil {
		t.Fatalf("read migration 47: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'iam.mfa_credential.reset'",
		"'platform.security_administrator'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 47 missing %q", fragment)
		}
	}
}

func TestCopilotDevelopCatalogMigrationPublishesRuntimeAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000046_iam_copilot_develop_catalog.up.sql")
	if err != nil {
		t.Fatalf("read migration 46: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.copilot_runtime'",
		"'develop.task.read'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 46 missing %q", fragment)
		}
	}
}

func TestManagerInferenceBindingMigrationPublishesTenantAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000045_iam_manager_inference_binding.up.sql")
	if err != nil {
		t.Fatalf("read migration 45: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'manager.configuration.read'",
		"'manager.configuration.update'",
		"ARRAY['platform', 'tenant']::text[]",
		"role.role_key = 'tenant.administrator'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 45 missing %q", fragment)
		}
	}
}

func TestAgentInferenceBindingMigrationPublishesConfigurationAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000044_iam_agent_inference_bindings.up.sql")
	if err != nil {
		t.Fatalf("read migration 44: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'agent.configuration.read'",
		"'agent.configuration.update'",
		"'platform.system_administrator'",
		"'tenant.administrator'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 44 missing %q", fragment)
		}
	}
}

func TestCopilotInferenceBindingMigrationPublishesGraphServiceAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000043_iam_copilot_inference_bindings.up.sql")
	if err != nil {
		t.Fatalf("read migration 43: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'copilot.configuration.read'",
		"'copilot.configuration.update'",
		"'copilot.knowledge_graph.execute'",
		"'platform.graph_runtime'",
		"'tenant.graph_runtime'",
		"'addp-graph'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 43 missing %q", fragment)
		}
	}
}

func TestInferenceRuntimeMigrationPublishesPermissionsRolesAndPrincipals(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000042_iam_inference_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 42: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'inference.runtime.execute'",
		"'inference.deployment.execute'",
		"'inference.provider_credential.update'",
		"'platform.inference_runtime'",
		"'tenant.agent_runtime'",
		"'tenant.copilot_runtime'",
		"'tenant.manager_runtime', 'inference.runtime.execute'",
		"'addp-agent'",
		"'addp-copilot'",
		"'addp-inference'",
		"permission.permission_key <> 'inference.runtime.execute'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 42 missing %q", fragment)
		}
	}
}

func TestIAMSecurityPolicyMigrationPublishesPolicyAndAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000041_iam_security_policy.up.sql")
	if err != nil {
		t.Fatalf("read migration 41: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"CREATE TABLE system.iam_security_policy",
		"CREATE INDEX idx_iam_security_policy_updated_by_principal_id",
		"applied_version bigint NOT NULL",
		"'iam.security_policy.read'",
		"'iam.security_policy.update'",
		"role.role_key = 'platform.security_administrator'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 41 missing %q", fragment)
		}
	}
}

func TestManagerPlatformRuntimeMigrationPublishesRoleAndAssignment(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000040_iam_manager_platform_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 40: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'platform.manager_runtime'",
		"ARRAY['platform']::text[]",
		"ARRAY['service_principal']::text[]",
		"permission.permission_key = 'system.runtime_registry.update'",
		"service_principal.name = 'addp-manager'",
		"INSERT INTO system.role_assignments",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 40 missing %q", fragment)
		}
	}
}

func TestManagerPlatformConfigurationAuthorizationMigrationPublishesPermissionAndRoleBinding(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000039_manager_platform_configuration_authorization.up.sql")
	if err != nil {
		t.Fatalf("read migration 39: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'manager.configuration.read', 'manager', 'read'",
		"'manager.configuration.update', 'manager', 'update'",
		"ARRAY['platform']::text[]",
		"role.role_key = 'platform.system_administrator'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 39 missing %q", fragment)
		}
	}
}

func TestNotebookSessionAuthorizationRepairMigrationPublishesCanonicalState(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000038_iam_notebook_session_authorization_repair.up.sql")
	if err != nil {
		t.Fatalf("read migration 38: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"to_regclass('system.notebook_catalog_authorizations')",
		"RENAME TO notebook_session_authorizations",
		"CREATE TABLE system.notebook_session_authorizations",
		"source_notebook_session_authorization_id",
		"system.schema_migration_checksums",
		"legacy notebook catalog authorization schema still exists",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 38 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE IF EXISTS system.notebook_catalog_authorizations",
		"DELETE FROM system.permissions",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 38 must preserve IAM history, found %q", forbidden)
		}
	}
}

func TestNotebookSessionAuthorizationMigrationPublishesBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000037_iam_notebook_session_authorization.up.sql")
	if err != nil {
		t.Fatalf("read migration 37: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"CREATE TABLE system.notebook_session_authorizations",
		"operations text[] NOT NULL CHECK",
		"ARRAY['catalog.list_children', 'execution_engine_access.derive']::text[]",
		"token_family_id bigint NOT NULL REFERENCES system.refresh_token_families(id)",
		"source_notebook_session_authorization_id uuid",
		"trg_notebook_session_authorizations_revoke_executions",
		"'system.notebook_session_authorization.execute'",
		"role.role_key = 'tenant.develop_runtime'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 37 missing %q", fragment)
		}
	}
}

func TestServiceQuerySampleMigrationPublishesAuthorizationBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000036_iam_service_query_sample.up.sql")
	if err != nil {
		t.Fatalf("read migration 36: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"audience IN ('develop', 'duckdb', 'service')",
		"'service.data_read.execute'",
		"'system.execution_authorization.create'",
		"role.role_key = 'tenant.service_publisher'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 36 missing %q", fragment)
		}
	}
}

func TestDuckDBRuntimeMigrationPublishesAuthorizationBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000035_iam_duckdb_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 35: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"execution_authorizations_source_check",
		"source_type = 'service_definition'",
		"'tenant.duckdb_runtime'",
		"'addp-duckdb'",
		"'meta.catalog.read'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 35 missing %q", fragment)
		}
	}
}

func TestDevelopNotebookUpdateMigrationPublishesPermissionAndRoleBinding(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000034_iam_develop_notebook_update.up.sql")
	if err != nil {
		t.Fatalf("read migration 34: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"permission_key = 'develop.notebook.update'",
		"SET status = 'active'",
		"role.role_key = 'tenant.data_engineer'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 34 missing %q", fragment)
		}
	}
}

func TestReadCatalogRejectsVersionGap(t *testing.T) {
	_, err := ReadCatalog(fstest.MapFS{
		"sql/000001_first.up.sql": {},
		"sql/000003_third.up.sql": {},
	}, "sql")
	if err == nil || !strings.Contains(err.Error(), "expected 000002") {
		t.Fatalf("ReadCatalog() error = %v, want version gap", err)
	}
}

func TestReadCatalogRejectsNonMigrationFile(t *testing.T) {
	_, err := ReadCatalog(fstest.MapFS{"sql/README.md": {}}, "sql")
	if err == nil || !strings.Contains(err.Error(), "invalid migration filename") {
		t.Fatalf("ReadCatalog() error = %v, want invalid filename", err)
	}
}
