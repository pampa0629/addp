import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect } from '@playwright/test'
import { backend as mockBase, identity, setIdentity } from './monitoring-targets.fixture'

export const deliveryView = {
  id: '626089da-5030-4ce5-9ce9-13c791f562f4', event_id: '80ff1d2a-213b-4cec-99a1-15458c463bb9',
  destination_id: 7, destination_name: '平台值班接收端', destination_version: 3,
  channel: 'webhook', status: 'dead', attempt_count: 8, cycle_attempt_count: 8, manual_retry_count: 0,
  event_type: 'opened', occurred_at: '2026-10-01T08:00:00Z', incident_id: 9, incident_status: 'resolved',
  created_at: '2026-10-01T08:00:01Z', last_error: 'notification_attempt_limit'
}
export async function fixture(page, { destinations = [], deliveries = [], deliveryTotal = deliveries.length, management = true, retryConflict = false, keys, locale = 'zh-cn', entries, path = '/platform-alerts' } = {}) {
  await mockBase(page, { locale, identity: identity(keys || ['monitor.platform_alert.read', 'monitor.platform_alert_notification.read', ...(management ? ['monitor.platform_alert.update', 'monitor.platform_alert_notification.update'] : [])]) })
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
    if (path.endsWith('/alerts')) {
      if (failure) return reply({ error: '观测不可用' }, 503)
      if (held) await barrier
      return reply({ data: entries || [{ id: 9, version: 3, category: 'log_pipeline', subject_kind: 'log_observer', subject_id: 'host-a', signal: 'collector_dropped', severity: 'critical', status: acknowledged ? 'acknowledged' : 'open', opened_at: new Date().toISOString(), instance_id: '' }], total: 1, page: 1, page_size: 20 })
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
  if (!keys || keys.includes('monitor.platform_alert.read')) await expect(page.getByTestId('pipeline-incidents')).toBeVisible()
  else if (keys.includes('monitor.platform_alert_notification.read')) await expect(page.getByTestId('platform-notifications')).toBeVisible()
  return { requests, fail: () => { failure = true }, hold: () => { held = true }, release: () => release() }
}
