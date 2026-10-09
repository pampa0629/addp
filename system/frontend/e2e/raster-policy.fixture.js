import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
export async function mockRaster(page, { scope = 'platform', writable = true, unavailable = false, conflict = false, empty = false, shrunk = false, permissions, language = 'zh-cn', engines = [{ id: 1, name: 'GeoPython' }], cacheByEngine = {} } = {}) {
  const writes = []
  let value = {
    policy: { engine_id: 1, version: 1, running: 2, waiting: 2, cache_mib: 256, default_tenant_running: 2, default_tenant_waiting: 2 },
    quota: { engine_id: 1, tenant_id: 3, version: 1, running: null, waiting: null }, effective_running: 2, effective_waiting: 2,
    runtime: { running: 0, waiting: 0, enabled: true, applied_version: 1, cache_mib: 256, effective_cpu: 2, memory_limit_bytes: 1073741824,
      observed_at: '2026-10-05T00:00:00Z', advice: { running: 1, waiting: 1, cache_mib: 32 } }
  }
  if (shrunk) { value.policy.running = 1; value.policy.default_tenant_running = 1; value.quota.running = 2; value.effective_running = 1 }
  const views = new Map(engines.map(engine => {
    const view = structuredClone(value)
    view.policy.engine_id = engine.id; view.quota.engine_id = engine.id
    view.policy.cache_mib = cacheByEngine[engine.id] ?? view.policy.cache_mib
    return [engine.id, view]
  }))
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), path = new URL(request.url()).pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || browserTestOrigin('system'), 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'raster-policy-fixture', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Raster administrator' })
    if (path.endsWith('/auth/context')) return reply({
      principal: { id: '1', principal_type: 'user' }, context: { type: scope, ...(scope === 'tenant' ? { tenant_id: '3', tenant_membership_id: '4' } : {}) },
      authorization: { role_assignments: [{ scope: { type: scope, ...(scope === 'tenant' ? { tenant_id: '3' } : {}) }, permissions:
        permissions || ['system.engine_raster_policy.read', ...(writable ? ['system.engine_raster_policy.update'] : [])] }] }
    })
    if (path.endsWith('/configuration-management/entries')) return reply([{ id: 'system.engine_raster_policy', owner_module: 'system', scope_types: ['platform_default_with_tenant_override'], frontend_route: '/system/configuration', read_permission: 'system.engine_raster_policy.read', update_permission: 'system.engine_raster_policy.update', available: true }])
    if (path.endsWith(`/system/${scope}/engine-raster-policies/engines`)) return reply(empty ? [] : engines)
    const engine = engines.find(engine => path.endsWith(`/system/${scope}/engine-raster-policies/${engine.id}`))
    if (engine) {
      let value = views.get(engine.id)
      if (request.method() === 'PUT') {
        const body = request.postDataJSON(); writes.push(body)
        if (conflict) return reply({ error: '配置版本冲突' }, 409)
        if (scope === 'platform') value.policy = { ...value.policy, ...body, version: body.version + 1 }
        else value.quota = { ...value.quota, ...body, version: body.version + 1 }
        return reply(value)
      }
      return unavailable ? reply({ error: '不可用' }, 503) : reply(value)
    }
    throw new Error(`Unexpected raster request: ${request.method()} ${path}`)
  })
  return writes
}
