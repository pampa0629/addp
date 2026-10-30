import { identity } from './monitoring-targets.fixture'
import { engineMetrics, engineSummaryMetrics } from '../src/utils/engineResources'

export const engineID = '10000000-0000-4000-8000-000000000001'
export const engineOwner = { deployment_id: engineID, display_name: 'Infra PostgreSQL', enabled: true, engine_type: 'postgresql', node_id: '10000000-0000-4000-8000-000000000002', version: 1, host_binding_state: 'bound' }
export const enginePermissions = ['monitor.resource_observation.read', 'platform.engine_deployment.read']
export async function engineBackend(page, { locale = 'zh-cn', auth = identity(enginePermissions), mode: initial = 'valid', owner = { ...engineOwner }, hosts = [], holdHosts = false } = {}) {
  let mode = initial, capacity = 1024 ** 3
  const calls = [], stamp = '2026-10-20T14:00:00Z'
  let held, release, releaseHosts, hostStatus = 200
  let heldHosts = holdHosts ? new Promise(resolve => { releaseHosts = resolve }) : null
  const hostReads = []
  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    let body, status = 200
    if (path === '/api/v1/system/refresh') body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path === '/api/v1/system/users/me') body = { id: 9, username: 'fixture-user' }
    else if (path === '/api/v1/system/auth/context') body = auth
    else if (path === '/api/v1/system/platform/engine_deployments') { calls.push(path); body = { data: [owner], page: Number(url.searchParams.get('page')), page_size: 20, total: 1 } }
    else if (path === `/api/v1/system/platform/engine_deployments/${engineID}`) { calls.push(path); body = owner }
    else if (path.startsWith('/api/v1/system/platform/host_nodes/')) {
      const id = path.split('/').pop()
      hostReads.push(id)
      if (heldHosts) await heldHosts
      const host = hosts.find(item => item.node_id === id)
      status = hostStatus === 200 && !host ? 404 : hostStatus
      body = status === 200 ? host : { error_code: 'permission_denied', error: 'fixture host refusal' }
    } else if (path.startsWith('/api/v1/monitor/platform/resource_')) {
      status = 503; body = { error_code: 'observability_backend_unavailable', error: 'fixture host metrics unavailable' }
    }
    else if (path.startsWith('/api/v1/monitor/platform/engine_resource_')) {
      calls.push(path)
      const trend = path.endsWith('_trends'), summary = path.endsWith('_summaries')
      const metrics = summary ? engineSummaryMetrics : engineMetrics.filter(item => url.searchParams.get('metrics').split(',').includes(item.key))
      const start = trend ? new Date(Math.max(Date.parse(url.searchParams.get('start')), Date.parse(stamp) - 120000)).toISOString() : stamp
      const count = trend ? Math.floor((Date.parse(stamp) - Date.parse(start)) / 15000) + 1 : 1
      const inactive = owner.host_binding_state === 'rejected' || !owner.enabled
      const state = inactive ? 'not_active' : mode === 'sql-failed' ? 'snapshot_unavailable' : trend ? 'no_sample' : 'collecting'
      body = { queried_at: stamp, start, end: stamp, step_seconds: 15, policy_version: 1, lookback_seconds: 300, data: [{ subject: { kind: 'engine_deployment', deployment_id: engineID, display_name: owner.display_name, engine_type: 'postgresql' }, node_id: owner.node_id, collection: { state, sampled_at: trend || inactive ? null : stamp }, series: metrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {}, points: Array.from({ length: count }, (_, index) => { const at = new Date(Date.parse(start) + index * 15000).toISOString(), missing = inactive || mode === 'sql-failed' || trend && index === 2; return { evaluated_at: at, sampled_at: missing ? null : at, value: missing ? null : metric.unit === 'bytes' ? capacity : 0, data_state: inactive ? 'not_active' : missing ? 'no_data' : 'valid' } }) })) }] }
      if (mode === 'foreign') body.data[0].subject.deployment_id = engineOwner.node_id
      if (['unavailable', 'denied', 'notFound'].includes(mode)) { status = { unavailable: 503, denied: 403, notFound: 404 }[mode]; body = { error_code: mode === 'unavailable' ? 'observability_backend_unavailable' : 'permission_denied', error: 'fixture refusal' } }
      if (held) await held
    } else throw new Error(`Unexpected engine fixture API ${path}`)
    await route.fulfill({ status, json: body })
  })
  return { calls, hostReads, owner, setCapacity: value => { capacity = value }, setHostStatus: value => { hostStatus = value }, releaseHosts: () => { releaseHosts?.(); heldHosts = null }, setMode: value => { mode = value }, hold: () => { held = new Promise(resolve => { release = resolve }) }, release: () => { release?.(); held = null } }
}
