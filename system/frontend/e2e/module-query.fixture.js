export async function mockModuleQueryAPI(page, { logPermission = false, pipelinePermission = false, pipelineManagement = true, now = Date.now } = {}) {
  const queries = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const headers = {
      'access-control-allow-origin': request.headers().origin || 'http://127.0.0.1:4173',
      'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type',
      'access-control-allow-methods': 'GET,POST,OPTIONS'
    }
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    const reply = body => route.fulfill({ json: body, headers })
    if (url.pathname.endsWith('/refresh')) return reply({ access_token: 'module-query-fixture', expires_in: 3600 })
    if (url.pathname.endsWith('/users/me')) return reply({ id: '1', display_name: 'Module reader' })
    if (url.pathname.endsWith('/auth/context')) return reply({
      principal: { id: '1', principal_type: 'user' }, context: { type: 'platform' },
      authorization: { role_assignments: [{ scope: { type: 'platform' }, permissions: ['platform.module.read', ...(logPermission ? ['platform.module_log.read'] : []), ...(pipelinePermission ? ['monitor.log_pipeline.read', 'monitor.log_notification.read', ...(pipelineManagement ? ['monitor.log_pipeline.update', 'monitor.log_notification.update'] : [])] : [])] }] }
    })
    if (url.pathname.endsWith('/platform/modules')) return reply({ modules: [
      { id: 1, module_name: 'manager', route_prefix: '/manager', enabled: true, version: 1, instances: [] }
    ] })
    if (url.pathname.endsWith('/platform/module-instances')) {
      const params = Object.fromEntries(url.searchParams)
      queries.push(params)
      const pageNumber = Number(params.page)
      const pageSize = Number(params.page_size)
      const count = Math.max(0, Math.min(pageSize, 45 - (pageNumber - 1) * pageSize))
      const data = Array.from({ length: count }, (_, index) => ({
        id: (pageNumber - 1) * pageSize + index + 1, instance_id: `page-${pageNumber}-${index}`,
        module_name: params.module_name || 'manager', role: params.role || 'backend',
        module_url: `http://${params.registered_host || 'manager.local'}:8081`,
        host_node_name: params.node_name || 'host-a', status: params.status || 'up',
        stop_reason: params.stop_reason || '',
        lease_expires_at: new Date(now() + (params.status === 'down' ? -1000 : 60000)).toISOString(),
        process_started_at: new Date(now() - 65000).toISOString(),
        stopped_at: params.status === 'down' ? new Date(now() - 60000).toISOString() : null
      }))
      return reply({ data, total: 45, page: pageNumber, page_size: pageSize })
    }
    throw new Error(`Unexpected module query request: ${request.method()} ${url.pathname}`)
  })
  return queries
}

export const moduleQueryLink = '?tab=instances&module_name=manager&registered_host=manager.local&node_name=host-a&role=backend&status=all&time_basis=offline&time_period=custom&time_from=2026-10-01T00%3A00%3A00.000Z&time_to=2026-10-02T00%3A00%3A00.000Z&page=2&page_size=20'
