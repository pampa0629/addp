package authorization

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestRepositoryPermissionManifests(t *testing.T) {
	report, err := LoadRepositoryAuthorizationCatalog(testRepositoryRoot(t))
	if err != nil {
		t.Fatalf("LoadRepositoryAuthorizationCatalog() error = %v", err)
	}
	descriptors := report.Permissions
	if len(descriptors) != 393 {
		t.Fatalf("descriptor count = %d, want 393", len(descriptors))
	}
	if descriptors[0].Key != "agent.configuration.read" || descriptors[len(descriptors)-1].Key != "workbench.view.update" {
		t.Fatalf("descriptor boundary keys = %q, %q", descriptors[0].Key, descriptors[len(descriptors)-1].Key)
	}

	roles := report.Roles
	if len(roles) != 60 {
		t.Fatalf("role count = %d, want 60", len(roles))
	}
	if roles[0].Key != "platform.agent_runtime" || roles[len(roles)-1].Key != "tenant.transfer_runtime" {
		t.Fatalf("role boundary keys = %q, %q", roles[0].Key, roles[len(roles)-1].Key)
	}
	assertRepositoryRolePermissions(t, roles, "platform.inference_runtime", []string{"system.runtime_registry.update"})
	assertRepositoryRolePermissions(t, roles, "platform.catalog_runtime", []string{"platform.tenant.read", "system.runtime_registry.update"})
	assertRepositoryRolePermissions(t, roles, "tenant.catalog_runtime", []string{"develop.catalog.read", "iam.department.read", "iam.tenant_membership.read", "meta.catalog.read", "model.catalog.read", "quality.catalog.read", "service.catalog.read", "standard.catalog.read", "standard.domain.read", "standard.element.read", "standard.glossary.read", "workbench.catalog.read"})
	assertRepositoryRolePermissions(t, roles, "platform.duckdb_runtime", []string{"system.runtime_registry.update"})
	assertRepositoryRolePermissions(t, roles, "tenant.agent_runtime", []string{"inference.runtime.execute", "system.engine_descriptor.read"})
	assertRepositoryRolePermissions(t, roles, "tenant.copilot_runtime", []string{"develop.task.read", "inference.runtime.execute", "system.engine_descriptor.read"})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.data_architect", []string{"user"})
	assertRepositoryRoleScopes(t, roles, "tenant.data_architect", []string{"tenant"})
	assertRepositoryRolePermissions(t, roles, "tenant.data_architect", []string{
		"develop.data_ddl.execute",
		"develop.data_read.execute",
		"develop.data_write.execute",
		"develop.task.execute",
		"model.dw_layer.create",
		"model.dw_layer.delete",
		"model.dw_layer.read",
		"model.dw_layer.update",
		"model.entity.approve",
		"model.entity.create",
		"model.entity.delete",
		"model.entity.read",
		"model.entity.update",
		"model.entity_relation.create",
		"model.entity_relation.delete",
		"model.entity_relation.read",
		"model.entity_relation.update",
		"model.logical_model.create",
		"model.logical_model.delete",
		"model.logical_model.read",
		"model.logical_model.update",
		"model.materialization.execute",
		"model.materialization_group.create",
		"model.materialization_group.delete",
		"model.materialization_group.read",
		"model.materialization_group.update",
		"standard.dimension_hierarchy.read",
		"standard.domain.read",
		"standard.element.read",
		"standard.metric.read",
		"system.execution_authorization.create",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.manager_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "tenant.manager_runtime", []string{
		"inference.runtime.execute",
		"meta.catalog.read",
		"meta.scan_task.execute",
		"system.engine.read",
		"system.engine_descriptor.read",
		"transfer.task.create",
		"transfer.task.execute",
		"transfer.task.read",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.meta_runtime", []string{
		"audit.tenant_event.create",
		"manager.content_index.update",
		"system.engine.read",
		"system.engine_descriptor.read",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.model_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "tenant.model_runtime", []string{
		"standard.dimension_hierarchy.read",
		"standard.domain.read",
		"standard.element.read",
		"standard.metric.read",
		"system.engine_descriptor.read",
		"system.execution_authorization.execute",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.orchestrator_runtime", []string{
		"develop.task_provider.execute",
		"develop.task_provider.read",
		"model.task_provider.execute",
		"model.task_provider.read",
		"quality.task_provider.execute",
		"quality.task_provider.read",
		"system.task_authorization.execute",
		"transfer.task_provider.execute",
		"transfer.task_provider.read",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.standard_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "tenant.standard_runtime", []string{
		"model.standard_reference.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.transfer_runtime", []string{
		"meta.catalog.read",
		"meta.inspect.execute",
		"meta.scan_task.execute",
		"system.engine.read",
		"system.engine_descriptor.read",
		"system.execution_authorization.execute",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.graph_runtime", []string{
		"copilot.knowledge_graph.execute",
		"model.entity.read",
		"model.entity_relation.read",
		"system.engine.read",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.geopython_runtime", []string{"manager.derived_artifact.create"})
	assertRepositoryRolePermissions(t, roles, "tenant.model3d_runtime", []string{"manager.derived_artifact.create"})
	assertRepositoryRolePermissions(t, roles, "tenant.pointcloud_runtime", []string{"manager.derived_artifact.create"})
	assertRepositoryRolePermissions(t, roles, "tenant.spark_runtime", []string{"system.engine.read"})
	assertRepositoryRolePermissions(t, roles, "tenant.monitor_runtime", []string{
		"audit.tenant_event.create",
		"meta.scan_task.read",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.quality_runtime", []string{
		"meta.catalog.read",
		"model.materialization_group.read",
		"model.materialization_read.execute",
		"standard.element.read",
		"system.engine.read",
		"system.execution_authorization.execute",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "platform.manager_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "platform.manager_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.monitor_runtime", []string{
		"system.runtime_registry.read",
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.gateway_runtime", []string{
		"system.api_key.read",
		"system.runtime_registry.read",
	})
	assertRepositoryRolePermissions(t, roles, "platform.model_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.quality_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.service_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.standard_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.transfer_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "platform.workbench_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.asset_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "tenant.asset_runtime", []string{
		"catalog.reference.read",
		"workbench.resource_grant.create",
		"workbench.resource_grant.revoke",
	})
	assertRepositoryRolePermissions(t, roles, "platform.develop_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "platform.portal_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "platform.portal_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.data_viewer", []string{
		"develop.data_read.execute",
		"manager.content.read",
		"manager.data_item.read",
		"manager.search.execute",
		"meta.catalog.read",
		"meta.lineage.read",
		"service.data_read.execute",
		"workbench.data_application.create",
		"workbench.data_application.delete",
		"workbench.data_application.execute",
		"workbench.data_application.publish",
		"workbench.data_application.read",
		"workbench.data_application.update",
		"workbench.view.create",
		"workbench.view.delete",
		"workbench.view.read",
		"workbench.view.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.ai_user", []string{
		"agent.run.cancel",
		"agent.run.create",
		"agent.run.execute",
		"agent.run.read",
		"agent.session.create",
		"agent.session.delete",
		"agent.session.read",
		"copilot.notebook.execute",
		"copilot.sql.execute",
		"copilot.transfer.execute",
		"copilot.workflow.execute",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.data_steward", []string{
		"develop.data_read.execute",
		"develop.data_write.execute",
		"manager.content.read",
		"manager.data_item.create",
		"manager.data_item.read",
		"manager.data_item.update",
		"manager.derived_artifact.create",
		"manager.derived_artifact.delete",
		"manager.derived_artifact.read",
		"manager.derived_artifact.update",
		"manager.search.execute",
		"meta.catalog.read",
		"meta.inspect.execute",
		"meta.lineage.read",
		"meta.scan_task.create",
		"meta.scan_task.delete",
		"meta.scan_task.execute",
		"meta.scan_task.read",
		"meta.scan_task.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.data_engineer", []string{
		"develop.data_read.execute",
		"develop.data_write.execute",
		"develop.notebook.create",
		"develop.notebook.delete",
		"develop.notebook.execute",
		"develop.notebook.read",
		"develop.notebook.update",
		"develop.task.create",
		"develop.task.delete",
		"develop.task.execute",
		"develop.task.read",
		"develop.task.update",
		"manager.content.read",
		"manager.data_item.read",
		"manager.data_profile.execute",
		"manager.search.execute",
		"monitor.execution.read",
		"orchestrator.workflow.create",
		"orchestrator.workflow.delete",
		"orchestrator.workflow.execute",
		"orchestrator.workflow.read",
		"orchestrator.workflow.update",
		"system.execution_authorization.create",
		"transfer.task.create",
		"transfer.task.delete",
		"transfer.task.execute",
		"transfer.task.read",
		"transfer.task.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.develop_runtime", []string{
		"meta.catalog.read",
		"meta.scan_task.execute",
		"system.engine_descriptor.read",
		"system.execution_authorization.execute",
		"system.notebook_session_authorization.execute",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.service_publisher", []string{
		"manager.content.read",
		"manager.data_item.read",
		"manager.search.execute",
		"meta.catalog.read",
		"service.data_read.execute",
		"service.definition.create",
		"service.definition.delete",
		"service.definition.read",
		"service.definition.update",
		"service.external_registration.create",
		"service.external_registration.delete",
		"service.external_registration.read",
		"service.external_registration.update",
		"system.execution_authorization.create",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.service_runtime", []string{
		"audit.tenant_event.create",
		"meta.catalog.read",
		"meta.lineage.create",
		"system.engine.read",
		"system.engine_descriptor.read",
		"system.execution_authorization.execute",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.asset_consumer", []string{
		"asset.application.create",
		"asset.application.read",
		"asset.authorization.read",
		"asset.category.read",
		"asset.entry.read",
		"asset.rating.create",
		"asset.rating.read",
		"asset.rating.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.asset_manager", []string{
		"asset.application.approve",
		"asset.application.create",
		"asset.application.read",
		"asset.application.reject",
		"asset.application.revoke",
		"asset.authorization.read",
		"asset.authorization.revoke",
		"asset.category.create",
		"asset.category.delete",
		"asset.category.read",
		"asset.category.update",
		"asset.entry.delete",
		"asset.entry.offline",
		"asset.entry.publish",
		"asset.entry.read",
		"asset.entry.update",
		"asset.management.read",
		"asset.rating.create",
		"asset.rating.read",
		"asset.rating.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.governance_manager", []string{
		"develop.data_read.execute",
		"meta.lineage.read",
		"monitor.execution.read",
		"quality.check_task.create",
		"quality.check_task.delete",
		"quality.check_task.execute",
		"quality.check_task.read",
		"quality.check_task.update",
		"quality.issue.read",
		"quality.issue.update",
		"quality.materialization_gate.create",
		"quality.materialization_gate.delete",
		"quality.materialization_gate.read",
		"quality.materialization_gate.update",
		"quality.rule_application.create",
		"quality.rule_application.delete",
		"quality.rule_application.read",
		"quality.rule_application.update",
		"standard.classification.create",
		"standard.classification.delete",
		"standard.classification.read",
		"standard.classification.update",
		"standard.code_set.create",
		"standard.code_set.delete",
		"standard.code_set.read",
		"standard.code_set.update",
		"standard.dimension_hierarchy.create",
		"standard.dimension_hierarchy.delete",
		"standard.dimension_hierarchy.read",
		"standard.dimension_hierarchy.update",
		"standard.document.create",
		"standard.document.delete",
		"standard.document.read",
		"standard.document.update",
		"standard.domain.create",
		"standard.domain.delete",
		"standard.domain.read",
		"standard.domain.update",
		"standard.element.approve",
		"standard.element.create",
		"standard.element.delete",
		"standard.element.read",
		"standard.element.update",
		"standard.glossary.approve",
		"standard.glossary.create",
		"standard.glossary.delete",
		"standard.glossary.offline",
		"standard.glossary.read",
		"standard.glossary.update",
		"standard.metric.approve",
		"standard.metric.create",
		"standard.metric.delete",
		"standard.metric.offline",
		"standard.metric.read",
		"standard.metric.update",
		"standard.unit.create",
		"standard.unit.delete",
		"standard.unit.read",
		"standard.unit.update",
		"system.execution_authorization.create",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.monitoring_operator", []string{
		"monitor.alert_incident.read",
		"monitor.alert_incident.update",
		"monitor.alert_rule.create",
		"monitor.alert_rule.delete",
		"monitor.alert_rule.read",
		"monitor.alert_rule.update",
		"monitor.execution.read",
		"monitor.health.read",
		"monitor.notification_delivery.read",
		"monitor.notification_delivery.retry",
		"monitor.notification_destination.create",
		"monitor.notification_destination.delete",
		"monitor.notification_destination.execute",
		"monitor.notification_destination.read",
		"monitor.notification_destination.update",
		"monitor.statistics.read",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.graph_engineer", []string{
		"graph.analysis.execute",
		"graph.analysis.read",
		"graph.build_task.cancel",
		"graph.build_task.create",
		"graph.build_task.delete",
		"graph.build_task.execute",
		"graph.build_task.read",
		"graph.build_task.update",
		"graph.graph.create",
		"graph.graph.delete",
		"graph.graph.read",
		"graph.graph.update",
		"graph.ontology.create",
		"graph.ontology.delete",
		"graph.ontology.read",
		"graph.ontology.update",
		"graph.review.approve",
		"graph.review.read",
		"graph.review.reject",
		"graph.review.update",
	})
}

func assertRepositoryRolePrincipalTypes(t *testing.T, roles []BuiltinRoleDescriptor, key string, want []string) {
	t.Helper()
	for _, role := range roles {
		if role.Key == key {
			if !reflect.DeepEqual(role.AllowedPrincipalTypes, want) {
				t.Fatalf("role %q principal types = %v, want %v", key, role.AllowedPrincipalTypes, want)
			}
			return
		}
	}
	t.Fatalf("role %q not found", key)
}

func assertRepositoryRoleScopes(t *testing.T, roles []BuiltinRoleDescriptor, key string, want []string) {
	t.Helper()
	for _, role := range roles {
		if role.Key == key {
			if !reflect.DeepEqual(role.AllowedScopeTypes, want) {
				t.Fatalf("role %q scopes = %v, want %v", key, role.AllowedScopeTypes, want)
			}
			return
		}
	}
	t.Fatalf("role %q not found", key)
}

func assertRepositoryRolePermissions(t *testing.T, roles []BuiltinRoleDescriptor, key string, want []string) {
	t.Helper()
	for _, role := range roles {
		if role.Key == key {
			if !reflect.DeepEqual(role.Permissions, want) {
				t.Fatalf("role %q permissions = %v, want %v", key, role.Permissions, want)
			}
			return
		}
	}
	t.Fatalf("role %q not found", key)
}

func testRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test file")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
}
