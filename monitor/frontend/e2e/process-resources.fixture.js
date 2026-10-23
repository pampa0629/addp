import { BlockList, isIP } from 'node:net'
import { identity } from './monitoring-targets.fixture'

export const permissions = ['platform.module.read', 'monitor.resource_observation.read']
export async function backend(page, options = {}) {
  let stamp = '2026-10-10T00:00:00Z'
  const rows = [1, 2].map(id => ({ id, module_name: 'monitor', instance_id: `native-${id}`, role: 'worker', node_id: '', host_node_name: '', host_node_ips: [], runtime_hostname: `mac-process-${id}`, status: 'up', process_metrics_declared: true, process_started_at: '2026-10-09T23:58:00Z' }))
  if (options.instances) rows.splice(0, rows.length, ...options.instances.map(row => ({ ...rows[0], instance_id: `native-${row.id}`, runtime_hostname: `mac-process-${row.id}`, ...row })))
  if (options.workerTypes) rows.splice(0, rows.length, ...[
    ['meta', 'worker', 'scan'], ['transfer', 'worker', 'sync'], ['transfer', 'worker', 'continuous_sync'],
    ['transfer', 'worker', undefined], ['meta', 'worker', 'future_worker'], ['transfer', 'backend', 'continuous_sync']
  ].map(([module, role, runtime], index) => ({ ...rows[0], id: index + 1, module_name: module, role, instance_id: `typed-${index + 1}`, runtime_hostname: 'shared-host', metadata: runtime === undefined ? {} : { runtime_name: runtime } })))
  if (options.scopes) rows.unshift(...Array.from({ length: 24 }, (_, i) => ({ ...rows[0], id: i + 3, instance_id: `history-${i}`, runtime_hostname: `history-${i}`, status: 'down' })), { ...rows[0], id: 27, instance_id: 'not-connected', runtime_hostname: 'not-connected', process_metrics_declared: false })
  let mode = 'valid', resolve, calls = [], listCalls = []
  let trendMode = 'valid', trendResolve
  const trendCalls = [], trendHeld = options.trendHeld ? new Promise(yes => { trendResolve = yes }) : null
  let held = options.held ? new Promise(yes => { resolve = yes }) : null
  const hostReads = []
  let releaseHosts
  const hostsHeld = options.holdHosts ? new Promise(yes => { releaseHosts = yes }) : null
  await page.addInitScript(lang => { if (!localStorage.getItem('addp-lang')) localStorage.setItem('addp-lang', lang) }, options.locale || 'zh-cn')
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    let body, status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = options.identity || identity(permissions)
    else if (path === '/api/v1/system/platform/module-instances') {
      listCalls.push(Object.fromEntries(url.searchParams))
      const nodeName = url.searchParams.get('node_name')?.trim().toLowerCase()
      const nodeIP = url.searchParams.get('node_ip')
      // System owns address validation/canonical matching. Node's IP primitives
      // emulate that boundary in this HTTP fixture, not in the product frontend.
      if (nodeIP && (!isIP(nodeIP) || nodeIP.includes('%'))) return route.fulfill({ status: 400, json: { error_code: 'invalid_params' } })
      const addresses = new BlockList()
      if (nodeIP) addresses.addAddress(nodeIP, isIP(nodeIP) === 4 ? 'ipv4' : 'ipv6')
      const pageNumber = Number(url.searchParams.get('page'))
      const filtered = rows.filter(row => {
        const time = url.searchParams.get('time_basis') === 'offline' ? row.stopped_at : row.registered_at
        return (!nodeIP || row.host_node_ips?.some(ip => addresses.check(ip, isIP(ip) === 4 ? 'ipv4' : 'ipv6'))) &&
          (!nodeName || [row.host_node_name, row.runtime_hostname].some(name => name?.toLowerCase() === nodeName)) &&
          (!url.searchParams.has('module_name') || row.module_name === url.searchParams.get('module_name')) &&
          (!url.searchParams.has('role') || row.role === url.searchParams.get('role')) &&
          (!url.searchParams.has('stop_reason') || (row.status === 'down' && row.stop_reason === url.searchParams.get('stop_reason'))) &&
          (url.searchParams.get('time_basis') !== 'offline' || (row.status === 'down' && time)) &&
          (!url.searchParams.has('time_from') || Date.parse(time) >= Date.parse(url.searchParams.get('time_from'))) &&
          (!url.searchParams.has('time_to') || Date.parse(time) < Date.parse(url.searchParams.get('time_to'))) && (!url.searchParams.has('registered_host') || (row.registered_host || '').toLowerCase() === url.searchParams.get('registered_host')) && (!url.searchParams.has('status') || row.status === url.searchParams.get('status')) && (!url.searchParams.has('process_metrics_declared') || String(row.process_metrics_declared) === url.searchParams.get('process_metrics_declared'))
      })
      body = { data: filtered.slice((pageNumber - 1) * 20, pageNumber * 20), page: pageNumber, page_size: 20, total: options.scopes || options.workerTypes || options.instances ? filtered.length : 21 }
    } else if (path.startsWith('/api/v1/system/platform/host_nodes/')) {
      const id = path.split('/').at(-1)
      hostReads.push(id)
      body = options.hosts?.find(host => host.node_id === id)
      if (!body) { status = 404; body = { error_code: 'resource_not_found' } }
      if (hostsHeld) await hostsHeld
    } else if (path === '/api/v1/monitor/platform/process_resource_summaries') {
      calls.push(url.searchParams.get('instance_ids'))
      const requested = new Set(url.searchParams.get('instance_ids').split(',').map(Number))
      body = { data: rows.filter(row => requested.has(row.id)).map(row => ({ subject: { kind: 'module_instance', id: row.id, module_name: row.module_name, instance_id: row.instance_id, role: row.role }, node_id: row.node_id, policy_version: 1, lookback_seconds: 300, queried_at: stamp, collection: { state: 'collecting', sampled_at: stamp }, series: [
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
    } else if (path === '/api/v1/monitor/platform/process_resource_trends') {
      const params = Object.fromEntries(url.searchParams), row = rows.find(row => row.id === Number(params.instance_id))
      trendCalls.push(params)
      const end = Date.parse(params.end), start = Math.max(Date.parse(params.start), Math.ceil(Date.parse(row.process_started_at) / 1000) * 1000), step = 15
      body = { subject: { kind: 'module_instance', id: row.id, module_name: row.module_name, instance_id: row.instance_id, role: row.role }, node_id: row.node_id, policy_version: 1, lookback_seconds: 300, queried_at: stamp, start: new Date(start).toISOString(), end: params.end, step_seconds: step, series: [
        ['process.cpu.core_equivalents', 'cores', 60, 1.5], ['process.memory.resident_bytes', 'bytes', 0, 1024 ** 3]
      ].map(([key, unit, window, value]) => ({ metric_key: key, unit, window_seconds: window, dimensions: {}, points: Array.from({ length: Math.floor((end - start) / (step * 1000)) + 1 }, (_, index) => ({ evaluated_at: new Date(start + index * step * 1000).toISOString(), sampled_at: index === 2 ? null : new Date(start + index * step * 1000).toISOString(), value: index === 2 ? null : value, data_state: index === 2 ? 'no_data' : 'valid' })) })) }
      options.trendResponse?.(body, params)
      if (trendMode === 'wrong-owner') body.subject.instance_id = 'previous-start'
      if (trendMode === 'unavailable' || trendMode === 'denied') { status = trendMode === 'denied' ? 403 : 503; body = { error_code: trendMode === 'denied' ? 'permission_denied' : 'observability_backend_unavailable' } }
      if (trendHeld && row.id === 1) await trendHeld
    } else throw new Error(`unexpected process fixture API ${path}`)
    await route.fulfill({ status, json: body })
  })
  return { setStatus: (id, status) => { const row = rows.find(row => row.id === id); row.status = status; if (status === 'up') { row.stopped_at = null; row.stop_reason = '' } }, calls, listCalls, trendCalls, hostReads, releaseHosts: () => releaseHosts?.(), setEnd: value => { stamp = value }, hold: () => { held = new Promise(yes => { resolve = yes }) }, replaceInstance: (id, next) => { const row = rows.find(row => row.id === id); row.id = next; row.instance_id = `native-${next}` }, setMode: value => { mode = value }, setTrendMode: value => { trendMode = value }, setRuntimeName: (id, value) => { rows.find(row => row.id === id).metadata = { runtime_name: value } }, release: () => { resolve?.(); held = null; trendResolve?.() } }
}
