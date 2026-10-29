import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI } from '../../../system/frontend/e2e/module-query.fixture'
import { backend, identity, localTimeInput } from '../../../monitor/frontend/e2e/monitoring-targets.fixture'

// Hold only the active iframe's real time-filter publication, preserving its identity.
async function installAlertTimePublicationGate(page) {
  await page.addInitScript(() => {
    if (window.parent !== window) return
    const held = []
    let armed = false
    const handler = event => {
      const message = event.data
      const iframe = document.querySelector('iframe[data-testid="module-iframe"]')
      if (!armed || event.source !== iframe?.contentWindow || event.origin !== new URL(iframe.src).origin ||
          message?.type !== 'addp:console-bridge:request' || message.channel !== 'console-navigation' ||
          message.payload?.synchronized !== true || !new URL(message.payload.route, location.origin).searchParams.has('time_from')) return
      event.stopImmediatePropagation()
      held.push(event)
      // Match the existing bridge's pending acknowledgement while the scheduler is held.
      event.source.postMessage({ type: 'addp:console-bridge:response', source: 'addp-console',
        channel: message.channel, requestId: message.requestId, pending: true }, event.origin)
    }
    window.addEventListener('message', handler, true)
    window.__alertTimePublication = {
      arm: () => { armed = true },
      routes: () => held.map(event => event.data.payload.route),
      release: () => {
        window.removeEventListener('message', handler, true)
        for (const event of held.splice(0)) window.dispatchEvent(new MessageEvent('message', {
          data: event.data, source: event.source, origin: event.origin
        }))
      }
    }
  })
}

for (const locale of ['zh-cn', 'en']) test(`automatic alert text and time filters preserve one Console iframe (${locale})`, async ({ page }) => {
  await backend(page, { locale, identity: identity(['monitor.platform_alert.read', 'monitor.platform_alert_notification.read', 'platform.host_node.read']) })
  const queries = []
  await page.route('**/api/v1/monitor/platform/**', route => {
    const url = new URL(route.request().url())
    if (url.pathname.endsWith('/alerts')) queries.push(Object.fromEntries(url.searchParams))
    return route.fulfill({ json: { data: [], total: 0, page: Number(url.searchParams.get('page')), page_size: 20 } })
  })
  await page.clock.install()
  await page.goto('/monitor/platform-alerts?page=2&category=resource&severity=warning')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('pipeline-incidents')).toBeVisible()
  await expect.poll(() => queries.length).toBe(1)
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin)
  const form = monitor.getByTestId('alert-filters')
  await expect(form.getByRole('button', { name: locale === 'en' ? 'Search' : '搜索', exact: true })).toHaveCount(0)
  const name = form.getByRole('textbox', { name: locale === 'en' ? 'Alert name' : '告警名称', exact: true })
  await name.fill('C'); await name.fill('CPU')
  expect(queries).toHaveLength(1)
  await page.clock.fastForward(301)
  await expect.poll(() => queries.length).toBe(2)
  await expect(page).toHaveURL(url => url.searchParams.get('search') === 'CPU' && !url.searchParams.has('page'))
  const host = form.getByRole('textbox', { name: locale === 'en' ? 'Host name' : '主机名称', exact: true })
  await host.fill('Host'); await host.press('Enter')
  await expect.poll(() => queries.length).toBe(3)
  expect(queries.at(-1)).toMatchObject({ search: 'CPU', host_name: 'Host', category: 'resource', severity: 'warning' })
  await expect(page).toHaveURL(url => url.searchParams.get('search') === 'CPU' && url.searchParams.get('host_name') === 'Host')
  await page.clock.fastForward(301); expect(queries).toHaveLength(3)
  await page.clock.resume()
  const from = monitor.getByPlaceholder(locale === 'en' ? 'Start time' : '起始时间')
  await from.fill(await localTimeInput(page, '2026-10-24T00:00:00Z')); await from.press('Tab')
  await expect.poll(() => queries.at(-1)?.time_from).toBe('2026-10-24T00:00:00.000Z')
  expect(queries.at(-1)).not.toHaveProperty('time_to')
  // A module request can precede the asynchronous Console URL publication.
  await expect.poll(() => new URL(frame.url()).searchParams.get('time_from')).toBe('2026-10-24T00:00:00.000Z')
  await expect(page).toHaveURL(url => url.searchParams.get('time_from') === '2026-10-24T00:00:00.000Z' && url.searchParams.get('search') === 'CPU' && url.searchParams.get('host_name') === 'Host')
  await page.keyboard.press('Escape')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  const beforeReload = queries.length
  await page.reload()
  await expect.poll(() => queries.length).toBeGreaterThan(beforeReload)
  await expect(from).toHaveValue(await localTimeInput(page, '2026-10-24T00:00:00Z'))
  await expect(host).toHaveValue('Host')
  await expect(name).toHaveValue('CPU')
  await expect.poll(() => queries.at(-1)).toMatchObject({ time_from: '2026-10-24T00:00:00.000Z', search: 'CPU', host_name: 'Host' })
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
})

