import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'
import { stableEmptyReviewSelection } from './graph-visual.fixture.js'

for (const [action, label] of [['approve', '批量通过'], ['reject', '批量拒绝']]) {
  test(`Graph review ${action} selects styled table checkboxes in order and clears selection after batch failure`, async ({ page }, testInfo) => {
    const root = '/api/v1/graph/graphs/1/review'
    const sources = ['batch-before', 'bad-native', 'batch-after']
    let pending = sources.map((source_text, index) => ({
      id: index + 1, source_text, item_type: 'entity', status: 'pending', confidence: 0.4,
      content: { type: 'Person', properties: { name: source_text } }
    }))
    const submitted = [], errors = []
    page.on('pageerror', error => errors.push(error.message))
    const unexpected = await mockGraph(page, {
      permissions: ['graph.graph.read', 'graph.review.read', `graph.review.${action}`],
      responses: { '/api/v1/graph/graphs/1': { id: 1, name: '隔离审核夹具' } }
    })
    await page.route('**/api/v1/graph/graphs/1/review**', route => {
      const request = route.request()
      if (request.method() === 'GET' && new URL(request.url()).pathname === root) {
        return route.fulfill({ json: { data: pending, total: pending.length } })
      }
      if (request.method() === 'POST' && new URL(request.url()).pathname === root + '/batch/' + action) {
        submitted.push(request.postDataJSON().ids)
        pending = pending.filter(item => item.source_text === 'bad-native')
        return route.fulfill({ status: 500, json: { error: '隔离夹具批量写入失败' } })
      }
      unexpected.push(`${request.method()} ${new URL(request.url()).pathname}`)
      return route.fulfill({ status: 500, json: { error: 'unexpected review request' } })
    })
    await page.goto('/graph/graphs/1/review?tab=all')
    const graph = page.frameLocator('iframe[data-testid="module-iframe"]')
    const rows = graph.locator('.el-table__body tbody tr')
    await expect(rows).toHaveCount(3)
    for (const source of sources) {
      const row = rows.filter({ has: graph.locator('.source-text').filter({ hasText: new RegExp(`^${source}$`) }) })
      await row.locator('.el-checkbox').click()
      await expect(row.getByRole('checkbox')).toBeChecked()
    }
    await expect(graph.getByRole('button', { name: `${label} (3)`, exact: true })).toBeVisible()
    const refreshed = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === root)
    await graph.getByRole('button', { name: `${label} (3)`, exact: true }).click()
    await refreshed
    expect(submitted).toEqual([[1, 2, 3]])
    await expect(rows).toHaveCount(1)
    await expect(rows.locator('.source-text')).toHaveText('bad-native')
    await expect(rows.getByRole('checkbox')).not.toBeChecked()
    await expect(graph.getByRole('button', { name: new RegExp(label) })).toHaveCount(0)
    await expect(graph.locator('.el-message--error')).toHaveText('隔离夹具批量写入失败')
    await expect(graph.locator('.el-message--success')).toHaveCount(0)
    const stable = await graph.locator('.el-table').evaluate(stableEmptyReviewSelection)
    expect(stable.states).toHaveLength(2) // Header and the remaining failed row.
    const table = graph.locator('.el-table')
    await table.evaluate(element => { element.style.visibility = 'hidden' })
    await expect(table.evaluate(stableEmptyReviewSelection, { timeout: 200 })).rejects.toThrow('unchecked visual state')
    await table.evaluate(element => { element.style.visibility = '' })
    await testInfo.attach('stable-empty-selection.json', { body: JSON.stringify(stable, null, 2), contentType: 'application/json' })
    await testInfo.attach('stable-empty-selection.png', { body: await page.screenshot(), contentType: 'image/png' })
    expect(unexpected).toEqual([])
    expect(errors).toEqual([])
  })
}
