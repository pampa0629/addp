import { backend as authenticate } from './process-resources.fixture'
import { identity } from './monitoring-targets.fixture'
export const tracePermissions = ['monitor.trace.read','platform.module.read']
export async function tracesBackend(page, options = {}) {
  await authenticate(page, { identity: options.identity || identity(tracePermissions), locale: options.locale })
  const calls = []; let mode = 'valid', release, held
  await page.route('**/api/v1/system/platform/module-instances?**', route => route.fulfill({ json: { data: [{ id: 1, module_name: 'monitor', instance_id: 'one', role: 'backend', status: 'down', node_id: '', process_started_at: '2026-10-19T00:00:00Z', process_metrics_declared: false }], total: 1, page: 1, page_size: 10 } }))
  await page.route('**/api/v1/monitor/platform/traces**', async route => {
    const url = new URL(route.request().url()); calls.push(Object.fromEntries(url.searchParams)); const own = mode
    const end = url.searchParams.get('end'), start = options.longSpan ? new Date(Date.parse(url.searchParams.get('start'))-3600000).toISOString() : url.searchParams.get('start')
    const queryStart = url.searchParams.get('start')
    const instance = own === 'foreign' ? 'foreign' : 'one'
    const base = { module_name: 'monitor', instance_id: instance, role: 'backend', method: 'GET', route: '', status: 200, failed: false, start, end, duration_ms: Date.parse(end)-Date.parse(start) }
    const spans = [
      { ...base, span_id: '2'.repeat(16), parent_span_id: '', kind: 'server', route: '/items/:id' },
      { ...base, span_id: '3'.repeat(16), parent_span_id: '2'.repeat(16), kind: 'client' },
      { ...base, span_id: '4'.repeat(16), parent_span_id: '3'.repeat(16), kind: 'server', module_name: 'system', instance_id: 'two', route: '/system/check' },
      { ...base, span_id: '5'.repeat(16), parent_span_id: '2'.repeat(16), kind: 'client' }
    ]
    const edges = [{ client_span_id: '3'.repeat(16), server_span_id: '4'.repeat(16), from_module: 'monitor', from_instance: instance, to_module: 'system', to_instance: 'two' }]
    const data = [{ trace_id: '1'.repeat(32), start, end, duration_ms: base.duration_ms, partial: true, policy_version: 1, coverage: 'sampled', edges, spans }]
    if (held) await held
    await route.fulfill({ status: own === 'failed' ? 503 : 200, json: own === 'failed' ? { error_code: 'trace_backend_unavailable' } : { start: queryStart, end, queried_at: new Date().toISOString(), coverage: 'sampled', policy_version: 1, data } })
  })
  return { calls, mode(v) { mode = v }, hold() { held = new Promise(resolve => { release = resolve }) }, release() { held = null; release?.() } }
}
export const traceQuery = () => { const end = new Date(Date.now()-1000); return `instance_id=1&trace_id=${'1'.repeat(32)}&start=${new Date(end.getTime()-60000).toISOString()}&end=${end.toISOString()}` }