for (const locale of ['zh-cn', 'en']) test(`alert reload waits for the published Console route despite an earlier API result (${locale})`, async ({ page }) => {
  await backend(page, { locale, identity: identity(['monitor.platform_alert.read', 'platform.host_node.read']) })
  await installAlertTimePublicationGate(page)
  const queries = []
  let releaseOld, oldDelivered
  const oldResponse = new Promise(resolve => { releaseOld = resolve })
  const oldDelivery = new Promise(resolve => { oldDelivered = resolve })
  const timeFrom = '2026-10-24T00:00:00.000Z'
  await page.route('**/api/v1/monitor/platform/alerts?**', async route => {
    const query = Object.fromEntries(new URL(route.request().url()).searchParams)
    queries.push(query)
    const first = queries.length === 1
    if (first) await oldResponse
    await route.fulfill({ json: { data: [{ id: 42, version: 1, category: 'resource', subject_kind: 'host_node', subject_id: 'f83e9874-2164-4411-a5c0-650d143006cf',
      signal: 'node.cpu.busy_percent', severity: 'warning', name: first ? 'CPU old' : 'CPU current',
      status: 'open', resolved_at: null, opened_at: '2026-10-25T00:00:00Z' }], total: 1, page: 1, page_size: 20 } })
    if (first) oldDelivered()
  })
  await page.goto('/monitor/platform-alerts?category=resource&severity=warning&search=CPU&host_name=Host')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  const table = monitor.getByTestId('pipeline-incidents')
  await expect(table).toBeVisible()
  await expect.poll(() => queries.length).toBe(1)
  const frame = page.frames().find(frame => frame.parentFrame()), origin = await frame.evaluate(() => performance.timeOrigin)
  await page.evaluate(() => window.__alertTimePublication.arm())
  const from = monitor.getByPlaceholder(locale === 'en' ? 'Start time' : '起始时间')
  await from.fill(await localTimeInput(page, timeFrom)); await from.press('Tab')
  await page.keyboard.press('Escape')
  await expect.poll(() => queries.at(-1)).toMatchObject({ time_from: timeFrom, search: 'CPU', host_name: 'Host' })
  await expect.poll(() => page.evaluate(() => window.__alertTimePublication.routes().length)).toBe(1)
  await expect.poll(() => new URL(frame.url()).searchParams.get('time_from')).toBe(timeFrom)
  await expect(table.getByText('CPU current', { exact: true })).toBeVisible()
  // This is the counterexample: neither an API result nor the iframe URL commits the public URL.
  expect(new URL(page.url()).searchParams.has('time_from')).toBe(false)
  await frame.evaluate(() => window.addEventListener('message', event => {
    if (event.data?.requestId === 'unknown-alert-navigation') window.__unknownAlertReplyObserved = true
  }))
  await page.evaluate(() => {
    const iframe = document.querySelector('iframe[data-testid="module-iframe"]')
    iframe.contentWindow.postMessage({ type: 'addp:console-bridge:response', channel: 'console-navigation',
      requestId: 'unknown-alert-navigation', ok: true, data: { route: '/monitor/platform-alerts' } }, new URL(iframe.src).origin)
  })
  await expect.poll(() => frame.evaluate(() => window.__unknownAlertReplyObserved)).toBe(true)
  expect(new URL(frame.url()).searchParams.get('time_from')).toBe(timeFrom)
  expect(new URL(page.url()).searchParams.has('time_from')).toBe(false)
  releaseOld(); await oldDelivery
  await expect(table.getByText('CPU current', { exact: true })).toBeVisible()
  await expect(table.getByText('CPU old', { exact: true })).toHaveCount(0)
  expect(new URL(page.url()).searchParams.has('time_from')).toBe(false)
  await page.evaluate(() => window.__alertTimePublication.release())
  await expect(page).toHaveURL(url => url.searchParams.get('time_from') === timeFrom && url.searchParams.get('search') === 'CPU' && url.searchParams.get('host_name') === 'Host')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(origin)
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  const committed = new URL(page.url()).search
  const beforeReload = queries.length
  await page.reload()
  await expect.poll(() => queries.length).toBeGreaterThan(beforeReload)
  await expect.poll(() => queries.at(-1)).toMatchObject({ time_from: timeFrom, search: 'CPU', host_name: 'Host', category: 'resource', severity: 'warning' })
  expect(new URL(page.url()).search).toBe(committed)
  await expect(from).toHaveValue(await localTimeInput(page, timeFrom))
  await expect(monitor.getByRole('textbox', { name: locale === 'en' ? 'Alert name' : '告警名称', exact: true })).toHaveValue('CPU')
  await expect(monitor.getByRole('textbox', { name: locale === 'en' ? 'Host name' : '主机名称', exact: true })).toHaveValue('Host')
  await expect(table.getByText('CPU current', { exact: true })).toBeVisible()
  await expect(table.getByText('CPU old', { exact: true })).toHaveCount(0)
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
})

