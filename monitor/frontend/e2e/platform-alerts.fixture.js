import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect } from '@playwright/test'
import { resourceBackend } from './node-resources.fixture'
import { backend as mockBase, identity, setIdentity } from './monitoring-targets.fixture'

export const deliveryView = {
  id: '626089da-5030-4ce5-9ce9-13c791f562f4', event_id: '80ff1d2a-213b-4cec-99a1-15458c463bb9',
  destination_id: 7, destination_name: '平台值班接收端', destination_version: 3,
  channel: 'webhook', status: 'dead', attempt_count: 8, cycle_attempt_count: 8, manual_retry_count: 0,
  event_type: 'opened', occurred_at: '2026-10-01T08:00:00Z', incident_id: 9, incident_status: 'resolved',
  created_at: '2026-10-01T08:00:01Z', last_error: 'notification_attempt_limit'
}
export async function fixture(page, { destinations = [], deliveries = [], deliveryTotal = deliveries.length, management = true, retryConflict = false, keys, locale = 'zh-cn', entries, rules = [], ruleTotal = rules.length, ruleConflict = false, resources = false, resourceOptions = {}, hostNames = {}, path = '/platform-alerts' } = {}) {
  const setupBackend = resources ? resourceBackend : mockBase
  const resourceState = await setupBackend(page, { ...resourceOptions, locale, identity: identity(keys || ['monitor.platform_alert.read', 'monitor.platform_alert_notification.read', ...(management ? ['monitor.platform_alert.update', 'monitor.platform_alert_notification.update'] : [])]) })
  destinations = destinations.map(row => ({ categories: ['log_pipeline'], recipients: [], event_types: ['opened', 'resolved'], ...row }))
  const requests = []
  let failure = false, acknowledged = false, held = false, release
  const barrier = new Promise(resolve => { release = resolve })
  await page.route('**/api/v1/monitor/platform/**', async route => {
    const req = route.request(), path = new URL(req.url()).pathname
    const headers = { 'access-control-allow-origin': req.headers().origin || browserTestOrigin('monitor'), 'access-control-allow-credentials': 'true', 'access-control-allow-headers': 'authorization,content-type', 'access-control-allow-methods': 'GET,POST,PUT,DELETE,OPTIONS' }
    if (req.method() === 'OPTIONS') return route.fulfill({ status: 204, headers })
    const reply = (json, status = 200) => route.fulfill({ headers, json, status })
    const body = req.postDataJSON(); requests.push({ path, query: Object.fromEntries(new URL(req.url()).searchParams), method: req.method(), body })
    if (path.includes('/resource_')) return route.fallback()
    if (path.includes('/resource-alert-rules')) {
      if (req.method() === 'GET') {
        if (held) await barrier
        const q = new URL(req.url()).searchParams
        const scoped = q.has('node_id') || q.has('metric_key') || q.has('device')
        const matched = rules.filter(row => (!q.has('node_id') || row.node_id === q.get('node_id')) && (!q.has('metric_key') || row.metric_key === q.get('metric_key')) && ['device','mountpoint','fstype'].every(key => !q.has(key) || row.dimensions?.[key] === q.get(key)))
        const pageNumber = Number(q.get('page')), size = Number(q.get('page_size'))
        return reply({ data: scoped ? matched.slice((pageNumber-1)*size,pageNumber*size) : rules, total: scoped ? matched.length : ruleTotal, page: pageNumber, presets: [
          { metric_key: 'node.cpu.busy_percent', policy: { trigger_percent: 90, recovery_percent: 80, trigger_seconds: 300, recovery_seconds: 120 } },
          { metric_key: 'node.memory.used_percent', policy: { trigger_percent: 80, recovery_percent: 75, trigger_seconds: 300, recovery_seconds: 120 } },
          { metric_key: 'node.filesystem.used_percent', policy: { trigger_percent: 85, recovery_percent: 80, trigger_seconds: 300, recovery_seconds: 120 } }
        ] })
      }
      if (ruleConflict) return reply({ error: 'Rule conflict' }, 409)
      if (req.method() === 'POST') { const row = { ...body, id: 'a0e9964c-e2d3-46ba-b151-52249cff528e', version: 1, last_verdict: 'paused', last_error: '' }; rules.push(row); ruleTotal++; return reply(row) }
      if (req.method() === 'PUT') { const row = { ...body, id: path.split('/').at(-1), version: body.version + 1, last_verdict: 'unknown', last_error: 'warming_up' }; rules = rules.map(item => item.id === row.id ? row : item); return reply(row) }
      rules = rules.filter(item => !path.endsWith(item.id)); ruleTotal = rules.length; return reply({})
    }
    if (path.endsWith('/alerts')) {
      if (failure) return reply({ error: '观测不可用' }, 503)
      if (held) await barrier
      const query = new URL(req.url()).searchParams, severity = query.get('severity')
      if (severity && !['critical', 'warning'].includes(severity)) return reply({ error: 'Invalid severity' }, 400)
      const search = query.get('search') || '', hostName = query.get('host_name') || ''
      if (hostName && !keys?.includes('platform.host_node.read')) return reply({error:'Host read permission required'},403)
      if (/[\p{Cc}\p{Cs}]/u.test(hostName) || [...hostName.trim()].length > 100) return reply({error:'Invalid host name'},400)
      if (/[\p{Cc}\p{Cs}]/u.test(search) || [...search.trim()].length > 100) return reply({error:'Invalid alert name'},400)
      const from = query.get('time_from'), to = query.get('time_to')
      if (from && !Number.isFinite(Date.parse(from)) || to && !Number.isFinite(Date.parse(to)) || from && to && Date.parse(from) >= Date.parse(to)) return reply({ error: 'Invalid opening time range' }, 400)
      const rows = (entries || [{ id: 9, version: 3, category: 'log_pipeline', subject_kind: 'log_observer', subject_id: 'host-a', signal: 'collector_dropped', name: locale === 'en' ? 'Collector dropped entries increased' : '采集器新增丢弃', severity: 'critical', status: acknowledged ? 'acknowledged' : 'open', opened_at: new Date().toISOString(), instance_id: '' }]).filter(row => (!hostName.trim() || row.category === 'resource' && row.subject_kind === 'host_node' && (hostNames[row.subject_id] || '').toLowerCase().includes(hostName.trim().toLowerCase())) && (!search.trim() || (row.name || '').toLowerCase().includes(search.trim().toLowerCase())) && (!severity || row.severity === severity) && (!from || Date.parse(row.opened_at) >= Date.parse(from)) && (!to || Date.parse(row.opened_at) < Date.parse(to)))
      const currentPage = Number(query.get('page') || 1)
      return reply({ data: rows.slice((currentPage - 1) * 20, currentPage * 20), total: rows.length, page: currentPage, page_size: 20 })
    }
    if (path.endsWith('/acknowledge')) { acknowledged = true; return reply({ id: 9 }) }
    if (path.endsWith('/retry')) {
      if (retryConflict) return reply({ error: '投递状态或通知目标已变化，请刷新后重新确认' }, 409)
      const row = deliveries.find(item => path.endsWith(`/${item.id}/retry`))
      Object.assign(row, { status: 'pending', manual_retry_count: row.manual_retry_count + 1, cycle_attempt_count: 0, last_error: '', next_attempt_at: new Date().toISOString() })
      return reply(row)
    }
    if (path.endsWith('/alert-notification-deliveries')) return reply({ data: deliveries, total: deliveryTotal, page: Number(new URL(req.url()).searchParams.get('page')), page_size: 20 })
    if (path.endsWith('/alert-notification-destinations') && req.method() === 'GET') return reply(destinations)
    if (path.endsWith('/alert-notification-destinations')) return reply({ ...body, id: 1, version: 1 })
    if (path.endsWith('/credential')) return reply({ id: 1, version: 2, channel: requests.find(r => r.method === 'POST' && r.path.endsWith('/alert-notification-destinations'))?.body.channel || 'webhook', enabled: false })
    if (path.endsWith('/alert-notification-destinations/1')) return reply({ ...body, id: 1, version: 3 })
    throw new Error(`Unexpected platform request: ${req.method()} ${path}`)
  })
  await page.goto(path)
  if (new URL(path, 'http://fixture').searchParams.get('tab') === 'rules' && keys?.includes('monitor.platform_resource_rule.read')) await expect(page.getByTestId('resource-rules')).toBeVisible()
  else if (!keys || keys.includes('monitor.platform_alert.read')) await expect(page.getByTestId('pipeline-incidents')).toBeVisible()
  else if (keys.includes('monitor.platform_alert_notification.read')) await expect(page.getByTestId('platform-notifications')).toBeVisible()
  else if (keys.includes('monitor.platform_resource_rule.read')) await expect(page.getByTestId('resource-rules')).toBeVisible()
  return { requests, resourceState, fail: () => { failure = true }, hold: () => { held = true }, release: () => release() }
}

export const resourceIncident = {
  id: 42, version: 3, category: 'resource', subject_kind: 'host_node', subject_id: 'f83e9874-2164-4411-a5c0-650d143006cf',
  signal: 'node.memory.used_percent', severity: 'warning', status: 'resolved', opened_at: '2026-10-06T23:50:00Z', resolved_at: '2026-10-06T23:59:00Z',
  resource_evidence: {
    rule_id: 'a0e9964c-e2d3-46ba-b151-52249cff528e', rule_version: 2, node_version: 1, target_id: '1621bb62-cb0e-40e1-b988-0d4d36683e51', target_version: 1, query_policy_version: 1,
    policy: { trigger_percent: 80, recovery_percent: 75, trigger_seconds: 300, recovery_seconds: 120 },
    evaluated_at: '2026-10-06T23:50:00.789Z', sampled_from: '2026-10-06T23:45:00Z', sampled_to: '2026-10-06T23:50:00Z', samples: 21, minimum: 81, maximum: 96, latest: 90
  }
}
