import { describe, expect, it, vi } from 'vitest'
import { CONSOLE_ROUTE_ACCESS, consoleRouteAccess } from '@common-ui'
import { filterSidebarMenus, firstAccessibleModuleRoute, matchesNavigationAccess } from '../src/utils/navigationAccess'

describe('Console page access', () => {
  it('selects platform inspection without granting Tenant modeling or machine publication', async () => {
    vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
    try {
      const { SIDEBAR_MENUS } = await import('../src/config/portalConfig')
      const menus = { ontology: SIDEBAR_MENUS.ontology }
      const visible = filterSidebarMenus(menus, 'platform', ['ontology.platform_definition.read']).ontology
      expect(firstAccessibleModuleRoute(visible)).toBe('/ontology/platform/definitions')
      expect(visible.items).toHaveLength(1)
      expect(firstAccessibleModuleRoute(filterSidebarMenus(menus, 'tenant', ['ontology.revision.read']).ontology)).toBe('/ontology/ontologies')
      expect(filterSidebarMenus(menus, 'tenant', ['ontology.platform_definition.read']).ontology.items).toEqual([])
      expect(filterSidebarMenus(menus, 'platform', ['ontology.platform_definition.publish']).ontology.items).toEqual([])
    } finally { vi.unstubAllGlobals() }
  })
  it('offers Data Explorer to item readers without granting other Manager pages', async () => {
    vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
    try {
      const { SIDEBAR_MENUS } = await import('../src/config/portalConfig')
      const menus = { manager: SIDEBAR_MENUS.manager }
      const visible = filterSidebarMenus(menus, 'tenant', ['manager.data_item.read']).manager
      expect(visible.items.map(item => item.index)).toEqual(['/manager/data-explorer'])
      expect(firstAccessibleModuleRoute(visible)).toBe('/manager/data-explorer')
      expect(filterSidebarMenus(menus, 'platform', ['manager.data_item.read']).manager.items).toEqual([])
      expect(filterSidebarMenus(menus, 'tenant', ['manager.content.read']).manager.items).toEqual([])
    } finally {
      vi.unstubAllGlobals()
    }
  })
  it('isolates managed node pages to their platform read permission', () => {
    for (const path of ['/system/host-nodes', '/system/host-nodes/10000000-0000-4000-8000-000000000001']) {
      expect(matchesNavigationAccess({ route: path }, 'platform', ['platform.host_node.read'])).toBe(true)
      expect(matchesNavigationAccess({ route: path }, 'tenant', ['platform.host_node.read'])).toBe(false)
      expect(matchesNavigationAccess({ route: path }, 'platform', ['platform.module.read'])).toBe(false)
      expect(matchesNavigationAccess({ route: path }, 'platform', ['platform.host_node.create'])).toBe(false)
    }
  })
  it('has one rule for every navigable menu page', async () => {
    vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
    try {
      const { SIDEBAR_MENUS } = await import('../src/config/portalConfig')
      function check(item) {
        if (item.children) { item.children.forEach(check); return }
        expect(consoleRouteAccess(item.index), item.index).toBeTruthy()
      }
      for (const menu of Object.values(SIDEBAR_MENUS)) {
        if (menu.items) menu.items.forEach(check)
        else check(menu)
      }
      expect(Object.keys(CONSOLE_ROUTE_ACCESS).length).toBeGreaterThan(60)
    } finally {
      vi.unstubAllGlobals()
    }
  })
  it('keeps collection settings under platform advanced settings with both read permissions', async () => {
    vi.stubGlobal('window', { location: { origin: 'http://localhost' } })
    try {
      const { SIDEBAR_MENUS } = await import('../src/config/portalConfig')
      const menus = { monitor: SIDEBAR_MENUS.monitor }
      const permissions = ['monitor.monitoring_target.read', 'platform.host_node.read']
      const advanced = filterSidebarMenus(menus, 'platform', permissions).monitor.items
      expect(advanced).toHaveLength(1)
      expect(advanced[0].label).toBe('console.menus.monitor.advanced')
      expect(advanced[0].children.map(item => item.index)).toEqual(['/monitor/monitoring-targets'])
      expect(filterSidebarMenus(menus, 'tenant', permissions).monitor.items).toEqual([])
      for (const permission of permissions) {
        expect(filterSidebarMenus(menus, 'platform', [permission]).monitor.items).toEqual([])
      }
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('keeps create-only Transfer accounts on the create page', () => {
    const permissions = ['transfer.task.create', 'meta.catalog.read']
    expect(matchesNavigationAccess({ route: '/transfer/tasks/create' }, 'tenant', permissions)).toBe(true)
    expect(matchesNavigationAccess({ route: '/transfer/tasks' }, 'tenant', permissions)).toBe(false)
  })

  it('checks context and additional action permissions on direct addresses', () => {
    const read = ['transfer.task.read']
    expect(matchesNavigationAccess({ route: '/transfer/tasks/4/detail' }, 'tenant', read)).toBe(true)
    expect(matchesNavigationAccess({ route: '/transfer/tasks/4/edit' }, 'tenant', read)).toBe(false)
    expect(matchesNavigationAccess({ route: '/transfer/tasks/4/edit' }, 'tenant',
      [...read, 'transfer.task.update', 'meta.catalog.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/transfer/tasks' }, 'platform', read)).toBe(false)
    expect(matchesNavigationAccess({ route: '/monitor/executions?module=transfer' }, 'tenant',
      ['monitor.execution.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/monitor/dashboard' }, 'tenant',
      ['monitor.statistics.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/monitor/dashboard' }, 'tenant',
      ['monitor.health.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/service/query-services/create' }, 'tenant',
      ['service.definition.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/service/query-services/create' }, 'tenant',
      ['service.definition.read', 'service.definition.create'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/service/query-services/create' }, 'tenant',
      ['service.definition.read', 'service.definition.create', 'system.engine_catalog.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/meta/scan' }, 'tenant',
      ['meta.catalog.read', 'meta.scan_task.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/meta/scan' }, 'tenant',
      ['meta.catalog.read', 'meta.scan_task.read', 'system.engine_catalog.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/manager/settings/embedding' }, 'platform',
      ['manager.configuration.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/manager/settings/embedding' }, 'platform',
      ['manager.configuration.read', 'inference.profile.read', 'inference.deployment.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/configuration/monitor' }, 'tenant',
      ['develop.configuration.read', 'monitor.configuration.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/configuration/monitor' }, 'platform',
      ['develop.configuration.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/configuration/monitor' }, 'platform',
      ['monitor.configuration.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/configuration/agent' }, 'tenant',
      ['agent.configuration.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/configuration/agent' }, 'tenant',
      ['agent.configuration.read', 'inference.profile.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/configuration' }, 'tenant',
      ['agent.configuration.read', 'inference.profile.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/configuration' }, 'tenant',
      ['agent.configuration.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/configuration' }, 'platform',
      ['inference.provider.read', 'inference.deployment.read', 'inference.profile.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/inference/settings/models' }, 'platform',
      ['inference.provider.read', 'inference.deployment.read', 'inference.profile.read'])).toBe(true)
    expect(consoleRouteAccess('/configuration/unknown')).toBeNull()
    expect(matchesNavigationAccess({ route: '/asset/assets/5/edit' }, 'tenant',
      ['asset.management.read', 'asset.entry.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/asset/assets' }, 'tenant',
      ['asset.management.read', 'asset.entry.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/asset/assets' }, 'tenant',
      ['asset.management.read', 'asset.entry.read', 'asset.category.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/graph/analysis' }, 'tenant',
      ['graph.analysis.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/graph/analysis' }, 'tenant',
      ['graph.analysis.read', 'graph.graph.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/graph/graphs/5/build' }, 'tenant',
      ['graph.build_task.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/graph/graphs/5/build' }, 'tenant',
      ['graph.build_task.read', 'graph.graph.read'])).toBe(true)
    expect(matchesNavigationAccess({ route: '/graph/graphs/5/review' }, 'tenant',
      ['graph.review.read'])).toBe(false)
    expect(matchesNavigationAccess({ route: '/graph/graphs/5/review' }, 'tenant',
      ['graph.review.read', 'graph.graph.read'])).toBe(true)
  })

  it('requires the catalogs that business pages load immediately', () => {
    const cases = [
      ['/manager/data-explorer', ['manager.content.read'], 'manager.data_item.read'],
      ['/manager/data-retrieval', ['manager.search.execute'], 'manager.data_item.read'],
      ['/manager/tasks/quick-view', ['manager.derived_artifact.read'], 'manager.data_item.read'],
      ['/manager/tasks/embedding', ['manager.derived_artifact.read', 'manager.data_item.read'], 'inference.model_label.read'],
      ['/modeling/entities', ['model.entity.read'], 'standard.domain.read'],
      ['/modeling/logical-tables', ['model.logical_model.read', 'standard.domain.read'], 'model.dw_layer.read'],
      ['/modeling/metric-implementations', ['model.metric_implementation.read', 'model.logical_model.read', 'standard.domain.read'], 'standard.metric.read'],
      ['/develop/sql', ['develop.task.read'], 'meta.catalog.read'],
      ['/develop/workflow', ['develop.task.read'], 'meta.catalog.read'],
      ['/standard/glossaries', ['standard.glossary.read'], 'standard.domain.read'],
      ['/standard/metrics', ['standard.metric.read', 'standard.domain.read'], 'standard.unit.read'],
      ['/security/sensitive-data-definitions', ['security.sensitive_data_type.read', 'security.detector.read', 'security.protection_baseline.read', 'security.grade.read'], 'security.classification.read'],
      ['/security/protection-enrollments', ['security.enrollment.read'], 'meta.catalog.read'],
      ['/catalog/governance/coverage', ['catalog.inventory.read'], 'catalog.entry.read'],
      ['/catalog/governance/tasks', ['catalog.entry.update'], 'catalog.entry.read'],
      ['/catalog/collections', ['catalog.collection.read'], 'catalog.entry.read'],
      ['/asset/applications', ['asset.application.read'], 'asset.management.read'],
    ]
    for (const [route, given, dependency] of cases) {
      expect(matchesNavigationAccess({ route }, 'tenant', given), route).toBe(false)
      expect(matchesNavigationAccess({ route }, 'tenant', [...given, dependency]), route).toBe(true)
    }
  })
})


describe('System configuration access', () => {
  it('uses each original read permission and preserves context boundaries', () => {
    for (const route of ['/configuration', '/system/configuration']) {
      for (const scope of ['platform', 'tenant']) {
        expect(matchesNavigationAccess({ route }, scope, ['system.engine_raster_policy.read'])).toBe(true)
        expect(matchesNavigationAccess({ route }, scope, ['system.engine_raster_policy.update'])).toBe(false)
      }
      expect(matchesNavigationAccess({ route }, 'platform', ['iam.security_policy.read'])).toBe(true)
      expect(matchesNavigationAccess({ route }, 'tenant', ['iam.security_policy.read'])).toBe(false)
    }
    expect(consoleRouteAccess('/system/engine-raster-policies')).toBeNull()
  })
})
