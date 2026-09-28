// Console page entry requirements. Each module still enforces the corresponding
// action and resource decision on its own routes and APIs.
const tenant = (...permissions) => ({ context: 'tenant', permissions, permissionMode: 'all' })
const platform = (...permissions) => ({ context: 'platform', permissions, permissionMode: 'all' })
const anyOf = (context, ...permissions) => ({ context, permissions })

export const CONSOLE_ROUTE_ACCESS = {
  '/ontology/ontologies': [tenant('ontology.revision.read')],
  '/transfer/tasks': [tenant('transfer.task.read')],
  '/transfer/tasks/create': [tenant('transfer.task.create', 'meta.catalog.read')],
  '/meta/scan': [tenant('meta.catalog.read', 'meta.scan_task.read', 'system.engine_catalog.read')],
  '/manager/data-explorer': [tenant('manager.content.read', 'manager.data_item.read')],
  '/manager/data-retrieval': [tenant('manager.search.execute', 'manager.data_item.read')],
  '/manager/tasks/quick-view': [tenant('manager.derived_artifact.read', 'manager.data_item.read')],
  '/manager/tasks/spatial': [tenant('manager.derived_artifact.read', 'manager.data_item.read')],
  '/manager/tasks/embedding': [tenant('manager.derived_artifact.read', 'manager.data_item.read', 'inference.model_label.read')],
  '/standard/domains': [tenant('standard.domain.read')],
  '/standard/glossaries': [tenant('standard.glossary.read', 'standard.domain.read')],
  '/standard/elements': [tenant('standard.element.read', 'standard.domain.read')],
  '/standard/code-sets': [tenant('standard.code_set.read', 'standard.domain.read')],
  '/standard/units': [tenant('standard.unit.read')],
  '/standard/metrics': [tenant('standard.metric.read', 'standard.domain.read', 'standard.unit.read')],
  '/standard/documents': [tenant('standard.document.read', 'standard.domain.read')],
  '/modeling/entities': [tenant('model.entity.read', 'standard.domain.read')],
  '/modeling/er-diagram': [tenant('model.entity.read', 'model.entity_relation.read', 'standard.domain.read')],
  '/modeling/dw-layers': [tenant('model.dw_layer.read')],
  '/modeling/logical-tables': [tenant('model.logical_model.read', 'model.dw_layer.read', 'standard.domain.read')],
  '/modeling/metric-implementations': [tenant('model.metric_implementation.read', 'model.logical_model.read', 'standard.metric.read', 'standard.domain.read')],
  '/modeling/star-schema': [tenant('model.logical_model.read', 'standard.domain.read', 'standard.metric.read')],
  '/quality/overview': [tenant('quality.plan.read', 'quality.issue.read', 'monitor.execution.read')],
  '/quality/rules': [tenant('quality.rule.read')],
  '/quality/plans': [tenant('quality.plan.read')],
  '/quality/executions': [tenant('monitor.execution.read')],
  '/quality/issues': [tenant('quality.issue.read')],
  '/security/classification-grading': [tenant('security.classification.read', 'security.grade.read')],
  '/security/sensitive-data-definitions': [tenant('security.sensitive_data_type.read', 'security.detector.read', 'security.protection_baseline.read', 'security.classification.read', 'security.grade.read')],
  '/security/protection-enrollments': [tenant('security.enrollment.read', 'meta.catalog.read')],
  '/develop/sql': [tenant('develop.task.read', 'meta.catalog.read')],
  '/develop/notebook': [tenant('develop.notebook.read')],
  '/develop/workflow': [tenant('develop.task.read', 'meta.catalog.read')],
  '/develop/tasks': [tenant('develop.task.read')],
  '/service/query-services': [tenant('service.definition.read')],
  '/service/tile': [tenant('service.definition.read')],
  '/service/graph-services': [tenant('service.definition.read')],
  '/service/services': [tenant('service.external_registration.read')],
  '/service/catalog': [anyOf('tenant', 'service.definition.read', 'service.external_registration.read')],
  '/workbench/applications': [tenant('workbench.data_application.read')],
  '/orchestrator/orchestrations': [tenant('orchestrator.workflow.read')],
  '/monitor/dashboard': [tenant('monitor.statistics.read')],
  '/monitor/executions': [tenant('monitor.execution.read')],
  '/monitor/alerts': [tenant('monitor.alert_rule.read', 'monitor.alert_incident.read')],
  '/monitor/notifications': [tenant('monitor.notification_destination.read', 'monitor.notification_delivery.read')],
  '/catalog/entries': [tenant('catalog.entry.read')],
  '/catalog/governance/coverage': [tenant('catalog.inventory.read', 'catalog.entry.read')],
  '/catalog/governance/tasks': [tenant('catalog.entry.read', 'catalog.entry.update')],
  '/catalog/me/entries': [tenant('catalog.entry.read')],
  '/catalog/collections': [tenant('catalog.collection.read', 'catalog.entry.read')],
  '/asset/type-definitions': [tenant('asset.management.read', 'asset.entry.read')],
  '/asset/categories': [tenant('asset.management.read', 'asset.category.read')],
  '/asset/assets': [tenant('asset.management.read', 'asset.entry.read', 'asset.category.read')],
  '/asset/applications': [tenant('asset.application.read', 'asset.management.read')],
  '/asset/dashboard': [tenant('asset.management.read', 'asset.entry.read', 'asset.application.read', 'asset.authorization.read', 'asset.rating.read')],
  '/agent': [tenant('agent.session.read')],
  '/portal/home': [tenant('asset.entry.read')],
  '/portal/search': [tenant('asset.entry.read')],
  '/portal/categories': [tenant('asset.category.read', 'asset.entry.read')],
  '/portal/assets': [tenant('asset.entry.read')],
  '/portal/my/applications': [tenant('asset.application.read')],
  '/graph/ontologies': [tenant('graph.ontology.read')],
  '/graph/graphs': [tenant('graph.graph.read')],
  '/graph/analysis': [tenant('graph.analysis.read', 'graph.graph.read')],
  '/graph/knowledge-service': [tenant('graph.graph.read')],
  '/inference/settings/models': [tenant('inference.provider.read', 'inference.deployment.read', 'inference.profile.read')],
  '/system/iam/organization': [platform('platform.tenant.read'), anyOf('tenant', 'iam.department.read', 'iam.project_group.read')],
  '/system/iam/accounts': [anyOf('platform', 'iam.user.read', 'iam.platform_identity_change.read'), anyOf('tenant', 'iam.tenant_membership.read', 'iam.tenant_invitation.read')],
  '/system/iam/roles': [anyOf('tenant', 'iam.tenant_role.read', 'iam.tenant_role_assignment.read')],
  '/system/iam/application-access': [anyOf('tenant', 'iam.service_account.read', 'iam.api_consumer.read', 'iam.oauth_client.read')],
  '/system/iam/security': [anyOf('platform', 'iam.security_policy.read', 'audit.event.read'), anyOf('tenant', 'audit.tenant_event.read')],
  '/system/modules': [platform('platform.module.read')],
  '/system/engines': [tenant('system.engine.read')],
  '/system/cleanup': [tenant('system.cleanup.read')],
  '/system/account/security': [{ context: 'any' }],
  '/configuration': [
    anyOf('platform', 'platform.configuration.read', 'develop.configuration.read', 'manager.configuration.read', 'monitor.configuration.read', 'service.configuration.read', 'transfer.configuration.read'),
    anyOf('tenant', 'develop.configuration.read'),
  ],
  '/manager/spatial-preview': [tenant('manager.content.read')],
  '/manager/settings/embedding': [platform('manager.configuration.read', 'inference.profile.read', 'inference.deployment.read')],
  '/service/published-services': [tenant('service.definition.read')],
  '/develop/executions': [tenant('develop.task.read')],
  '/develop/approvals': [tenant('develop.task.read')],
}

