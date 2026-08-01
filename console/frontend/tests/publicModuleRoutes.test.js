import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

const readRepoFile = path => readFileSync(new URL(`../../../${path}`, import.meta.url), 'utf8')

describe('P1 module public route contracts', () => {
  it.each([
    ['graph', 'navigateGraphRoute', 'graph'],
    ['service', 'navigateServiceRoute', 'service'],
    ['standard', 'navigateStandardRoute', 'standard'],
    ['model', 'navigateModelRoute', 'modeling'],
    ['quality', 'navigateQualityRoute', 'quality'],
    ['asset', 'navigateAssetRoute', 'asset']
  ])('%s delegates module navigation to the Console bridge', (directory, helper, moduleName) => {
    const source = readRepoFile(`${directory}/frontend/src/utils/moduleNavigation.js`)
    expect(source).toContain(`export function ${helper}`)
    expect(source).toContain(`navigateConsoleModuleRoute(router, '${moduleName}', location, options)`)
  })

  it('keeps module router paths local and uses one execution parameter name', () => {
    for (const [directory, consolePrefix] of [
      ['standard', 'standard'],
      ['model', 'modeling'],
      ['quality', 'quality'],
      ['asset', 'asset']
    ]) {
      const source = readRepoFile(`${directory}/frontend/src/router/index.js`)
      expect(source).not.toMatch(new RegExp(`path: ['\"]/?${consolePrefix}/`))
    }

    const qualityRouter = readRepoFile('quality/frontend/src/router/index.js')
    expect(qualityRouter).toContain("path: 'executions/:execution_id'")
    expect(qualityRouter).not.toContain("path: 'executions/:id'")
  })

  it('uses the shared canonical resolver for stable module tabs', () => {
    const ontologyDetail = readRepoFile('graph/frontend/src/views/OntologyDetail.vue')
    expect(ontologyDetail).toContain('resolveCanonicalTabRouteState')
    expect(ontologyDetail).toContain('watch(() => route.query, restoreTabFromRoute)')

    const reviewQueue = readRepoFile('graph/frontend/src/views/ReviewQueue.vue')
    expect(reviewQueue).toContain('resolveCanonicalTabRouteState')
    expect(reviewQueue).toContain('watch(() => route.query, restoreTabFromRoute)')

    const knowledgeService = readRepoFile('graph/frontend/src/views/KnowledgeService.vue')
    const knowledgeRouteState = readRepoFile('graph/frontend/src/utils/knowledgeServiceRouteState.js')
    expect(knowledgeService).toContain('resolveKnowledgeServiceRouteState')
    expect(knowledgeService).toContain('await applyGraphSelection(routeState.graphId)')
    expect(knowledgeRouteState).toContain('resolveCanonicalTabRouteState')

    const serviceCatalog = readRepoFile('service/frontend/src/views/ServiceCatalog.vue')
    expect(serviceCatalog).toContain('resolveCanonicalTabRouteState')
    expect(serviceCatalog).toContain('watch(() => route.query, restoreTabFromRoute)')

    const entityDetail = readRepoFile('model/frontend/src/views/EntityDetail.vue')
    expect(entityDetail).toContain('resolveCanonicalTabRouteState')
    expect(entityDetail).toContain('watch(() => route.query, restoreTabFromRoute)')

    const applications = readRepoFile('asset/frontend/src/views/ApplicationList.vue')
    expect(applications).toContain('resolveCanonicalTabRouteState')
    expect(applications).toContain('watch(() => route.query, restoreTabFromRoute)')
  })

  it('persists stable Model and Asset object selections in canonical query parameters', () => {

    const starSchema = readRepoFile('model/frontend/src/views/StarSchemaView.vue')
    expect(starSchema).toContain('route.query.table_id')
    expect(starSchema).toContain("query: { table_id: tableId }")

    const assetManager = readRepoFile('asset/frontend/src/views/AssetManager.vue')
    expect(assetManager).toContain('route.query.catalog_id')
    expect(assetManager).toContain("{ catalog_id: String(id) }")
  })

  it('exposes only Asset workflows implemented by its backend contract', () => {
    const router = readRepoFile('asset/frontend/src/router/index.js')
    expect(router).toContain("path: 'assets/:id'")
    expect(router).toContain("path: 'assets/:id/edit'")
    expect(router).not.toContain("path: 'assets/create'")

    const detail = readRepoFile('asset/frontend/src/views/AssetDetail.vue')
    expect(detail).not.toMatch(/assetAPI\.(create|submit|approve|reject|republish)/)

    const api = readRepoFile('asset/frontend/src/api/asset.js')
    expect(api).not.toContain("client.post('/asset/assets', data)")
  })
})

