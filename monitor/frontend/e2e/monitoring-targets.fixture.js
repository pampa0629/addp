export const id = '1621bb62-cb0e-40e1-b988-0d4d36683e51'
export const node = 'f83e9874-2164-4411-a5c0-650d143006cf'
export const permissions = ['platform.host_node.read', ...['read', 'create', 'update', 'delete'].map(action => `monitor.monitoring_target.${action}`)]
export function identity(keys = permissions, context = { type: 'platform' }, principal = 'user', delegation = null) {
  return { principal: { id: 'fixture-user', type: principal }, context, delegation, authorization: { role_assignments: [{ scope: context, permissions: keys }] } }
}
export async function backend(page, options = {}) {
  let target = { id, version: 1, subject: { kind: 'node', node_id: node }, monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://node.test:9443/metrics' }, enabled: true }
  const writes = [], reads = []
  let conflict = options.conflict, release = null
  const held = options.holdList ? new Promise(resolve => { release = resolve }) : null
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), options.locale || 'zh-cn')
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    let body, status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = options.identity || identity()
    else if (path.startsWith('/api/v1/system/platform/host_nodes')) {
      const host = { node_id: node, display_name: '测试主机', addresses: ['node.test'], enabled: true, version: 1 }
      body = path.endsWith('/host_nodes') ? { data: [host], total: 1, page: 1, page_size: 20 } : host
      if (options.hostUnavailable) { status = 503; body = { error: 'host unavailable' } }
    }
    else if (path.startsWith('/api/v1/monitor/platform/monitoring_targets')) {
      if (req.method() === 'GET') {
        reads.push(path)
        if (path.endsWith('/monitoring_targets')) {
          if (held) await held
          body = { data: target ? [target] : [], total: target ? options.total || 1 : 0, page: Number(url.searchParams.get('page')), page_size: Number(url.searchParams.get('page_size')), total_pages: 1 }
          if (options.unavailable) { status = 503; body = { error: 'unavailable' } }
        } else body = target
      } else {
        const input = req.postDataJSON(); writes.push({ method: req.method(), input })
        if (conflict) { conflict = false; status = 409; body = { error_code: 'resource_version_conflict' }; target = { ...target, version: 2 } }
        else if (req.method() === 'DELETE') { target = null; status = 204 }
        else { target = { ...input, id, version: req.method() === 'POST' ? 1 : input.version + 1 }; body = target; if (req.method() === 'POST') status = 201 }
      }
    } else throw new Error(`Unexpected target fixture API ${path}`)
    await route.fulfill({ status, contentType: 'application/json', ...(status === 204 ? {} : { body: JSON.stringify(body) }) })
  })
  return { writes, reads, release: () => release?.() }
}
export async function setIdentity(page, value) {
  await page.evaluate(next => { document.querySelector('#app').__vue_app__.config.globalProperties.$pinia._s.get('monitor-auth').authContext = next }, value)
}
