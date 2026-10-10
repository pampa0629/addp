import { allResourceMetrics, summaryMetrics } from '../src/utils/nodeResources'
import { identity, node, setIdentity } from './monitoring-targets.fixture'
export { identity, node, setIdentity }
export const secondNode = '15daeb87-b7bf-434b-a8ea-598c82e266d4'
export const resourcePermissions = ['platform.host_node.read', 'monitor.resource_observation.read']
export const mount = { device: '/dev/fixture', mountpoint: '/data', fstype: 'ext4' }
export const serverEnd = '2026-10-07T00:00:00Z'
export function observations(id, keys, trend = false, mode = '', end = serverEnd) {
  const points = key => {
    const item = allResourceMetrics.find(metric => metric.key === key)
    const filesystemValues = { 'node.filesystem.total_bytes': 100 * 1024 ** 3, 'node.filesystem.free_bytes': 30 * 1024 ** 3, 'node.filesystem.available_bytes': 25 * 1024 ** 3, 'node.filesystem.used_bytes': 70 * 1024 ** 3, 'node.filesystem.used_percent': 100 * 70 / 95, 'node.filesystem.inodes_total': 100, 'node.filesystem.inodes_free': mode === 'inode-empty' ? 100 : 80, 'node.filesystem.inodes_used': mode === 'inode-empty' ? 0 : 20, 'node.filesystem.inodes_used_percent': mode === 'inode-empty' ? 0 : 20 }
    const loadValue = mode === 'load-active' ? { load1m: 10.59, load5m: 10.86, load15m: 12.12 }[item.name] : undefined
    const numeric = loadValue ?? filesystemValues[key] ?? (item.unit === 'milliseconds' ? 2.5 : item.unit === 'bytes_per_second' ? mode === 'network-idle' && key.startsWith('node.network.') ? 0 : 2048 : undefined) ?? (item.unit === 'bytes' ? 17179869184 : item.name === 'uptime' ? 3600 : 0)
    return (trend ? [-30, -15, 0].map(offset => new Date(Date.parse(end) + offset * 1000).toISOString()) : [end]).map((evaluated_at, index) => {
      const data_state = (mode === 'load-missing' && item.unit === 'load' || mode === 'disk-idle' && item.unit === 'milliseconds' || mode === 'device-no-data' && (key.startsWith('node.disk.') || key.startsWith('node.network.')) || mode === 'disk-missing' && key.startsWith('node.disk.') || ['network-missing', 'network-uncollected', 'network-failed', 'network-unknown'].includes(mode) && key.startsWith('node.network.')) || mode === 'collection-failed' || mode === 'filesystem-uncollected' && key.startsWith('node.filesystem.') ? 'no_data' : mode === 'inode-missing' && key.startsWith('node.filesystem.inodes_') ? 'no_data' : mode === 'cpu-warmup' && item.name === 'cpuBusy' ? 'no_data' : mode === 'disconnected' ? 'not_connected' : trend && index === 1 ? 'no_data' : !trend && item.name === 'memoryAvailable' ? 'stale' : 'valid'
      return { evaluated_at, sampled_at: data_state === 'not_connected' || data_state === 'no_data' ? null : evaluated_at, value: data_state === 'no_data' || data_state === 'not_connected' ? null : numeric, data_state }
    })
  }
  const collection = { state: mode === 'disconnected' ? 'not_connected' : mode === 'collection-failed' ? 'failed' : 'collecting', sampled_at: mode === 'disconnected' ? null : end, network: ['disconnected', 'collection-failed'].includes(mode) ? 'unknown' : mode === 'network-uncollected' ? 'not_collected' : mode === 'network-failed' ? 'failed' : mode === 'network-unknown' ? 'unknown' : 'available', filesystem: mode === 'disconnected' || mode === 'collection-failed' ? 'unknown' : mode === 'filesystem-uncollected' ? 'not_collected' : 'available' }
  return { ...(trend ? {} : { collection }), subject: { kind: 'node', node_id: id }, end, start: trend ? new Date(Date.parse(end) - 30000).toISOString() : end, queried_at: end, step_seconds: 15, node_version: 1, policy_version: 1, series: keys.map(key => ({ metric_key: key, dimensions: key.startsWith('node.filesystem.') && !['disconnected', 'filesystem-uncollected'].includes(mode) ? mount : (key.startsWith('node.disk.') || key.startsWith('node.network.')) && !['disconnected', 'disk-missing', 'network-missing', 'network-uncollected', 'network-failed', 'network-unknown', 'collection-failed'].includes(mode) ? { device: key.startsWith('node.network.') ? 'eth0' : 'sda' } : {}, unit: allResourceMetrics.find(item => item.key === key).unit, window_seconds: allResourceMetrics.find(item => item.key === key).windowSeconds || 0, points: points(key) })) }
}
export async function resourceBackend(page, options = {}) {
  const state = { reads: [], mode: options.mode || '', summaryMode: options.summaryMode || '', listNodes: options.nodes, trendMode: options.trendMode || '', diskDevices: options.diskDevices, targetID: options.targetID, serverEnd, identity: options.identity || identity(resourcePermissions) }
  let release
  let held = options.holdInstant ? new Promise(resolve => { release = resolve }) : null
  let releaseSummary, heldSummary = options.holdSummary ? new Promise(resolve => { releaseSummary = resolve }) : null
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), options.locale || 'zh-cn')
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname
    let body, status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = state.identity
    else if (path.startsWith('/api/v1/system/platform/host_nodes')) {
      state.reads.push({ path, query: Object.fromEntries(url.searchParams) })
      const nodes = (state.listNodes || [node, secondNode].map((node_id, index) => ({ node_id, display_name: index ? '节点乙' : '节点甲', addresses: ['127.0.0.1'], enabled: true, version: 1 })))
      if (path.endsWith('/host_nodes')) body = { data: nodes, total: 45, page: Number(url.searchParams.get('page')), page_size: Number(url.searchParams.get('page_size')) }
      else { body = nodes.find(item => path.endsWith(item.node_id)); if (!body) { status = 404; body = { error: 'not found' } } }
    } else if (path.startsWith('/api/v1/monitor/platform/monitoring_targets') && req.method() === 'GET') {
      state.reads.push({ path, method: req.method() })
      const target = state.targetID ? { id: state.targetID, version: 1, subject: { kind: 'node', node_id: node }, monitor_kind: 'host_resources', source: { type: 'node_exporter', endpoint: 'https://node.test:9443/metrics' }, enabled: true } : null
      if (path.endsWith('/monitoring_targets')) body = { data: target ? [target] : [], total: target ? 1 : 0, page: Number(url.searchParams.get('page')), page_size: Number(url.searchParams.get('page_size')), total_pages: target ? 1 : 0 }
      else if (target && path.endsWith(`/${target.id}`)) body = target
      else { status = 404; body = { error: 'not found' } }
    } else if (path.endsWith('/resource_summaries')) {
      state.reads.push({ path, query: Object.fromEntries(url.searchParams), method: req.method() })
      const mode = state.summaryMode || state.mode
      body = { data: url.searchParams.get('node_ids').split(',').map(id => observations(id, summaryMetrics.map(metric => metric.key), false, mode, state.serverEnd)) }
      if (mode === 'invalid') body.data[0].subject.node_id = secondNode
      if (mode === 'stale') for (const row of body.data) {
        const old = new Date(Date.parse(row.end) - 61000).toISOString()
        row.collection = { state: 'stale', sampled_at: old, filesystem: 'unknown', network: 'unknown' }
        for (const series of row.series) Object.assign(series.points[0], { sampled_at: old, data_state: 'stale' })
      }
      if (mode === 'no-sample') for (const row of body.data) {
        row.collection = { state: 'no_sample', sampled_at: null, filesystem: 'unknown', network: 'unknown' }
        for (const series of row.series) Object.assign(series.points[0], { sampled_at: null, value: null, data_state: 'no_data' })
      }
      if (mode === 'unavailable' || mode === 'denied' || mode === 'budget') { status = mode === 'denied' ? 403 : mode === 'budget' ? 422 : 503; body = { error_code: mode === 'denied' ? 'permission_denied' : mode === 'budget' ? 'observability_query_budget_exceeded' : 'observability_backend_unavailable', error: mode } }
      if (heldSummary) await heldSummary
    } else if (path.startsWith('/api/v1/monitor/platform/resource_')) {
      const trend = path.endsWith('/resource_trends'), mode = trend ? state.trendMode || state.mode : url.searchParams.get('metrics').startsWith('node.network.') ? state.networkMode || state.mode : url.searchParams.get('metrics').startsWith('node.disk.') ? state.diskMode || state.mode : url.searchParams.get('metrics').startsWith('node.filesystem.inodes_') ? state.inodeMode || state.mode : url.searchParams.get('metrics').startsWith('node.filesystem.') ? state.filesystemMode || state.mode : state.mode
      state.reads.push({ path, query: Object.fromEntries(url.searchParams), method: req.method() })
      const id = url.searchParams.get('node_id'), keys = url.searchParams.get('metrics').split(',')
      if (!trend && held && id === node) await held
      body = observations(id, keys, trend, mode, state.serverEnd)
      if (!trend && state.targetID) body.target_id = state.targetID
      const family = keys[0].startsWith('node.network.') ? 'network' : keys[0].startsWith('node.filesystem.') ? 'filesystem' : null
      if (!trend && family && state[`${family}Collection`]) body.collection = { ...body.collection, ...state[`${family}Collection`] }
      if (state.diskDevices && keys.every(key => key.startsWith('node.disk.'))) {
        const devices = trend ? state.diskDevices.filter(device => device.device === url.searchParams.get('device')) : state.diskDevices
        body.series = body.series.flatMap(series => devices.map(device => ({ ...series, dimensions: { device: device.device }, points: series.points.map(point => {
          const observed = device.points[allResourceMetrics.find(metric => metric.key === series.metric_key).name] || point
          return { ...point, ...observed, sampled_at: ['no_data', 'not_connected'].includes(observed.data_state) ? null : point.evaluated_at }
        }) })))
      }
      if (['disabled', 'unconfigured', 'unavailable', 'timeout', 'budget', 'busy', 'denied'].includes(mode)) {
        const codes = { disabled: [409, 'observability_capability_disabled'], unconfigured: [503, 'observability_capability_unconfigured'], unavailable: [503, 'observability_backend_unavailable'], timeout: [504, 'observability_query_timeout'], budget: [422, 'observability_query_budget_exceeded'], busy: [429, 'observability_query_concurrency_exceeded'], denied: [403, 'permission_denied'] }
        const result = codes[mode]; status = result[0]; body = { error_code: result[1], error: mode }
      }
      if (mode === 'invalid') body.subject.node_id = secondNode
    } else throw new Error(`Unexpected resource fixture API ${path}`)
    await route.fulfill({ status, json: body })
  })
  return Object.assign(state, { hold: () => { held = new Promise(resolve => { release = resolve }) }, release: () => { release?.(); held = null }, releaseSummary: () => { releaseSummary?.(); heldSummary = null } })
}