describe('P2 module public route contracts', () => {
  it.each([
    ['agent', 'navigateAgentRoute', 'agent'],
    ['meta', 'navigateMetaRoute', 'meta'],
    ['system', 'navigateSystemRoute', 'system']
  ])('%s delegates module navigation to the Console bridge', (directory, helper, moduleName) => {
    const source = readRepoFile(`${directory}/frontend/src/utils/moduleNavigation.js`)
    expect(source).toContain(`export function ${helper}`)
    expect(source).toContain(`navigateConsoleModuleRoute(router, '${moduleName}', location, options)`)
  })

  it('uses a canonical Agent session path and route-driven session loading', () => {
    const router = readRepoFile('agent/frontend/src/router/index.js')
    expect(router).toContain("path: '/sessions/:session_id'")
    expect(router).not.toContain("path: '/agent'")

    const chat = readRepoFile('agent/frontend/src/views/ChatView.vue')
    const routeState = readRepoFile('agent/frontend/src/utils/routeState.js')
    expect(chat).toContain('route.params.session_id')
    expect(chat).toContain('resolveAgentSessionRouteState')
    expect(routeState).toContain("name: 'ChatSession'")
    expect(chat).toContain("name: 'ChatSession'")
    expect(chat).toContain("{ history: 'replace' }")

    const login = readRepoFile('agent/frontend/src/views/Login.vue')
    expect(login).toContain('route.query.redirect')
    expect(login).toContain("!route.query.redirect.startsWith('//')")
  })

  it('uses Meta engine ownership and removes the old module-prefix redirect', () => {
    const router = readRepoFile('meta/frontend/src/router/index.js')
    expect(router).not.toContain('normalizeRedirect')
    expect(router).not.toContain("startsWith('/meta/')")

    const scan = readRepoFile('meta/frontend/src/views/MetadataScan.vue')
    const routeState = readRepoFile('meta/frontend/src/utils/routeState.js')
    expect(scan).toContain('resolveMetadataScanRouteState')
    expect(routeState).toContain('routeQuery.engine_id')
    expect(routeState).toContain('routeQuery.task_id')
    expect(routeState).toContain('{ engine_id: engineId, task_id: taskId }')
  })

  it('restores System IAM tabs and engine details without the removed Logs route', () => {
    const router = readRepoFile('system/frontend/src/router/index.js')
    expect(router).toContain("path: 'engines/:id'")

    const iam = readRepoFile('system/frontend/src/views/IAMWorkbench.vue')
    const routeState = readRepoFile('system/frontend/src/utils/routeState.js')
    expect(iam).toContain('resolveIAMRouteState')
    expect(routeState).toContain('routeQuery.tab')
    expect(routeState).toContain("new Set(['platform-audit', 'tenant-audit'])")

    const engines = readRepoFile('system/frontend/src/views/Engines.vue')
    expect(engines).toContain('route.params.id')
    expect(engines).toContain("name: 'EngineDetail'")

    const cleanup = readRepoFile('system/frontend/src/views/CleanupManager.vue')
    expect(cleanup).not.toContain("name: 'Logs'")
    expect(cleanup).toContain("entity_type: 'cleanup'")
  })

  it('keeps Portal search and catalog pagination recoverable with a safe detail return', () => {
    const search = readRepoFile('portal/frontend/src/views/Search.vue')
    const routeState = readRepoFile('portal/frontend/src/utils/routeState.js')
    expect(search).toContain('resolveSearchRouteState')
    expect(routeState).toContain('routeQuery.type_id')
    expect(routeState).toContain('routeQuery.page')
    expect(search).toContain("router.replace({ name: 'Search'")

    const catalog = readRepoFile('portal/frontend/src/views/Catalog.vue')
    expect(catalog).toContain('resolveCatalogRouteState')
    expect(catalog).toContain("name: 'Catalog'")

    const detail = readRepoFile('portal/frontend/src/views/AssetDetail.vue')
    expect(detail).toContain('assetDetailReturnTarget')
    expect(routeState).toContain("previousRoute.startsWith('/portal/')")
    expect(detail).not.toContain('$router.back()')
  })
})
