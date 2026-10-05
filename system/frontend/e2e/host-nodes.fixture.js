export const HOST_NODE_ID = '10000000-0000-4000-8000-000000000001'
export const HOST_NODE_OTHER_ID = '10000000-0000-4000-8000-000000000002'

export async function mockHostNodesAPI(page, { scope = 'platform', permissions = ['platform.host_node.read', 'platform.host_node.create', 'platform.host_node.update'], conflict = false, detailFailure = 0, language = 'zh-cn' } = {}) {
  const calls = [], writes = []
  let node = { node_id: HOST_NODE_ID, display_name: 'Node A', node_kind: 'virtual', addresses: ['node-a.local'], enabled: true,
    allowed_module_bindings: [{ module_name: 'manager', client_id: 'addp-manager' }], version: 7,
    created_at: '2026-10-05T00:00:00Z', updated_at: '2026-10-05T00:00:00Z' }
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/system/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname
    const headers = { 'access-control-allow-origin': request.headers().origin || 'http://127.0.0.1:4173', 'access-control-allow-credentials': 'true',
      'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,OPTIONS' }
    const reply = (json, status = 200) => route.fulfill({ json, status, headers })
    if (request.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    if (path.endsWith('/refresh')) return reply({ access_token: 'node-fixture-token', expires_in: 3600 })
    if (path.endsWith('/users/me')) return reply({ id: '1', display_name: 'Node administrator' })
    if (path.endsWith('/auth/context')) return reply({ principal: { id: '1', principal_type: 'user' }, context: { type: scope, ...(scope === 'tenant' ? { tenant_id: '3', tenant_membership_id: '4' } : {}) },
      authorization: { role_assignments: [{ scope: { type: scope, ...(scope === 'tenant' ? { tenant_id: '3' } : {}) }, permissions }] } })
    if (path.includes('/platform/host_nodes')) {
      calls.push({ method: request.method(), path, query: Object.fromEntries(url.searchParams) })
      if (request.method() === 'POST') {
        const body = request.postDataJSON(); writes.push(body)
        node = { ...node, ...body, version: 1 }; return reply(node, 201)
      }
      if (request.method() === 'PUT') {
        const body = request.postDataJSON(); writes.push(body)
        if (conflict) return reply({ error: 'Version conflict', error_code: 'resource_version_conflict' }, 409)
        node = { ...node, ...body, version: body.version + 1 }; return reply(node)
      }
      if (path.endsWith('/host_nodes')) {
        const pageNumber = Number(url.searchParams.get('page'))
        return reply({ data: [{ ...node, version: 2 }, { ...node, node_id: HOST_NODE_OTHER_ID, display_name: 'Node B' }], total: 45, page: pageNumber, page_size: Number(url.searchParams.get('page_size')), total_pages: 3 })
      }
      if (detailFailure) return reply({ error: 'Read failed' }, detailFailure)
      if (path.endsWith(HOST_NODE_OTHER_ID)) return reply({ ...node, node_id: HOST_NODE_OTHER_ID, display_name: 'Node B' })
      return reply(node)
    }
    if (path.endsWith('/platform/modules')) return reply({ modules: [{ id: 1, module_name: 'manager', enabled: true, version: 1, instances: [] }] })
    if (path.endsWith('/platform/module-instances')) return reply({ data: ['bound', 'rejected', 'unbound'].map((state, i) => ({ instance_id: `binding-${state}`, module_name: 'manager', role: 'backend',
      status: 'up', lease_expires_at: '2099-01-01T00:00:00Z', host_node_name: 'legacy-display-only', node_id: state === 'bound' ? HOST_NODE_ID : '',
      declared_node_id: state === 'unbound' ? '' : HOST_NODE_ID, node_binding_state: state, node_binding_reason: state === 'rejected' ? 'node_disabled' : '', id: i + 1 })), total: 3, page: 1, page_size: 10 })
    throw new Error(`Unexpected node fixture request: ${request.method()} ${path}`)
  })
  return { calls, writes }
}
