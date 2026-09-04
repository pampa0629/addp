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
	if catalog.LatestVersion != 121 {
		t.Fatalf("LatestVersion = %d, want 121", catalog.LatestVersion)
	}
}

func TestStandardCollectionMigrationPublishesGovernancePermissionsAndRuntimeRead(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000120_iam_standard_governance_user_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 120: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'standard.collection.create'", "'standard.collection.delete'", "'standard.collection.publish'",
		"'standard.collection.read'", "'standard.collection.update'", "'standard.collection_assignment.update'",
		"'tenant.governance_manager'", "'iam.tenant_membership.read'", "'tenant.standard_runtime'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 120 missing %q", fragment)
		}
	}
}

func TestSecurityManualAssessmentMigrationCreatesExactPermission(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000119_iam_security_manual_assessment.up.sql")
	if err != nil {
		t.Fatalf("read migration 119: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'security.assessment.create'", "'tenant.administrator'", "'tenant.governance_manager'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 119 missing %q", fragment)
		}
	}
}

func TestSecurityDetectorMigrationCreatesExactCRUDPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000118_iam_security_detector.up.sql")
	if err != nil {
		t.Fatalf("read migration 118: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'security.detector.create'", "'security.detector.delete'", "'security.detector.read'", "'security.detector.update'",
		"'tenant.administrator'", "'tenant.governance_manager'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 118 missing %q", fragment)
		}
	}
}

func TestSecurityPolicyMigrationCreatesExactCRUDPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000117_iam_security_policy.up.sql")
	if err != nil {
		t.Fatalf("read migration 117: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'security.policy.create'", "'security.policy.delete'", "'security.policy.read'", "'security.policy.update'",
		"'tenant.administrator'", "'tenant.governance_manager'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 117 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "security.policy.manage") {
		t.Fatal("migration 117 must not introduce non-canonical manage action")
	}
}

func TestSecurityAssessmentMigrationCreatesExactGovernancePermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000116_iam_security_assessment.up.sql")
	if err != nil {
		t.Fatalf("read migration 116: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'security.assessment.read'", "'security.assessment.update'", "'security.finding.update'",
		"'tenant.administrator'", "'tenant.governance_manager'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 116 missing %q", fragment)
		}
	}
}

func TestSecurityMetaFactsMigrationCreatesExactTenantRuntimeBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000115_iam_security_meta_facts.up.sql")
	if err != nil {
		t.Fatalf("read migration 115: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'meta.security_facts.read'", "'security.finding.read'", "'tenant.security_runtime'",
		"'addp-security'", "ARRAY['tenant']::text[]", "false",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 115 missing %q", fragment)
		}
	}
}

func TestSecurityEnrollmentProjectionMigrationKeepsOwnerRuntimeBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000114_iam_security_enrollment_projection.up.sql")
	if err != nil {
		t.Fatalf("read migration 114: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'security.enrollment.create'", "'security.enrollment.read'", "'security.enrollment.update'",
		"'security.protection_projection.read'", "'security.protection_projection.update'",
		"'tenant.develop_runtime'", "'tenant.manager_runtime'", "'tenant.service_runtime'", "'tenant.transfer_runtime'",
		"'platform.tenant.read'", "'platform.develop_runtime'", "'platform.manager_runtime'", "'platform.service_runtime'", "'platform.transfer_runtime'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 114 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "tenant.security_runtime") {
		t.Fatal("migration 114 must not create tenant Security runtime membership")
	}
}

func TestSecurityModuleMigrationOwnsPermissionsAndRuntimeIdentity(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000113_iam_security_module.up.sql")
	if err != nil {
		t.Fatalf("read migration 113: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"permission_key LIKE 'standard.classification.%'",
		"'security.classification.read'",
		"'security.grade.read'",
		"'security.sensitive_data_type.read'",
		"'security.protection_baseline.read'",
		"'platform.security_runtime'",
		"'addp-security'",
		"'tenant.administrator', 'tenant.governance_manager'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 113 missing %q", fragment)
		}
	}
}

