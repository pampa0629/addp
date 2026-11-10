import { identity } from './monitoring-targets.fixture'
import { resourceBackend } from './node-resources.fixture'
import { engineOwner } from './engine-resources.fixture'
import { engineSummaryFor } from '../src/utils/engineResources'
import { processMetrics } from '../src/utils/processResources'
export const overviewPermissions = ['monitor.resource_observation.read', 'platform.host_node.read', 'platform.module.read']
export const overviewNodeID = '00000000-0000-4000-8000-000000000041'
import { overviewResponse } from './resource-overview-response.js'
export async function overviewBackend(page, options = {}) {
  const base = await resourceBackend(page, { ...options, identity: options.identity || identity(overviewPermissions) })
  await page.route(`**/api/v1/system/platform/host_nodes/${overviewNodeID}`, route => route.fulfill({ json: { node_id: overviewNodeID, display_name: '完整范围主机41', node_kind: 'virtual', version: 1, enabled: true, addresses: ['127.0.0.1'] } }))
  const calls = [], coverageCalls = []
  let coverageMode = 'valid'
  let mode = 'valid', stamp = '2026-10-13T00:00:00Z', held = null, resolve
  await page.route('**/api/v1/system/platform/module_startup_failures', route => route.fulfill({ json: { data: [] } }))
  await page.route('**/api/v1/system/platform/modules', route => route.fulfill({ json: { modules: ['manager', 'meta', 'monitor'].map(module_name => ({ module_name })), count: 3 } }))
  const engineOwners = options.coverage ? [
    engineOwner,
    { ...engineOwner, deployment_id: '10000000-0000-4000-8000-000000000003', enabled: false },
    { ...engineOwner, deployment_id: '10000000-0000-4000-8000-000000000004', scope: 'business', engine_type: 'redis' },
    { ...engineOwner, deployment_id: '10000000-0000-4000-8000-000000000005', scope: 'business', host_binding_state: 'rejected', node_id: '' }
  ] : []
  const processOwners = options.coverage ? ['manager', 'monitor', 'monitor', 'monitor', 'meta'].map((module_name, index) => ({
    id: index + 1, module_name, role: 'backend', instance_id: `${module_name}-${index + 1}`, node_id: '', status: 'up', process_metrics_declared: index !== 2 && index !== 3
  })) : []
  async function inventory(route, key, owners) {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    coverageCalls.push({ key, params })
    const data = owners.filter(row => (!params.module_name || row.module_name === params.module_name) &&
      (!params.process_metrics_declared || String(row.process_metrics_declared) === params.process_metrics_declared))
    await route.fulfill({ json: { data, page: Number(params.page), page_size: Number(params.page_size),
      total: coverageMode === 'incomplete' && key === 'engines' ? 101 : data.length } })
  }
  await page.route('**/api/v1/system/platform/module-instances?*', route => inventory(route, 'services', processOwners))
  await page.route('**/api/v1/system/platform/engine_deployments?*', route => inventory(route, 'engines', engineOwners))
  function sample(row, metrics, subject, valid) {
    return { subject, node_id: row.node_id, queried_at: stamp, policy_version: 1, lookback_seconds: 300,
      collection: { state: valid ? 'collecting' : 'no_sample', sampled_at: valid ? stamp : null },
      series: metrics.map(metric => ({ metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds,
        dimensions: {}, points: [{ evaluated_at: stamp, sampled_at: valid ? stamp : null, value: valid ? 0 : null, data_state: valid ? 'valid' : 'no_data' }] })) }
  }
  await page.route('**/api/v1/monitor/platform/engine_resource_summaries?*', async route => {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    coverageCalls.push({ key: 'engine-samples', params })
    if (coverageMode === 'budget') return route.fulfill({ status: 422, json: { error_code: 'observability_query_budget_exceeded' } })
    if (coverageMode === 'denied') return route.fulfill({ status: 403, json: { error_code: 'permission_denied' } })
    const ids = params.deployment_ids.split(',')
    const data = engineOwners.filter(row => ids.includes(row.deployment_id)).map(row =>
      sample(row, engineSummaryFor(row.engine_type), { ...row, kind: 'engine_deployment' }, row.scope === 'infra'))
    await route.fulfill({ json: { data, start: stamp, end: stamp, queried_at: stamp, policy_version: 1, lookback_seconds: 300, step_seconds: 15 } })
  })
  await page.route('**/api/v1/monitor/platform/process_resource_summaries?*', async route => {
    const params = Object.fromEntries(new URL(route.request().url()).searchParams)
    coverageCalls.push({ key: 'service-samples', params })
    if (coverageMode === 'service-budget') return route.fulfill({ status: 422, json: { error_code: 'observability_query_budget_exceeded' } })
    const ids = params.instance_ids.split(',').map(Number)
    const data = processOwners.filter(row => ids.includes(row.id)).map(row =>
      sample(row, processMetrics, { ...row, kind: 'module_instance' }, row.id === 1))
    await route.fulfill({ json: { data } })
  })
  await page.route('**/api/v1/monitor/platform/resource_overview*', async route => {
    calls.push({ method: route.request().method(), query: new URL(route.request().url()).search, body: route.request().postData() })
    let body = overviewResponse(stamp), status = 200
    if (mode === 'empty') { for (const key of Object.keys(body.inventory)) { if (key !== 'collection') body.inventory[key] = 0 }; body.inventory.collection = Object.fromEntries(Object.keys(body.inventory.collection).map(key => [key, 0])); body.rankings.forEach(rank => { rank.coverage = { expected: 0, valid: 0, no_data: 0, stale: 0, not_connected: 0, complete: true }; rank.top = [] }) }
    if (mode === 'malformed') body.rankings[0].top.reverse()
    if (['budget', 'unavailable', 'denied'].includes(mode)) { status = { budget: 422, unavailable: 503, denied: 403 }[mode]; body = { error_code: { budget: 'observability_query_budget_exceeded', unavailable: 'observability_backend_unavailable', denied: 'permission_denied' }[mode] } }
    if (held) await held
    await route.fulfill({ status, json: body })
  })
  return { ...base, calls, coverageCalls, coverageMode: value => { coverageMode = value }, mode: value => { mode = value }, stamp: value => { stamp = value }, hold: () => { held = new Promise(yes => { resolve = yes }) }, release: () => { resolve?.(); held = null } }
}
