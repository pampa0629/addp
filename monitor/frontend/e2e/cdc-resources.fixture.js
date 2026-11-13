import { identity } from './monitoring-targets.fixture'
import { cdcMetrics } from '../src/utils/cdcResources'

export const componentIDs = { redpanda: '10000000-0000-4000-8000-000000000011', 'kafka-connect': '10000000-0000-4000-8000-000000000012' }
export function cdcResponse(kind, keys = null, params = null, mode = '') {
  const end = '2026-11-11T08:00:00Z', start = params ? new Date(Date.parse(end) - 60000).toISOString() : end
  const selected = keys ? cdcMetrics[kind].filter(metric => keys.includes(metric.key)) : cdcMetrics[kind]
  const failed = ['failed', 'snapshot'].includes(mode) && kind === 'kafka-connect'
  return { discovery_state: 'available', queried_at: end, start, end, step_seconds: 15, policy_version: 1, lookback_seconds: 300, data: [{
    subject: { kind: 'infrastructure_component', component_id: componentIDs[kind], component_type: kind, started_at: '2026-11-11T07:00:00Z' },
    collection: { state: params ? 'no_sample' : failed ? mode === 'snapshot' ? 'snapshot_unavailable' : 'failed' : 'collecting', sampled_at: params ? null : end },
    series: selected.map(metric => ({ metric_key: metric.key, dimensions: {}, unit: metric.unit, window_seconds: 0,
      points: Array.from({ length: params ? 5 : 1 }, (_, index) => {
        const at = new Date(Date.parse(start) + index * 15000).toISOString()
        const missing = failed && (!params || index > 2) || mode === 'empty' && ['pendingRecords', 'sourceDelay', 'offsetFailure'].includes(metric.name)
        return { evaluated_at: at, sampled_at: missing ? null : at, value: missing ? null : metric.unit === 'bytes' ? 1024 ** 3 : mode === 'empty' ? 0 : ({ connectors: 2, tasks: 2, sourceDelay: -0.002, failedTasks: 1, runningTasks: 1, streamingConnectors: 1, connected: 1, delaySamples: 1, offsetTasks: 1, groupPartitions: 1, pendingRecords: 4 }[metric.name] || 0), data_state: missing ? 'no_data' : 'valid' }
      }) }))
  }] }
}
export async function cdcBackend(page, { locale = 'zh-cn', permissions = ['monitor.resource_observation.read'], mode: initial = '' } = {}) {
  let mode = initial, held = null, release, trendHeld = null, releaseTrend
  const calls = [], queries = []
  await page.addInitScript(lang => { if (!localStorage.getItem('addp-lang')) localStorage.setItem('addp-lang', lang) }, locale)
  await page.route('**/module-health/**', route => route.fulfill({ json: { status: 'ready' } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname
    let body, status = 200
    if (path.endsWith('/system/refresh')) body = { access_token: 'fixture-token', expires_in: 3600 }
    else if (path.endsWith('/system/users/me')) body = { id: 9, username: 'fixture-user' }
    else if (path.endsWith('/system/auth/context')) body = identity(permissions)
    else {
      calls.push(path); queries.push(Object.fromEntries(url.searchParams))
      if (mode === 'denied') { status = 403; body = { error_code: 'permission_denied' } }
      else if (path.endsWith('/cdc_resource_observations')) {
        body = cdcResponse('redpanda', null, null, mode)
        body.data.push(cdcResponse('kafka-connect', null, null, mode).data[0])
        if (mode === 'unavailable') { body.discovery_state = 'unavailable'; body.data = [] }
        if (mode === 'absent') body.data = []
        if (mode === 'foreign') body.data[0].subject.component_type = 'postgresql'
      } else if (path.endsWith('/cdc_resource_trends')) {
        const kind = Object.keys(componentIDs).find(kind => componentIDs[kind] === url.searchParams.get('component_id'))
        body = cdcResponse(kind, url.searchParams.get('metrics').split(','), Object.fromEntries(url.searchParams), mode)
      } else if (path.endsWith('/platform/modules')) body = { modules: [{ module_name: 'transfer' }], count: 1 }
      else if (path.endsWith('/platform/module_startup_failures')) body = { data: [] }
      else if (path.endsWith('/platform/module-instances')) body = { data: [], total: 0, page: Number(url.searchParams.get('page')), page_size: 20 }
      else throw new Error(`Unexpected CDC fixture API: ${path}`)
      if (held) await held
      if (path.endsWith('/cdc_resource_trends') && trendHeld) await trendHeld
    }
    await route.fulfill({ status, json: body })
  })
  return { calls, queries, setMode: value => { mode = value }, hold: () => { held = new Promise(done => { release = done }) }, release: () => { release?.(); held = null }, holdTrend: () => { trendHeld = new Promise(done => { releaseTrend = done }) }, releaseTrend: () => { releaseTrend?.(); trendHeld = null } }
}