func TestExternalOAuthClientManagementMigrationKeepsTenantOwnershipAndRevocationBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000111_iam_external_oauth_client_management.up.sql")
	if err != nil {
		t.Fatalf("read migration 111: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"ADD COLUMN owner_scope", "ADD COLUMN owner_tenant_id", "ADD COLUMN version",
		"oauth_clients_management_owner_check", "idx_oauth_clients_created_by_principal", "'iam.oauth_client.suspend'",
		"'tenant.administrator'", "authorization_version = principal.authorization_version + 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 111 missing %q", fragment)
		}
	}
}

func TestInvitationEnrollmentTicketRemovalMigrationKeepsSingleAcceptancePath(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000108_iam_remove_invitation_enrollment_ticket.up.sql")
	if err != nil {
		t.Fatalf("read migration 108: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"DROP TABLE system.enrollment_tickets",
		"DROP FUNCTION system.validate_enrollment_ticket_transition()",
		"DROP FUNCTION system.prevent_invitation_enrollment_delete()",
		"DROP COLUMN enrollment_ticket_ttl_minutes",
		"CREATE FUNCTION system.prevent_tenant_invitation_delete()",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 108 missing %q", fragment)
		}
	}
}

func TestWorkbenchResourceGrantMigrationPublishesAssetRuntimePermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000107_iam_workbench_resource_grant.up.sql")
	if err != nil {
		t.Fatalf("read migration 107: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'workbench.resource_grant.create'", "'workbench.resource_grant.revoke'",
		"ARRAY['tenant']::text[]", "'tenant.asset_runtime'", "authorization_version = principal.authorization_version + 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 107 missing %q", fragment)
		}
	}
}

func TestWorkbenchCatalogReadMigrationPublishesNarrowPermission(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000105_iam_workbench_catalog_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 105: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'workbench.catalog.read'", "ARRAY['tenant']::text[]", "false",
		"'tenant.catalog_runtime'", "authorization_version = principal.authorization_version + 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 105 missing %q", fragment)
		}
	}
}

func TestWorkbenchDataApplicationMigrationPublishesPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000104_iam_workbench_data_application.up.sql")
	if err != nil {
		t.Fatalf("read migration 104: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'workbench.data_application.create'", "'workbench.data_application.delete'",
		"'workbench.data_application.execute'", "'workbench.data_application.publish'",
		"'workbench.data_application.read'", "'workbench.data_application.update'",
		"'tenant.administrator'", "'tenant.data_viewer'", "authorization_version = principal.authorization_version + 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 104 missing %q", fragment)
		}
	}
}

func TestTransferTaskProviderRuntimeMigrationPublishesNarrowPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000103_iam_transfer_task_provider.up.sql")
	if err != nil {
		t.Fatalf("read migration 103: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'transfer.task_provider.execute'", "'transfer.task_provider.read'",
		"'tenant.orchestrator_runtime'", "authorization_version = principal.authorization_version + 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 103 missing %q", fragment)
		}
	}
}

func TestQualityMaterializationGateMigrationPublishesPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000093_iam_quality_materialization_gate.up.sql")
	if err != nil {
		t.Fatalf("read migration 93: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'quality.materialization_gate.read'", "'quality.task_provider.execute'", "'tenant.orchestrator_runtime'", "'tenant.quality_runtime'", "'model.materialization_group.read'"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 93 missing %q", fragment)
		}
	}
}

func TestWorkbenchRuntimeMigrationPublishesConsumerBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000092_iam_workbench_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 92: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'workbench.view.create'", "'workbench.view.delete'",
		"'workbench.view.read'", "'workbench.view.update'",
		"'platform.workbench_runtime'", "'addp-workbench'",
		"'tenant.data_viewer'", "'service.data_read.execute'",
		"'system.runtime_registry.update'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 92 missing %q", fragment)
		}
	}
}

func TestWorkbenchViewRemovalMigrationDisablesRetiredPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000112_iam_remove_workbench_view.up.sql")
	if err != nil {
		t.Fatalf("read migration 112: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"DELETE FROM system.role_permissions", "UPDATE system.permissions", "status = 'disabled'",
		"'workbench.view.create'", "'workbench.view.delete'", "'workbench.view.read'", "'workbench.view.update'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 112 missing %q", fragment)
		}
	}
}

