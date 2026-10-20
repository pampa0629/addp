import { expect, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { browserAPIFromResponse, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse, json, login } from './transfer-browser-support.js'

const permissions = ['graph.graph.read', 'graph.review.read', 'graph.review.approve', 'graph.review.reject', 'graph.review.update'].sort()
function required(name) {
  if (!process.env[name]) throw new Error(`missing Online environment: ${name}`)
  return process.env[name]
}

test('ordinary reviewer uses the real queue and observes native batch partial failure', async ({ page }) => {
  const expected = JSON.parse(required('ADDP_ONLINE_GRAPH_REVIEW_EXPECTATIONS'))
  const root = `/api/v1/graph/graphs/${expected.graph_id}/review`
  const failures = [], errors = [], commands = []
  let started = false, batchStage = false
  page.on('request', request => {
    if (request.headers().authorization) started = true
    if (new URL(request.url()).pathname.startsWith(root) && request.method() !== 'GET') {
      commands.push({ method: request.method(), path: new URL(request.url()).pathname, body: request.postDataJSON() })
    }
  })
  page.on('response', response => {
    if (isAnonymousRefreshResponse(response, started)) return
    const path = new URL(response.url()).pathname
    if (path.startsWith('/api/v1/') && response.status() >= 400) {
      failures.push({ path, status: response.status() })
    }
  })
  page.on('pageerror', () => errors.push('pageerror'))
  page.on('console', message => {
    if (isAnonymousRefreshConsoleError(message, started)) return
    if (!['warning', 'error'].includes(message.type())) return
    if (batchStage && message.type() === 'error' &&
        message.text() === 'Failed to load resource: the server responded with a status of 500 (Internal Server Error)' &&
        new URL(message.location().url).pathname === root + '/batch/approve') return
    errors.push(message.type())
  })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  const queueResponse = page.waitForResponse(response => new URL(response.url()).pathname === root)
  await login(page, required('ADDP_ONLINE_GRAPH_REVIEW_USERNAME'), required('ADDP_ONLINE_GRAPH_REVIEW_PASSWORD'),
    `/graph/graphs/${expected.graph_id}/review?tab=all`)
  const response = await queueResponse
  const api = await browserAPIFromResponse(response, required('GATEWAY_URL'), {
    principalID: expected.principal_id, tenantID: expected.tenant_id, permissions: new Set(permissions)
  })
  try {
    const queued = await json(response, 'real pending queue')
    expect(queued.total).toBe(7)
    expect(Object.fromEntries(queued.data.map(item => [item.source_text, item.id]))).toEqual(expected.items)
    const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const row = source => frame.locator('.el-table__body tbody tr').filter({
      has: frame.locator('.source-text').filter({ hasText: new RegExp(`^${source}$`) })
    })
    await expect(frame.locator('.el-table__body tbody tr')).toHaveCount(7)
    await page.reload()
    await expect(frame.locator('#tab-all')).toHaveAttribute('aria-selected', 'true')
    await expect(frame.locator('.el-table__body tbody tr')).toHaveCount(7)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'graph-review-pending-console.png') })
    async function action(source, label, suffix) {
      const result = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === `${root}/${expected.items[source]}/${suffix}`)
      await row(source).getByRole('button', { name: label, exact: true }).click()
      await json(await result, `review ${suffix}`)
      await expect(row(source)).toHaveCount(0)
    }
    await action('approve', '通过', 'approve')
    await action('reject', '拒绝', 'reject')
    await row('modify').getByRole('button', { name: '修改', exact: true }).click()
    const dialog = frame.getByRole('dialog', { name: '修改内容后确认' })
    await dialog.locator('.el-form-item').filter({ has: frame.locator('label').filter({ hasText: /^name$/ }) }).locator('input').fill('Modified fixture')
    const modified = page.waitForResponse(r => r.request().method() === 'PUT' && new URL(r.url()).pathname === `${root}/${expected.items.modify}`)
    await dialog.getByRole('button', { name: '确认写入', exact: true }).click()
    await json(await modified, 'modified review')
    await expect(dialog).toBeHidden()
    await expect(row('modify')).toHaveCount(0)
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'graph-review-modified-console.png') })
    await action('review-link', '通过', 'approve')
    for (const source of ['batch-before', 'bad-native', 'batch-after']) {
      await row(source).locator('.el-checkbox').click()
      await expect(row(source).getByRole('checkbox')).toBeChecked()
    }
    const batch = page.waitForResponse(r => r.request().method() === 'POST' && new URL(r.url()).pathname === root + '/batch/approve')
    const refreshed = page.waitForResponse(r => r.request().method() === 'GET' && new URL(r.url()).pathname === root)
    batchStage = true
    await frame.getByRole('button', { name: /批量通过/ }).click()
    expect((await batch).status()).toBe(500)
    const pending = await json(await refreshed, 'queue refreshed after failure')
    expect(pending.total).toBe(1)
    expect(pending.data[0].id).toBe(expected.items['bad-native'])
    expect(pending.data[0].status).toBe('pending')
    await expect(row('bad-native')).toHaveCount(1)
    await expect(frame.getByRole('button', { name: /批量通过/ })).toHaveCount(0)
    await expect(frame.locator('.el-table__body input[type="checkbox"]:checked')).toHaveCount(0)
    await expect(frame.locator('.el-message--error')).toBeVisible()
    await expect(frame.locator('.el-message--success').filter({ hasText: /已批量通过/ })).toHaveCount(0)
    const all = await json(await api.get(root + '?page_size=100'), 'persisted review result')
    const status = Object.fromEntries(all.data.map(item => [item.source_text, item.status]))
    expect(status).toEqual({ approve: 'approved', reject: 'rejected', modify: 'modified', 'batch-before': 'approved',
      'bad-native': 'pending', 'batch-after': 'approved', 'review-link': 'approved' })
    expect(commands.map(command => [command.method, command.path])).toEqual([
      ['POST', `${root}/${expected.items.approve}/approve`], ['POST', `${root}/${expected.items.reject}/reject`],
      ['PUT', `${root}/${expected.items.modify}`], ['POST', `${root}/${expected.items['review-link']}/approve`], ['POST', root + '/batch/approve']
    ])
    expect(commands.at(-1).body.ids).toEqual(['batch-before', 'bad-native', 'batch-after'].map(key => expected.items[key]))
    expect(failures).toEqual([{ path: root + '/batch/approve', status: 500 }])
    expect(errors).toEqual([])
    await page.screenshot({ path: resolve(required('ADDP_ONLINE_ARTIFACT_DIR'), 'graph-review-partial-failure-console.png') })
    writeFileSync(required('ADDP_ONLINE_GRAPH_REVIEW_REPORT'), JSON.stringify({
      ...expected, schema_version: 'addp.graph-review-browser/v1', result: 'passed', permissions,
      single_approve: true, single_reject: true, modify: true, relation_approve: true,
      batch_status: 500, batch_continued: true, selection_cleared: true, refreshed: true,
      unexpected_http_failures: 0, browser_errors: 0, controlled_extraction: true, real_model: false
    }))
  } finally {
    await api.dispose()
  }
})
