import { backend as authentication } from './process-resources.fixture'
import { identity } from './monitoring-targets.fixture'
import { httpMetrics } from '../src/utils/httpQuality'

export const httpPermissions = ['platform.module.read', 'monitor.http_observation.read']
export const rulePermissions = ['platform.module.read', ...['read','create','update','delete'].map(action => `monitor.platform_http_rule.${action}`)]
export const httpInstances = [1, 2].map(id => ({ id, module_name: 'monitor', instance_id: `http-${id}`, role: 'backend', status: 'up', node_id: '', process_metrics_declared: true, process_started_at: '2026-10-19T23:00:00Z' }))
export async function httpBackend(page, options = {}) {
  await authentication(page, { ...options, identity: options.identity || identity(httpPermissions) })
  const calls = [], writes = []
  let mode = 'valid', release, held, rule = options.rule || null, conflict = false
  await page.route('**/api/v1/system/platform/module-instances?**', async route => {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams); calls.push({ kind: 'instances', ...params })
    const rows = httpInstances.filter(row => row.role === params.role && (!params.module_name || row.module_name === params.module_name))
    await route.fulfill({ json: { data: rows, total: rows.length, page: Number(params.page), page_size: Number(params.page_size) } })
  })
  await page.route('**/api/v1/monitor/platform/http_*', async route => {
    const url = new URL(route.request().url()), params = Object.fromEntries(url.searchParams), kind = url.pathname.split('/').at(-1)
    calls.push({ kind, ...params }); const ownMode = mode
    const end = params.end || '2026-10-20T00:00:00Z', start = params.start || end, step = 15, window = Number(params.window_seconds || 60)
    const grouped = kind === 'http_interfaces', filtered = Object.hasOwn(params, 'route')
    const instances = httpInstances.filter(row => params.instance_ids.split(',').includes(String(row.id)))
    const data = instances.map(owner => ({ subject: { ...owner, kind: 'module_instance' }, policy_version: 1, queried_at: end, collection: { state: 'collecting', sampled_at: end }, observations: (grouped ? [{ route: '/fast/:id', method: 'GET', mean: .5, qps: 50 }, { route: '/slow/:id', method: 'GET', mean: 2, qps: 20 }] : [{ route: params.route || '', method: params.method || '', mean: 1, qps: owner.id === 2 ? 0 : 10 }]).map(row => ({ route: row.route, method: row.method, series: (grouped || filtered ? httpMetrics.slice(0, -1) : httpMetrics).map(metric => {
      const missing = row.qps === 0 && !['qps','active'].includes(metric.name)
      return { metric_key: metric.key, unit: metric.unit, window_seconds: metric.name === 'active' ? 0 : window, dimensions: {}, points: Array.from({ length: Math.floor((Date.parse(end) - Date.parse(start)) / (step * 1000)) + 1 }, (_, index) => {
        const time = new Date(Date.parse(start) + index * step * 1000).toISOString()
        return { evaluated_at: time, sampled_at: missing ? null : time, value: missing ? null : ({ qps: row.qps, active: row.qps === 0 ? 0 : 1, failure: 20, clientError: 10, mean: row.mean, p95: 2, p99: 3 })[metric.name], data_state: missing ? 'no_data' : 'valid' }
      }) }
    }) })) }))
    if (ownMode === 'foreign') data[0].subject.instance_id = 'previous-process'
    if (held) await held
    await route.fulfill({ status: ownMode === 'denied' ? 403 : 200, json: ownMode === 'denied' ? { error_code: 'permission_denied' } : { start, end, step_seconds: step, window_seconds: window, ...(grouped ? { ranking_limit: 10 } : {}), data } })
  })
  await page.route('**/api/v1/monitor/platform/http-alert-rules**', async route => {
    const request = route.request(), method = request.method(), url = new URL(request.url())
    if (method === 'GET') { calls.push({ kind: 'rules' }); await route.fulfill({ json: { data: rule ? [rule] : [], total: rule ? 1 : 0, page: Number(url.searchParams.get('page')), page_size: 20 } }); return }
    const input = request.postDataJSON(); writes.push({ method, input })
    if (conflict) { await route.fulfill({ status: 409, json: { error_code: 'resource_version_conflict' } }); return }
    rule = { id: '10000000-0000-4000-8000-000000000019', module_name: 'monitor', role: 'backend', route: null, method: '', last_verdict: 'paused', last_error: '', last_value: null, anchor_instance_id: input.instance_id, ...input, version: 1 }
    await route.fulfill({ status: method === 'POST' ? 201 : 200, json: rule })
  })
  return { calls, writes, setMode: value => { mode = value }, setConflict: value => { conflict = value }, hold: () => { held = new Promise(resolve => { release = resolve }) }, release: () => { release?.(); held = null } }
}
