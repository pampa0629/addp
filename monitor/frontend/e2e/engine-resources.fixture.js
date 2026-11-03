import { identity } from './monitoring-targets.fixture'
import { engineMetricsFor, engineSummaryFor } from '../src/utils/engineResources'

export const engineID = '10000000-0000-4000-8000-000000000001'
export const engineOwner = { scope: 'infra', deployment_id: engineID, display_name: 'Infra PostgreSQL', enabled: true, engine_type: 'postgresql', node_id: '10000000-0000-4000-8000-000000000002', version: 1, host_binding_state: 'bound' }
export const enginePermissions = ['monitor.resource_observation.read', 'platform.engine_deployment.read']
export async function engineBackend(page, { locale = 'zh-cn', auth = identity(enginePermissions), mode: initial = 'valid', owner = { ...engineOwner }, owners = null, hosts = [], holdHosts = false } = {}) {
  const deploymentRows = owners || [owner]
  let mode = initial, capacity = 1024 ** 3
  const calls = [], queries=[], stamp = '2026-10-20T14:00:00Z'
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
    else if (path === '/api/v1/system/platform/engine_deployments') {
      calls.push(path); queries.push(Object.fromEntries(url.searchParams))
      const matching = deploymentRows.filter(row =>
        (!url.searchParams.has('scope') || row.scope === url.searchParams.get('scope')) &&
        (!url.searchParams.get('search') || row.display_name.toLowerCase().includes(url.searchParams.get('search').toLowerCase())))
      const currentPage = Number(url.searchParams.get('page') || 1), pageSize = Number(url.searchParams.get('page_size') || 20)
      body = {data: matching.slice((currentPage-1)*pageSize,currentPage*pageSize),page:currentPage,page_size:pageSize,total:matching.length}
    }
    else if (path.startsWith('/api/v1/system/platform/engine_deployments/')) {
      calls.push(path); body = deploymentRows.find(row => row.deployment_id === path.split('/').pop()); if(!body){status=404;body={error_code:'resource_not_found'}}
    }
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
      const ids=summary ? url.searchParams.get('deployment_ids').split(',') : [url.searchParams.get('deployment_id')]
      const selected=deploymentRows.filter(row=>ids.includes(row.deployment_id))
      const responses=selected.map(owner=>{
        const metrics = summary ? engineSummaryFor(owner.engine_type) : engineMetricsFor(owner.engine_type).filter(item => url.searchParams.get('metrics').split(',').includes(item.key))
      const start = trend ? new Date(Math.max(Date.parse(url.searchParams.get('start')), Date.parse(stamp) - 120000)).toISOString() : stamp
      const count = trend ? Math.floor((Date.parse(stamp) - Date.parse(start)) / 15000) + 1 : 1
      const inactive = owner.host_binding_state === 'rejected' || !owner.enabled
      const absentState = inactive ? 'not_active' : { 'not-connected': 'not_connected', 'source-unavailable': 'source_unavailable' }[mode]
      const failure = { 'snapshot-unavailable': 'snapshot_unavailable', 'scrape-failed': 'failed', stale: 'stale' }[mode]
      const state = absentState || (trend || mode === 'no-sample' ? 'no_sample' : failure || 'collecting')
      const sampledAt = absentState || trend || mode === 'no-sample' ? null : mode === 'stale' ? new Date(Date.parse(stamp) - 61000).toISOString() : stamp
      return {
        queried_at: stamp, start, end: stamp, step_seconds: 15, policy_version: 1, lookback_seconds: 300,
        data: [{
          subject: { kind: 'engine_deployment', deployment_id: owner.deployment_id, display_name: owner.display_name, engine_type: owner.engine_type },
          node_id: owner.node_id, collection: { state, sampled_at: sampledAt },
          series: metrics.map(metric => ({
            metric_key: metric.key, unit: metric.unit, window_seconds: metric.windowSeconds, dimensions: {},
            points: Array.from({ length: count }, (_, index) => {
              const at = new Date(Date.parse(start) + index * 15000).toISOString()
              // A failed current scrape can coexist with valid earlier history.
              const missing = mode==='minio-unpublished' && ['minioOffline','minioInflight','minio4xx','minio5xx','minioErrorPercent'].includes(metric.name) || mode==='no-scan' && ['minioObjectBytes','minioObjects','minioBuckets','minioScanAge'].includes(metric.name) || absentState || mode === 'no-sample' || failure && (!trend || index >= count - 2)
                || mode === 'no-limit' && metric.name === 'redisPercent' || mode === 'no-lookups' && metric.name === 'redisHitPercent' || trend && index === 2
              return {
                evaluated_at: at, sampled_at: missing ? null : at,
                value: missing ? null : mode === 'over-limit' && metric.name === 'redisPercent' ? 180 : mode === 'no-limit' && metric.name === 'redisLimit' ? 0 : metric.unit === 'bytes' ? capacity : 0,
                data_state: absentState || (missing ? 'no_data' : 'valid')
              }
            })
          }))
        }]
      }
      })
      body=responses.length ? {...responses[0],data:responses.flatMap(value=>value.data)} : {data:[]}
      if (mode === 'foreign') body.data[0].subject.deployment_id = engineOwner.node_id
      if (['unavailable', 'denied', 'notFound'].includes(mode)) { status = { unavailable: 503, denied: 403, notFound: 404 }[mode]; body = { error_code: mode === 'unavailable' ? 'observability_backend_unavailable' : 'permission_denied', error: 'fixture refusal' } }
      if (held) await held
    } else throw new Error(`Unexpected engine fixture API ${path}`)
    await route.fulfill({ status, json: body })
  })
  return { calls, queries, hostReads, owner, setCapacity: value => { capacity = value }, setHostStatus: value => { hostStatus = value }, releaseHosts: () => { releaseHosts?.(); heldHosts = null }, setMode: value => { mode = value }, hold: () => { held = new Promise(resolve => { release = resolve }) }, release: () => { release?.(); held = null } }
}