test('platform alert filters and notifications preserve one Console iframe and restore on reload', async ({ page }) => {
  await backend(page, { identity: identity(['monitor.platform_alert.read', 'monitor.platform_alert_notification.read','platform.host_node.read']) })
  const reads = [], incidentQueries = []
  const timeFrom = '2026-10-01T00:00:00.000Z', timeTo = '2026-10-02T00:00:00.000Z'
  await page.route('**/api/v1/monitor/platform/**', route => {
    const url = new URL(route.request().url()); reads.push(url.pathname)
    if (url.pathname.endsWith('/alerts')) incidentQueries.push(Object.fromEntries(url.searchParams))
    return route.fulfill({ json: url.pathname.endsWith('/destinations') ? [] : url.pathname.endsWith('/alert-notification-destinations') ? [] : { data: [], total: 0, page: 1, page_size: 20 } })
  })
  await page.goto(`/monitor/platform-alerts?category=resource&severity=warning&search=CPU%20%25_&host_name=Host%20%25_&time_from=${encodeURIComponent(timeFrom)}&time_to=${encodeURIComponent(timeTo)}`)
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByRole('heading', { name: '平台告警', exact: true })).toBeVisible()
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('平台告警')
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await expect.poll(() => incidentQueries.at(-1)?.severity).toBe('warning')
  expect(incidentQueries.at(-1)).toMatchObject({search:"CPU %_",host_name:"Host %_",time_from:timeFrom,time_to:timeTo})
  await monitor.locator('.el-select').filter({ has: monitor.getByRole('combobox', { name: '严重级别', exact: true }) }).click()
  await monitor.getByRole('option', { name: '严重', exact: true }).click()
  await expect(page).toHaveURL(/category=resource&severity=critical(?:&|$)/)
  await expect.poll(() => incidentQueries.at(-1)?.severity).toBe('critical')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await monitor.getByRole('button', { name: '通知管理', exact: true }).click()
  await expect(page).toHaveURL(/\/monitor\/platform-alerts\?tab=notifications&category=resource&severity=critical(?:&|$)/)
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  await page.reload()
  await expect(monitor.getByTestId('platform-notifications')).toBeVisible()
  expect(new URL(page.url()).searchParams.get('search')).toBe("CPU %_")
  expect(new URL(page.url()).searchParams.get('host_name')).toBe('Host %_')
  expect(new URL(page.url()).searchParams.get('time_from')).toBe(timeFrom)
  expect(new URL(page.url()).searchParams.get('time_to')).toBe(timeTo)
  await monitor.getByRole('button', { name: '告警事件', exact: true }).click()
  await expect.poll(() => incidentQueries.at(-1)).toMatchObject({search:'CPU %_',host_name:'Host %_',severity:'critical',time_from:timeFrom,time_to:timeTo})
  expect(reads.some(path => path.includes('/log-'))).toBe(false)
})
test('Tenant cannot open platform alerts using a platform permission string', async ({ page }) => {
  await backend(page, { identity: identity(['monitor.platform_alert.read'], { type: 'tenant', tenant_id: '7' }) })
  await page.goto('/monitor/platform-alerts')
  await expect(page.locator('.el-result')).toBeVisible()
  await expect(page.locator('iframe.module-iframe')).toHaveCount(0)
})

