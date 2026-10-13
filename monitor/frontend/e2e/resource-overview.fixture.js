import { identity } from './monitoring-targets.fixture'
import { resourceBackend } from './node-resources.fixture'
export const overviewPermissions = ['monitor.resource_observation.read', 'platform.host_node.read', 'platform.module.read']
export const overviewNodeID = '00000000-0000-4000-8000-000000000041'
import { overviewResponse } from './resource-overview-response.js'
export async function overviewBackend(page, options = {}) {
  const base = await resourceBackend(page, { ...options, identity: options.identity || identity(overviewPermissions) })
  await page.route(`**/api/v1/system/platform/host_nodes/${overviewNodeID}`, route => route.fulfill({ json: { node_id: overviewNodeID, display_name: '完整范围主机41', node_kind: 'virtual', version: 1, enabled: true, addresses: ['127.0.0.1'] } }))
  const calls = []
  let mode = 'valid', stamp = '2026-10-13T00:00:00Z', held = null, resolve
  await page.route('**/api/v1/system/platform/module-instances?*', route => route.fulfill({ json: { data: [], total: 0, page: 1, page_size: 20, total_pages: 0 } }))
  await page.route('**/api/v1/monitor/platform/resource_overview*', async route => {
    calls.push({ method: route.request().method(), query: new URL(route.request().url()).search, body: route.request().postData() })
    let body = overviewResponse(stamp), status = 200
    if (mode === 'empty') { for (const key of Object.keys(body.inventory)) { if (key !== 'collection') body.inventory[key] = 0 }; body.inventory.collection = Object.fromEntries(Object.keys(body.inventory.collection).map(key => [key, 0])); body.rankings.forEach(rank => { rank.coverage = { expected: 0, valid: 0, no_data: 0, stale: 0, not_connected: 0, complete: true }; rank.top = [] }) }
    if (mode === 'malformed') body.rankings[0].top.reverse()
    if (['budget', 'unavailable', 'denied'].includes(mode)) { status = { budget: 422, unavailable: 503, denied: 403 }[mode]; body = { error_code: { budget: 'observability_query_budget_exceeded', unavailable: 'observability_backend_unavailable', denied: 'permission_denied' }[mode] } }
    if (held) await held
    await route.fulfill({ status, json: body })
  })
  return { ...base, calls, mode: value => { mode = value }, stamp: value => { stamp = value }, hold: () => { held = new Promise(yes => { resolve = yes }) }, release: () => { resolve?.(); held = null } }
}
