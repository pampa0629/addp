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
	if len(descriptors) != 311 {
		t.Fatalf("descriptor count = %d, want 311", len(descriptors))
	}
	if descriptors[0].Key != "agent.run.cancel" || descriptors[len(descriptors)-1].Key != "transfer.task.update" {
		t.Fatalf("descriptor boundary keys = %q, %q", descriptors[0].Key, descriptors[len(descriptors)-1].Key)
	}

	roles := report.Roles
	if len(roles) != 30 {
		t.Fatalf("role count = %d, want 30", len(roles))
	}
	if roles[0].Key != "platform.asset_runtime" || roles[len(roles)-1].Key != "tenant.transfer_runtime" {
		t.Fatalf("role boundary keys = %q, %q", roles[0].Key, roles[len(roles)-1].Key)
	}
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.manager_runtime", []string{"service_principal"})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.asset_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "tenant.asset_runtime", []string{
		"develop.task.read",
		"meta.catalog.read",
		"service.definition.read",
		"standard.metric.read",
	})
	assertRepositoryRolePrincipalTypes(t, roles, "tenant.portal_runtime", []string{"service_principal"})
	assertRepositoryRolePermissions(t, roles, "tenant.portal_runtime", []string{
		"service.endpoint.read",
	})
	assertRepositoryRolePermissions(t, roles, "platform.develop_runtime", []string{
		"system.runtime_registry.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.data_viewer", []string{
		"develop.data_read.execute",
		"manager.content.read",
		"manager.data_item.read",
		"manager.search.execute",
		"meta.catalog.read",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.ai_user", []string{
		"agent.run.cancel",
		"agent.run.create",
		"agent.run.execute",
		"agent.run.read",
		"agent.session.create",
		"agent.session.delete",
		"agent.session.read",
		"copilot.sql.execute",
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
	})
	assertRepositoryRolePermissions(t, roles, "tenant.service_publisher", []string{
		"manager.content.read",
		"manager.data_item.read",
		"manager.search.execute",
		"meta.catalog.read",
		"service.definition.create",
		"service.definition.delete",
		"service.definition.read",
		"service.definition.update",
		"service.external_registration.create",
		"service.external_registration.delete",
		"service.external_registration.read",
		"service.external_registration.update",
	})
	assertRepositoryRolePermissions(t, roles, "tenant.asset_consumer", []string{
		"asset.application.create",
		"asset.application.read",
		"asset.authorization.read",
		"asset.catalog.read",
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
		"asset.catalog.create",
		"asset.catalog.delete",
		"asset.catalog.read",
		"asset.catalog.update",
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
		"monitor.execution.read",
		"quality.check_task.create",
		"quality.check_task.delete",
		"quality.check_task.execute",
		"quality.check_task.read",
		"quality.check_task.update",
		"quality.issue.read",
		"quality.issue.update",
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