export function consoleRouteAccess(path) {
  if (!path) return null
  const pathname = path.split(/[?#]/, 1)[0].replace(/\/$/, '') || '/'
  if (pathname === '/transfer') return CONSOLE_ROUTE_ACCESS['/transfer/tasks']
  if (/^\/transfer\/tasks\/[^/]+\/edit$/.test(pathname)) {
    return [tenant('transfer.task.read', 'transfer.task.update', 'meta.catalog.read')]
  }
  if (/^\/transfer\/tasks\/[^/]+\/detail$/.test(pathname) ||
    /^\/transfer\/executions\/[^/]+$/.test(pathname)) {
    return CONSOLE_ROUTE_ACCESS['/transfer/tasks']
  }
  const editingRoutes = [
    [/^\/service\/(query-services|graph-services)\/create$/, 'service.definition.read', 'service.definition.create', 'system.engine_catalog.read'],
    [/^\/service\/(query-services|graph-services)\/[^/]+\/edit$/, 'service.definition.read', 'service.definition.update', 'system.engine_catalog.read'],
    [/^\/service\/(tile|published-services)\/create$/, 'service.definition.read', 'service.definition.create'],
    [/^\/service\/(tile|published-services)\/[^/]+\/edit$/, 'service.definition.read', 'service.definition.update'],
    [/^\/service\/services\/create$/, 'service.external_registration.read', 'service.external_registration.create'],
    [/^\/service\/services\/[^/]+\/edit$/, 'service.external_registration.read', 'service.external_registration.update'],
    [/^\/orchestrator\/orchestrations\/new$/, 'orchestrator.workflow.read', 'orchestrator.workflow.create'],
    [/^\/orchestrator\/orchestrations\/[^/]+\/edit$/, 'orchestrator.workflow.read', 'orchestrator.workflow.update'],
    [/^\/workbench\/applications\/new$/, 'workbench.data_application.read', 'workbench.data_application.create'],
    [/^\/workbench\/applications\/[^/]+$/, 'workbench.data_application.read', 'workbench.data_application.update'],
    [/^\/asset\/assets\/new$/, 'asset.management.read', 'asset.entry.read', 'asset.category.read', 'asset.entry.update'],
    [/^\/asset\/assets\/[^/]+\/edit$/, 'asset.management.read', 'asset.entry.read', 'asset.category.read', 'asset.entry.update'],
    [/^\/graph\/ontologies\/create$/, 'graph.ontology.read', 'graph.ontology.create'],
    [/^\/graph\/ontologies\/[^/]+\/edit$/, 'graph.ontology.read', 'graph.ontology.update'],
  ]
  const editing = editingRoutes.find(([pattern]) => pattern.test(pathname))
  if (editing) return [tenant(...editing.slice(1))]
  if (/^\/graph\/graphs\/[^/]+\/review$/.test(pathname)) return [tenant('graph.graph.read', 'graph.review.read')]
  if (/^\/graph\/graphs\/[^/]+\/build/.test(pathname)) return [tenant('graph.graph.read', 'graph.build_task.read')]
  if (pathname === '/ontology/ontologies/new') return [tenant('ontology.revision.update')]
  if (/^\/ontology\/ontologies\/[^/]+\/trial$/.test(pathname)) return [tenant('ontology.semantic.read')]
  if (CONSOLE_ROUTE_ACCESS[pathname]) return CONSOLE_ROUTE_ACCESS[pathname]
  const parent = Object.keys(CONSOLE_ROUTE_ACCESS)
    .filter(route => pathname.startsWith(`${route}/`))
    .sort((a, b) => b.length - a.length)[0]
  return parent ? CONSOLE_ROUTE_ACCESS[parent] : null
}

export function allowsConsoleRoute(path, contextType, grantedPermissions = []) {
  const rules = consoleRouteAccess(path)
  if (!rules) return false
  const granted = grantedPermissions instanceof Set ? grantedPermissions : new Set(grantedPermissions)
  return rules.some(rule => {
    if (rule.context !== 'any' && rule.context !== contextType) return false
    if (!rule.permissions?.length) return true
    return rule.permissionMode === 'all'
      ? rule.permissions.every(permission => granted.has(permission))
      : rule.permissions.some(permission => granted.has(permission))
  })
}

export function resolveModuleLandingRoute(modulePrefix, modulePaths, contextType, grantedPermissions = []) {
  return modulePaths.find(path =>
    allowsConsoleRoute(`${modulePrefix}${path}`, contextType, grantedPermissions)
  ) || '/forbidden'
}