test('System diagnostics navigate to the sole Monitor management page with observer context', async ({ page }) => {
  await mockModuleQueryAPI(page, { pipelinePermission: true })
  const requests = []
  await page.route('**/api/v1/monitor/platform/**', route => {
    const path = new URL(route.request().url()).pathname; requests.push(path)
    return route.fulfill({ json: path.endsWith('/log-pipeline') ? { deployment_state: 'enabled', configured: true, health: 'alert', active_incidents: 1, notifications: 'unconfigured', node: { node: 'host-a', received_at: new Date().toISOString(), registry_valid: true, observation: { probe_delivered: true, probe_delay_ms: 10 } } } : { data: [], total: 0, page: 1, page_size: 20 } })
  })
  await page.goto('/system/modules?tab=log-pipeline')
  const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(frame.getByTestId('pipeline-health')).toContainText('告警')
  await frame.getByRole('button', { name: '查看平台告警', exact: true }).click()
  await expect(page).toHaveURL(/\/monitor\/platform-alerts\?category=log_pipeline&subject_id=host-a$/)
  await expect(frame.getByRole('heading', { name: '平台告警', exact: true })).toBeVisible()
  await expect(frame.getByPlaceholder('来源主体 ID')).toHaveValue('host-a')
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  expect(requests.some(path => path.endsWith('/alerts'))).toBe(true)
  await page.goBack()
  await expect(page).toHaveURL(/\/system\/modules\?tab=log-pipeline$/)
  await expect(frame.getByTestId('pipeline-health')).toContainText('告警')
})

test('rules-only platform entry preserves the sole iframe and restores rule paging', async ({ page }) => {
  await backend(page, { identity: identity(['monitor.platform_resource_rule.read']) })
  const reads = []
  await page.route('**/api/v1/monitor/platform/**', route => {
    const url = new URL(route.request().url()); reads.push({ path: url.pathname, page: url.searchParams.get('page') })
    return route.fulfill({ json: { data: [], total: 0, presets: [], page: Number(url.searchParams.get('page')), page_size: 20 } })
  })
  await page.goto('/monitor/platform-alerts?tab=rules&rule_page=2')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(monitor.getByTestId('resource-rules')).toBeVisible()
  await expect(page.locator('iframe[data-testid="module-iframe"]')).toHaveCount(1)
  await expect(page.locator('.sidebar .el-menu-item.is-active')).toContainText('平台告警')
  await expect.poll(() => reads.at(-1)?.page).toBe('2')
  await page.reload()
  await expect(monitor.getByTestId('resource-rules')).toBeVisible()
  expect(reads.every(row => row.path.endsWith('/resource-alert-rules'))).toBe(true)
  await expect(monitor.getByRole('button', { name: '通知管理', exact: true })).toHaveCount(0)
})

test('Console instance rule dialog is painted and interactive inside the Monitor iframe', async ({ page }, testInfo) => {
  await backend(page, { identity: identity(['platform.module.read', ...['read', 'create', 'update', 'delete'].map(action => `monitor.platform_resource_rule.${action}`)]) })
  await page.route('**/api/v1/system/platform/module-instances?**', route => {
    const params = new URL(route.request().url()).searchParams
    return route.fulfill({ json: { data: [{ id: 1, module_name: 'monitor', instance_id: 'native-1', role: 'backend', status: 'up', node_id: '', process_metrics_declared: true, process_started_at: '2026-10-20T00:00:00Z' }], total: 1, page: Number(params.get('page')), page_size: Number(params.get('page_size')) } })
  })
  await page.route('**/api/v1/monitor/platform/process-resource-alert-rules**', route => route.fulfill({ json: { data: [], total: 0, page: 1, page_size: 20 } }))
  await page.goto('/monitor/platform-alerts?tab=process-rules')
  const monitor = page.frameLocator('iframe[data-testid="module-iframe"]')
  await monitor.getByRole('button', { name: '创建服务实例规则', exact: true }).click()
  const dialog = monitor.getByRole('dialog', { name: '创建服务实例规则', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog).toHaveCSS('opacity', '1')
  const field = dialog.getByRole('textbox', { name: '名称', exact: true })
  await field.fill('可见弹窗')
  await expect(field).toHaveValue('可见弹窗')
  const panel = dialog.locator('.el-dialog')
  await expect(panel).toBeInViewport()
  await expect(panel).toHaveCSS('opacity', '1')
  await expect(dialog.locator('..')).toHaveCSS('opacity', '1')
  await dialog.locator('.el-select:has(input[aria-label="选择服务实例"])').click()
  await expect(monitor.getByRole('option', { name: 'monitor · native-1', exact: true })).toBeVisible()
  await monitor.getByRole('option', { name: 'monitor · native-1', exact: true }).click()
  await expect(dialog).toContainText('monitor · native-1')
  await page.screenshot({ path: testInfo.outputPath('console-instance-rule-dialog.png'), fullPage: true })
  await dialog.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(dialog).not.toBeVisible()
})
