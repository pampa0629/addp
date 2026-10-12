import { identity } from './monitoring-targets.fixture'

export const permissions = ['platform.module.read', 'monitor.resource_observation.read']
const stamp = '2026-10-10T00:00:00Z'
export async function backend(page, options = {}) {
  const rows = [1, 2].map(id => ({ id, module_name: 'monitor', instance_id: `native-${id}`, role: 'worker', node_id: '', host_node_name: '', runtime_hostname: `mac-process-${id}`, status: 'up', process_metrics_declared: true }))
  if (options.workerTypes) rows.splice(0, rows.length, ...[
    ['meta', 'worker', 'scan'], ['transfer', 'worker', 'sync'], ['transfer', 'worker', 'continuous_sync'],
    ['transfer', 'worker', undefined], ['meta', 'worker', 'future_worker'], ['transfer', 'backend', 'continuous_sync']
  ].map(([module, role, runtime], index) => ({ ...rows[0], id: index + 1, module_name: module, role, instance_id: `typed-${index + 1}`, runtime_hostname: 'shared-host', metadata: runtime === undefined ? {} : { runtime_name: runtime } })))
  if (options.scopes) rows.unshift(...Array.from({ length: 24 }, (_, i) => ({ ...rows[0], id: i + 3, instance_id: `history-${i}`, runtime_hostname: `history-${i}`, status: 'down' })), { ...rows[0], id: 27, instance_id: 'not-connected', runtime_hostname: 'not-connected', process_metrics_declared: false })
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
      const filtered = rows.filter(row => (!url.searchParams.has('status') || row.status === url.searchParams.get('status')) && (!url.searchParams.has('process_metrics_declared') || String(row.process_metrics_declared) === url.searchParams.get('process_metrics_declared')))
      body = { data: options.scopes ? filtered.slice((pageNumber - 1) * 20, pageNumber * 20) : pageNumber === 1 ? filtered : [], page: pageNumber, page_size: 20, total: options.scopes || options.workerTypes ? filtered.length : 21 }
    } else if (path === '/api/v1/monitor/platform/process_resource_summaries') {
      calls.push(url.searchParams.get('instance_ids'))
      const requested = new Set(url.searchParams.get('instance_ids').split(',').map(Number))
      body = { data: rows.filter(row => requested.has(row.id)).map(row => ({ subject: { kind: 'module_instance', id: row.id, module_name: row.module_name, instance_id: row.instance_id, role: row.role }, node_id: '', policy_version: 1, lookback_seconds: 300, queried_at: stamp, collection: { state: 'collecting', sampled_at: stamp }, series: [
        ['process.cpu.core_equivalents', 'cores', 60, row.id === 1 ? 1.5 : 0], ['process.memory.resident_bytes', 'bytes', 0, 1024 ** 3], ['process.uptime_seconds', 'seconds', 0, 3600]
      ].map(([key, unit, window, value]) => ({ metric_key: key, unit, window_seconds: window, dimensions: {}, points: [{ evaluated_at: stamp, sampled_at: stamp, value, data_state: 'valid' }] })) })) }
      if (mode === 'warmup') Object.assign(body.data[0].series[0].points[0], { value: null, sampled_at: null, data_state: 'no_data' })
      if (mode === 'wrong-owner') body.data[0].subject.instance_id = 'foreign'
      for (const result of body.data) {
        const row = rows.find(row => row.id === result.subject.id)
        const state = row.status === 'down' ? 'not_active' : !row.process_metrics_declared ? 'not_connected' : mode === 'failed' && row.id === 2 ? 'failed' : 'collecting'
        result.collection = { state, sampled_at: ['collecting', 'failed'].includes(state) ? stamp : null }
        if (state !== 'collecting') for (const series of result.series) Object.assign(series.points[0], { value: null, sampled_at: null, data_state: state === 'failed' ? 'no_data' : state })
      }
      if (mode === 'unavailable' || mode === 'denied') { status = mode === 'denied' ? 403 : 503; body = { error_code: mode === 'denied' ? 'permission_denied' : 'observability_backend_unavailable' } }
      if (held) await held
    } else throw new Error(`unexpected process fixture API ${path}`)
    await route.fulfill({ status, json: body })
  })
  return { calls, listCalls, setMode: value => { mode = value }, setRuntimeName: (id, value) => { rows.find(row => row.id === id).metadata = { runtime_name: value } }, release: () => resolve?.() }
}