func TestOrganizationManagementMigrationAddsVersionsAndPublishesPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000091_iam_organization_management.up.sql")
	if err != nil {
		t.Fatalf("read migration 91: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"ALTER TABLE system.departments", "ALTER TABLE system.department_memberships",
		"ALTER TABLE system.project_groups", "ALTER TABLE system.project_group_memberships",
		"ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0)",
		"DROP CONSTRAINT project_group_memberships_project_group_id_tenant_membershi_key",
		"'iam.department.restore'", "'tenant.administrator'",
		"permission.permission_key = 'iam.department.delete'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 91 missing %q", fragment)
		}
	}
}

func TestServiceExecutionAuditMigrationGrantsTenantAuditAppend(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000090_iam_service_execution_audit.up.sql")
	if err != nil {
		t.Fatalf("read migration 90: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'audit.tenant_event.create'", "'tenant.service_runtime'", "ON CONFLICT (role_id, permission_id) DO NOTHING"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 90 missing %q", fragment)
		}
	}
}

func TestExecutionAuthorizationEngineAccessScopesMigrationIsExactAndImmutable(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000084_iam_execution_authorization_engine_access_scopes.up.sql")
	if err != nil {
		t.Fatalf("read migration 84: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"system.execution_authorization_engine_accesses",
		"PRIMARY KEY (authorization_id, engine_id)",
		"DROP COLUMN effects",
		"DROP COLUMN engine_ids",
		"execution authorization access boundary is immutable",
		"execution authorization sealing requires a non-empty immutable access boundary",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 84 missing %q", fragment)
		}
	}
}

func TestPortalTenantRuntimeRemovalMigrationRevokesAssignmentAndDisablesRole(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000085_iam_remove_portal_tenant_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 85: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'tenant.portal_runtime'", "'addp-portal'", "UPDATE system.role_assignments", "status = 'revoked'", "revoked_at", "status = 'disabled'"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 85 missing %q", fragment)
		}
	}
}

func TestLegacyServiceEndpointProjectionPermissionIsDisabled(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000086_iam_remove_service_endpoint_projection.up.sql")
	if err != nil {
		t.Fatalf("read migration 86: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'service.endpoint.read'", "DELETE FROM system.role_permissions", "status = 'disabled'"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 86 missing %q", fragment)
		}
	}
}

func TestManagerContentProjectionPermissionIsAssignedToMetaRuntimeAndTenantAdministrator(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000087_iam_manager_content_index_projection.up.sql")
	if err != nil {
		t.Fatalf("read migration 87: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'manager.content_index.update'", "'tenant.meta_runtime'", "'tenant.administrator'"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 87 missing %q", fragment)
		}
	}
}

func TestAssetCatalogReferenceResolutionMigrationUsesSingleCatalogPermission(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000083_iam_asset_catalog_reference_resolution.up.sql")
	if err != nil {
		t.Fatalf("read migration 83: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'catalog.reference.read'", "'tenant.asset_runtime'", "'tenant.administrator'",
		"'develop.task.read'", "'meta.catalog.read'", "'service.definition.read'", "'standard.metric.read'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 83 missing %q", fragment)
		}
	}
}

func TestCatalogReferenceResolutionMigrationPublishesTenantPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000082_iam_catalog_reference_resolution.up.sql")
	if err != nil {
		t.Fatalf("read migration 82: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'iam.department.read'", "'iam.tenant_membership.read'", "'tenant.catalog_runtime'",
		"'catalog.entry.update'", "'catalog.entry.certify'", "'catalog.entry.deprecate'",
		"'catalog.audit.read'", "'catalog.source.rebind'",
		"'tenant.administrator'", "'platform.system_administrator'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 82 missing %q", fragment)
		}
	}
}

