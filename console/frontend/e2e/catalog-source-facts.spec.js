import { expect, test } from '@playwright/test'
import { expectSourceFactsLayout, expectFingerprintHover, expectFingerprintFocus } from './helpers/catalog-source-facts.js'

const entryID = '00000000-0000-4000-8000-000000000071'
const fingerprint = 'f8a9a816a7526985e59ffc492a408b3361dcffe9b178ff0d7a0c48babd3fe575'
const engineName = 'Business PostgreSQL 户外业务验收引擎'
const itemName = 'ods_outdoor_activities_source_facts_display_verification'
const fullName = `outdoor.${itemName}`
const entryURL = `/catalog/entries/${entryID}?tab=professional`

async function openSourceFacts(page, width, { audit = false } = {}) {
  const writes = [], unexpected = [], errors = [], reads = []
  let historyReads = 0
  await page.setViewportSize({ width, height: 1000 })
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: {
      access_token: 'catalog-source-facts-reader-token', expires_in: 900
    } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: {
      id: '32', username: 'catalog-reader', display_name: '目录读取验收账号'
    } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { id: '32', type: 'user' },
      context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' },
        permissions: ['catalog.entry.read', ...(audit ? ['catalog.audit.read'] : [])] }] }
    } })
    if (request.method() !== 'GET') writes.push(`${request.method()} ${path}`)
    reads.push({ path, authorization: request.headers().authorization })
    if (path === `/api/v1/catalog/entries/${entryID}`) return route.fulfill({ json: {
      id: entryID, display_name: '技术来源布局验收', entry_type: 'data_item',
      entry_status: 'active', governance_status: 'curated', visibility: 'tenant', version: 3,
      responsibilities: [], semantic_links: [], components: [],
      source_engine: { id: '2', status: 'current', name: engineName, engine_type: 'postgresql' },
      source: { source_module: 'meta', source_type: 'data_item', source_identity: fingerprint,
        source_status: 'active', observed_at: '2026-10-17T02:58:00Z',
        observed_snapshot: { engine_id: '2', item_id: '8', name: itemName, full_name: fullName,
          item_type: 'table', scanned_depth: 'deep' }
      }
    } })
    if (path === `/api/v1/catalog/me/entries/${entryID}/marks`) return route.fulfill({ json: { favorite: false, following: false } })
    if (path === `/api/v1/catalog/entries/${entryID}/data-dictionary`) return route.fulfill({ json: {
      as_of: '2026-10-17T02:58:00Z', generated_at: '2026-10-17T02:58:00Z', fields: []
    } })
    if (audit && path === `/api/v1/catalog/entries/${entryID}/history`) {
      historyReads++
      return route.fulfill({ json: { audit_events: [], source_bindings: [
        { source_identity: fingerprint, source_status: 'active', is_current: true, bound_at: '2026-10-17T02:58:00Z' },
        { source_identity: 'a'.repeat(64), source_status: 'missing', is_current: false, bound_at: '2026-10-16T02:58:00Z' }
      ] } })
    }
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_request' } })
  })
  await page.goto(entryURL)
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  const detail = catalog.getByTestId('catalog-entry-detail')
  await expect(detail).toHaveAttribute('data-load-state', 'loaded')
  const facts = detail.locator('.source-facts-descriptions')
  return { catalog, facts, historyReadCount: () => historyReads, verifyReadOnly() {
    expect(writes).toEqual([])
    expect(unexpected).toEqual([])
    expect(errors).toEqual([])
    expect(reads.length).toBeGreaterThan(0)
    expect(reads.every(read => read.authorization === 'Bearer catalog-source-facts-reader-token')).toBe(true)
  } }
}

test('Catalog source binding history translates canonical statuses when Console language changes in the same iframe', async ({ page }) => {
  const fixture = await openSourceFacts(page, 1440, { audit: true }), { catalog } = fixture
  await catalog.getByRole('tab', { name: '关系与历史', exact: true }).click()
  const rows = catalog.locator('.history-card .el-table__body tbody tr')
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0).locator('td').nth(1)).toHaveText('有效')
  await expect(rows.nth(1).locator('td').nth(1)).toHaveText('来源缺失')
  const origin = await catalog.locator('body').evaluate(() => performance.timeOrigin)
  const historyURL = await catalog.locator('body').evaluate(() => location.href)
  const consoleURL = page.url()
  const reads = fixture.historyReadCount()
  await page.getByRole('button', { name: 'CN', exact: true }).click()
  await page.getByRole('menuitem', { name: 'English', exact: true }).click()
  await expect(rows.nth(0).locator('td').nth(1)).toHaveText('Active')
  await expect(rows.nth(1).locator('td').nth(1)).toHaveText('Missing source')
  await page.getByRole('button', { name: 'EN', exact: true }).click()
  await page.getByRole('menuitem', { name: '中文', exact: true }).click()
  await expect(rows.nth(0).locator('td').nth(1)).toHaveText('有效')
  await expect(rows.nth(1).locator('td').nth(1)).toHaveText('来源缺失')
  expect(await catalog.locator('body').evaluate(() => performance.timeOrigin)).toBe(origin)
  expect(fixture.historyReadCount()).toBe(reads)
  await expect(catalog.getByRole('tab', { name: '关系与历史', exact: true })).toHaveAttribute('aria-selected', 'true')
  expect(await catalog.locator('body').evaluate(() => location.href)).toBe(historyURL)
  await expect(page).toHaveURL(consoleURL)
  fixture.verifyReadOnly()
})

for (const width of [1440, 900]) {
  test(`Catalog technical source keeps balanced columns and readable names at ${width}px`, async ({ page }) => {
    const fixture = await openSourceFacts(page, width)
    await expectSourceFactsLayout(fixture.facts, { engineName, engineType: 'postgresql', itemName, itemType: 'table', fullName })
    fixture.verifyReadOnly()
  })

  test(`Catalog fingerprint is visually truncated and exposes the full value on hover at ${width}px`, async ({ page }) => {
    const fixture = await openSourceFacts(page, width), { catalog, facts } = fixture
    await expectFingerprintHover(catalog, facts, fingerprint)
    fixture.verifyReadOnly()
  })
}

test('Catalog keyboard focus exposes the full fingerprint and survives reload', async ({ page }) => {
  const fixture = await openSourceFacts(page, 1440), { catalog, facts } = fixture
  for (let attempt = 0; attempt < 2; attempt++) {
    await expectFingerprintFocus(catalog, facts, fingerprint)
    await expect(page).toHaveURL(entryURL)
    if (attempt === 0) await page.reload()
  }
  fixture.verifyReadOnly()
})
