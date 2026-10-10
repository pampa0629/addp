import { identity } from './monitoring-targets.fixture'

export const permissions = ['platform.module.read', 'monitor.resource_observation.read']
const stamp = '2026-10-10T00:00:00Z'
export async function backend(page, options = {}) {
  const rows = [1, 2].map(id => ({ id, module_name: 'monitor', instance_id: `native-${id}`, role: 'worker', node_id: '', host_node_name: '', runtime_hostname: `mac-process-${id}`, status: 'up', process_metrics_declared: true }))
  let mode = 'valid', resolve, calls = [], listCalls = []
  const held = options.held ? new Promise(yes => { resolve = yes }) : null
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), options.locale || 'zh-cn')
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    let body, status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = options.identity || identity(permissions)
    else if (path === '/api/v1/system/platform/module-instances') {
      listCalls.push(Object.fromEntries(url.searchParams))
      const pageNumber = Number(url.searchParams.get('page'))
      body = { data: pageNumber === 1 ? rows : [], page: pageNumber, page_size: 20, total: 21 }
    } else if (path === '/api/v1/monitor/platform/process_resource_summaries') {
      calls.push(url.searchParams.get('instance_ids'))
      body = { data: rows.map(row => ({ subject: { kind: 'module_instance', id: row.id, module_name: row.module_name, instance_id: row.instance_id, role: row.role }, node_id: '', policy_version: 1, lookback_seconds: 300, queried_at: stamp, collection: { state: 'collecting', sampled_at: stamp }, series: [
        ['process.cpu.core_equivalents', 'cores', 60, row.id === 1 ? 1.5 : 0], ['process.memory.resident_bytes', 'bytes', 0, 1024 ** 3], ['process.uptime_seconds', 'seconds', 0, 3600]
      ].map(([key, unit, window, value]) => ({ metric_key: key, unit, window_seconds: window, dimensions: {}, points: [{ evaluated_at: stamp, sampled_at: stamp, value, data_state: 'valid' }] })) })) }
      if (mode === 'warmup') Object.assign(body.data[0].series[0].points[0], { value: null, sampled_at: null, data_state: 'no_data' })
      if (mode === 'wrong-owner') body.data[0].subject.instance_id = 'foreign'
      if (mode === 'unavailable' || mode === 'denied') { status = mode === 'denied' ? 403 : 503; body = { error_code: mode === 'denied' ? 'permission_denied' : 'observability_backend_unavailable' } }
      if (held) await held
    } else throw new Error(`unexpected process fixture API ${path}`)
    await route.fulfill({ status, json: body })
  })
  return { calls, listCalls, setMode: value => { mode = value }, release: () => resolve?.() }
}