func TestCatalogCollaborationMigrationPublishesScopedPermissions(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000094_iam_catalog_collaboration.up.sql")
	if err != nil {
		t.Fatalf("read migration 94: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'catalog.collection.read'", "'catalog.collection.update'", "'catalog.entry.read'",
		"'project_group'", "'tenant.administrator'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 94 missing %q", fragment)
		}
	}
}

func TestModelCatalogReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000095_iam_model_catalog_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 95: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'model.catalog.read'", "'tenant.catalog_runtime'", "ARRAY['tenant']::text[]"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 95 missing %q", fragment)
		}
	}
}

func TestStandardCatalogReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000096_iam_standard_catalog_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 96: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'standard.catalog.read'", "'tenant.catalog_runtime'", "ARRAY['tenant']::text[]"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 96 missing %q", fragment)
		}
	}
}

func TestServiceCatalogReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000097_iam_service_catalog_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 97: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'service.catalog.read'", "'tenant.catalog_runtime'", "ARRAY['tenant']::text[]"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 97 missing %q", fragment)
		}
	}
}

func TestDevelopCatalogReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000098_iam_develop_catalog_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 98: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'develop.catalog.read'", "'tenant.catalog_runtime'", "ARRAY['tenant']::text[]"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 98 missing %q", fragment)
		}
	}
}

func TestQualityCatalogReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000099_iam_quality_catalog_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 99: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'quality.catalog.read'", "'tenant.catalog_runtime'", "ARRAY['tenant']::text[]"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 99 missing %q", fragment)
		}
	}
}

func TestCatalogEngineDescriptorReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000101_iam_catalog_engine_descriptor_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 101: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'system.engine_descriptor.read'", "'tenant.catalog_runtime'", "ON CONFLICT (role_id, permission_id) DO NOTHING"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 101 missing %q", fragment)
		}
	}
}

func TestCatalogProjectGroupReadMigrationGrantsCatalogRuntime(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000102_iam_catalog_project_group_read.up.sql")
	if err != nil {
		t.Fatalf("read migration 102: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'iam.project_group.read'", "'tenant.catalog_runtime'", "ON CONFLICT (role_id, permission_id) DO NOTHING"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 102 missing %q", fragment)
		}
	}
}

func TestCatalogRuntimeMigrationPublishesIdentityAndLeastPrivilegeRoles(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000081_iam_catalog_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 81: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'addp-catalog'", "'platform.catalog_runtime'", "'tenant.catalog_runtime'",
		"'platform.tenant.read'", "'system.runtime_registry.update'", "'meta.catalog.read'",
		"'catalog.inventory.read'", "'catalog.entry.read'", "'catalog.source.rebind'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 81 missing %q", fragment)
		}
	}
}

func TestExecutionAudienceAndModelMaterializationMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000075_iam_execution_audience_model_materialization.up.sql")
	if err != nil {
		t.Fatalf("read migration 75: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"SET audience = 'quality'",
		"WHERE audience = 'addp-quality'",
		"CHECK (audience IN ('develop', 'duckdb', 'model', 'quality', 'service'))",
		"'model.materialization.execute'",
		"'tenant.data_architect'",
		"'tenant.model_runtime'",
		"'system.execution_authorization.execute'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 75 missing %q", fragment)
		}
	}
}

func TestModelTaskProviderMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000076_iam_model_task_provider.up.sql")
	if err != nil {
		t.Fatalf("read migration 76: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'model.task_provider.execute'",
		"'model.task_provider.read'",
		"'tenant.orchestrator_runtime'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 76 missing %q", fragment)
		}
	}
}

func TestModelMaterializationContextMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000077_iam_model_materialization_context.up.sql")
	if err != nil {
		t.Fatalf("read migration 77: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'model.materialization_context.read'", "'tenant.develop_runtime'"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 77 missing %q", fragment)
		}
	}
}

func TestDataArchitectManagedMaterializationWriteMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000078_iam_data_architect_managed_materialization_write.up.sql")
	if err != nil {
		t.Fatalf("read migration 78: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{"'tenant.data_architect'", "'develop.task.execute'", "'develop.data_write.execute'"} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 78 missing %q", fragment)
		}
	}
}

func TestModelMaterializationWriteAttemptMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000079_iam_model_materialization_write_attempt.up.sql")
	if err != nil {
		t.Fatalf("read migration 79: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'model.materialization_write.execute'", "'tenant.develop_runtime'", "'tenant.transfer_runtime'",
		"permission_key = 'model.materialization_context.read'", "status = 'disabled'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 79 missing %q", fragment)
		}
	}
}

func TestRemoveModelWriterCouplingMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000100_iam_remove_model_writer_coupling.up.sql")
	if err != nil {
		t.Fatalf("read migration 100: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'tenant.develop_runtime'", "'tenant.transfer_runtime'",
		"'model.materialization_read.execute'", "'model.materialization_write.execute'",
		"status = 'disabled'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 100 missing %q", fragment)
		}
	}
}

func TestTransferExecutionAuthorizationMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000080_iam_transfer_execution_authorization.up.sql")
	if err != nil {
		t.Fatalf("read migration 80: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'transfer'", "'tenant.transfer_runtime'", "'system.execution_authorization.execute'",
		"execution_authorizations_audience_check", "source_execution_attempt", "source_execution_lease_token",
		"uq_execution_authorizations_static_execution", "uq_execution_authorizations_execution_attempt",
		"execution authorization identity and boundary are immutable",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 80 missing %q", fragment)
		}
	}
}

func TestExecutionAuthorizationLeaseBoundaryMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000106_iam_execution_authorization_lease_boundary.up.sql")
	if err != nil {
		t.Fatalf("read migration 106: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"DROP CONSTRAINT execution_authorizations_execution_attempt_check",
		"source_execution_attempt > 0",
		"source_execution_lease_token IS NOT NULL",
		"source_type = 'user'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 106 missing %q", fragment)
		}
	}
	if strings.Contains(sql, "audience = 'transfer'") || strings.Contains(sql, "audience = 'develop'") {
		t.Fatal("migration 106 kept an audience-specific lease boundary")
	}
}

func TestModuleRuntimeInstanceProjectionIndexesMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000074_module_runtime_instance_projection_indexes.up.sql")
	if err != nil {
		t.Fatalf("read migration 74: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"idx_module_runtime_instances_definition_role_updated",
		"module_definition_id, role, updated_at DESC, id DESC",
		"idx_module_runtime_instances_definition_registered",
		"module_definition_id, registered_at DESC, id DESC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 74 missing %q", fragment)
		}
	}
}

func TestModuleRegistryRevisionMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000073_module_registry_revision.up.sql")
	if err != nil {
		t.Fatalf("read migration 73: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"CREATE TABLE system.module_registry_state",
		"revision bigint NOT NULL",
		"VALUES (1, 1)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 73 missing %q", fragment)
		}
	}
}

func TestTaskProviderBecomesModuleDeclaration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000072_task_provider_module_declaration.up.sql")
	if err != nil {
		t.Fatalf("read migration 72: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"ADD COLUMN task_provider jsonb",
		"every TaskProvider must belong to an existing module definition",
		"version = module.version + 1",
		"DROP TABLE system.task_providers",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 72 missing %q", fragment)
		}
	}
}

func TestModuleManagementControlPlaneMigrationPublishesVersionAndAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000071_module_management_control_plane.up.sql")
	if err != nil {
		t.Fatalf("read migration 71: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"ADD COLUMN version bigint NOT NULL DEFAULT 1",
		"'platform.module.read'",
		"'platform.module.update'",
		"'platform.system_administrator'",
		"authorization_version = principal.authorization_version + 1",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 71 missing %q", fragment)
		}
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

func TestRemoveStandardDimensionHierarchyPermissionMigration(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000121_iam_remove_standard_dimension_hierarchy.up.sql")
	if err != nil {
		t.Fatalf("read migration 121: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'standard.dimension_hierarchy.create'",
		"'standard.dimension_hierarchy.delete'",
		"'standard.dimension_hierarchy.read'",
		"'standard.dimension_hierarchy.update'",
		"DELETE FROM system.role_permissions",
		"SET status = 'disabled'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 121 missing %q", fragment)
		}
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
