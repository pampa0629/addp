import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

const engines = [25, 29].map(id => ({
  id, name: `Expansion engine ${id}`, engine_type: 'postgresql',
  lifecycle_state: 'active', connection_status: 'online'
}))
const rootKey = id => `addp://engine/${id}/path/?type=server&node_id=${id * 10}`
const branchKey = id => `addp://engine/${id}/path/business?type=schema&node_id=${id * 10 + 1}`
const branch = (id, loaded = false) => ({
  id: branchKey(id), locator: branchKey(id), label: `schema ${id}`, type: 'schema', hasChildren: true,
  children: loaded ? [{
    id: `addp://engine/${id}/path/business/customers?type=table&item_id=${id * 10 + 2}`,
    locator: `addp://engine/${id}/path/business/customers?type=table&item_id=${id * 10 + 2}`,
    label: `customers ${id}`, type: 'table', hasChildren: false, children: []
  }] : []
})
const root = id => ({ id: rootKey(id), locator: rootKey(id), label: `Expansion engine ${id}`,
  type: 'server', hasChildren: true, children: [branch(id)] })
const row = (page, label) => page.locator('.el-tree-node__content').filter({ hasText: new RegExp(`^\\s*${label}\\s*$`) }).first()
const arrow = (page, label) => row(page, label).locator(':scope > .el-tree-node__expand-icon')
const expanded = (page, label, value) => expect(row(page, label).locator('..')).toHaveAttribute('aria-expanded', String(value))

async function installBackend(page) {
  const state = { holdChildren: false, releaseChildren: null, rootRequests: [], rootGates: new Map(), searchGates: new Map(), searchRequests: [], ancestorGates: new Map() }
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: [] } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const send = json => route.fulfill({ json })
    if (url.pathname === '/api/v1/system/refresh') return send({ access_token: 'expansion-test', expires_in: 3600 })
    if (url.pathname === '/api/v1/system/users/me') return send({ id: '1', display_name: 'expansion-test' })
    if (url.pathname === '/api/v1/system/auth/context') return send(managerAuthContext)
    if (url.pathname === '/api/v1/manager/engines') return send({ data: engines })
    const match = url.pathname.match(/^\/api\/v1\/meta\/resource-tree\/(25|29)(\/\w+)?$/)
    if (match) {
      const id = Number(match[1])
      if (match[2] === '/search') {
        const keyword = url.searchParams.get('q')
        state.searchRequests.push(`${id}:${keyword}`)
        const gate = state.searchGates.get(`${id}:${keyword}`)
        if (gate) await new Promise(resolve => { gate.release = resolve })
        if (gate?.fail) return route.fulfill({ status: 500, json: { error: '旧搜索失败' } })
        return send({ results: [{ ...branch(id), label: `${keyword} ${id}` }] })
      }
      if (match[2] === '/ancestors') {
        const gate = state.ancestorGates.get(id)
        if (gate) await new Promise(resolve => { gate.release = resolve })
        return send({ ancestors: [root(id), branch(id)], target_locator: branchKey(id) })
      }
      if (match[2] === '/node') {
        if (state.holdChildren) await new Promise(resolve => { state.releaseChildren = resolve })
        return send(branch(id, true))
      }
      state.rootRequests.push(id)
      const gate = state.rootGates.get(id)
      if (gate) await new Promise(resolve => { gate.release = resolve })
      if (gate?.fail) return route.fulfill({ status: 500, json: { error: '引擎目录读取失败' } })
      return send(root(id))
    }
    return send({})
  })
  return state
}

test('catalog roots start collapsed without fetching any resource tree', async ({ page }) => {
  const backend = await installBackend(page)
  await page.goto('/data-explorer')
  await expanded(page, engines[0].name, false)
  await expanded(page, engines[1].name, false)
  expect(backend.rootRequests).toEqual([])
})

test('a late search response cannot mix old and new keywords across engines', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = {}
  backend.searchGates.set('25:old', gate)
  await page.goto('/data-explorer')
  const input = page.locator('.explorer-search input')
  await input.fill('old')
  await expect.poll(() => typeof gate.release).toBe('function')
  await input.fill('new')
  expect(await page.locator('.explorer-search .no-results').count()).toBe(0)
  await expect(page.locator('.result-item .node-label')).toHaveText(['new 25', 'new 29'])
  const response = page.waitForResponse(response => response.url().includes('/25/search') && new URL(response.url()).searchParams.get('q') === 'old')
  gate.release()
  await response
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect(page.locator('.result-item .node-label')).toHaveText(['new 25', 'new 29'])
})

test('clearing a pending search discards its later failure', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = { fail: true }
  backend.searchGates.set('25:old', gate)
  await page.goto('/data-explorer')
  await page.locator('.explorer-search input').fill('old')
  await expect.poll(() => typeof gate.release).toBe('function')
  await page.locator('.explorer-search input').fill('')
  const response = page.waitForResponse(response => response.url().includes('/25/search'))
  gate.release()
  await response
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect(page.locator('.el-message--error')).toHaveCount(0)
  await expect(page.locator('.search-results')).toHaveCount(0)
})

for (const [locale, prefix] of [['zh-cn', '搜索失败：'], ['en', 'Search failed: ']]) {
  test(`a failed search stays visible as a failure until a new search succeeds (${locale})`, async ({ page }) => {
    await installBackend(page)
    await page.addInitScript(value => localStorage.setItem('addp-lang', value), locale)
    await page.route('**/api/v1/meta/resource-tree/25/search?**', async route => {
      if (new URL(route.request().url()).searchParams.get('q') !== 'broken') return route.fallback()
      return route.fulfill({ status: 503, json: { error: 'Search source unavailable' } })
    })
    await page.goto('/data-explorer')
    await page.locator('.explorer-search input').fill('broken')
    await expect(page.locator('.explorer-search .el-alert--error')).toContainText(prefix + 'Search source unavailable')
    await expect(page.locator('.explorer-search .no-results')).toHaveCount(0)
    await page.locator('.explorer-search input').fill('new')
    await expect(page.locator('.result-item .node-label')).toHaveText(['new 25', 'new 29'])
    await expect(page.locator('.explorer-search .el-alert--error')).toHaveCount(0)
  })
}

for (const [locale, countLabel] of [['zh-cn', '找到 2 个匹配资源'], ['en', '2 matching resources found']]) {
  test(`search distinguishes equal resource names by engine with a localized count (${locale})`, async ({ page }) => {
    await installBackend(page)
    await page.addInitScript(value => localStorage.setItem('addp-lang', value), locale)
    await page.route('**/api/v1/meta/resource-tree/*/search?**', route => {
      const engineId = Number(new URL(route.request().url()).pathname.split('/').at(-2))
      return route.fulfill({ json: { results: [{ ...branch(engineId), label: 'customers' }] } })
    })
    await page.goto('/data-explorer')
    await page.locator('.explorer-search input').fill('customers')
    await expect(page.locator('.result-item .node-label')).toHaveText(['customers', 'customers'])
    await expect.soft(page.locator('.results-count')).toHaveText(countLabel)
    await expect.soft(page.locator('.result-item .result-engine')).toHaveText(engines.map(engine => engine.name))
    await page.locator('.result-item').filter({ hasText: engines[1].name }).click()
    await expect(row(page, 'schema 29').locator('..')).toHaveClass(/is-current/)
  })
}

test('an unloaded root can be collapsed while loading and stays closed after its identity resolves', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = {}
  backend.rootGates.set(25, gate)
  await page.goto('/data-explorer')
  await arrow(page, engines[0].name).click()
  await expect.poll(() => typeof gate.release).toBe('function')
  await arrow(page, engines[0].name).click()
  await expanded(page, engines[0].name, false)
  const response = page.waitForResponse(response => /resource-tree\/25\?/.test(response.url()))
  gate.release()
  await response
  await expect(row(page, engines[0].name).locator('..')).toHaveAttribute('data-key', rootKey(25))
  await expanded(page, engines[0].name, false)
  await arrow(page, engines[0].name).click()
  await expect(row(page, 'schema 25')).toBeVisible()
  expect(backend.rootRequests).toEqual([25])
})

test('a pending root label toggles closed and a failed root can be retried with one arrow click', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = { fail: true }
  backend.rootGates.set(25, gate)
  await page.goto('/data-explorer')
  await row(page, engines[0].name).click()
  await expect.poll(() => typeof gate.release).toBe('function')
  await row(page, engines[0].name).click()
  await expanded(page, engines[0].name, false)
  gate.release()
  await expect(page.locator('.el-message--error')).toBeVisible()
  backend.rootGates.delete(25)
  await arrow(page, engines[0].name).click()
  await expect(row(page, 'schema 25')).toBeVisible()
  expect(backend.rootRequests).toEqual([25, 25])
})

test('a slow root label click cannot replace a later selected engine', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = {}
  backend.rootGates.set(25, gate)
  await page.goto('/data-explorer')
  await row(page, engines[0].name).click()
  await expect.poll(() => typeof gate.release).toBe('function')
  await row(page, engines[1].name).click()
  await expect(page.locator('.node-panel .header-title')).toHaveText(engines[1].name)
  const response = page.waitForResponse(response => /resource-tree\/25\?/.test(response.url()))
  gate.release()
  await response
  await expect(row(page, engines[0].name).locator('..')).toHaveAttribute('data-key', rootKey(25))
  await expect(row(page, engines[1].name).locator('..')).toHaveClass(/is-current/)
  await expect(page.locator('.node-panel .header-title')).toHaveText(engines[1].name)
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(rootKey(29))
})

test('a slow root label click cannot replace a later search result selection', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = {}
  backend.rootGates.set(25, gate)
  await page.goto('/data-explorer')
  await row(page, engines[0].name).click()
  await expect.poll(() => typeof gate.release).toBe('function')
  await page.locator('.explorer-search input').fill('new')
  await page.locator('.result-item').filter({ hasText: 'new 29' }).click()
  await expect(row(page, 'schema 29').locator('..')).toHaveClass(/is-current/)
  const response = page.waitForResponse(response => /resource-tree\/25\?/.test(response.url()))
  gate.release()
  await response
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect(row(page, 'schema 29').locator('..')).toHaveClass(/is-current/)
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(branchKey(29))
})

test('a pending URL locator does not replace a later tree selection', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = {}
  backend.ancestorGates.set(25, gate)
  await page.goto(`/data-explorer?locator=${encodeURIComponent(branchKey(25))}`)
  await expect.poll(() => typeof gate.release).toBe('function')
  await row(page, engines[1].name).click()
  await expect(page.locator('.node-panel .header-title')).toHaveText(engines[1].name)
  const response = page.waitForResponse(response => response.url().includes('/25/ancestors'))
  gate.release()
  await response
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect(row(page, engines[1].name).locator('..')).toHaveClass(/is-current/)
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(rootKey(29))
  await expanded(page, engines[0].name, false)
})

test('catalog refresh preserves loaded subtrees and never fetches unopened engines', async ({ page }) => {
  const backend = await installBackend(page)
  await page.goto('/data-explorer')
  await row(page, engines[0].name).click()
  await row(page, 'schema 25').click()
  await expect(row(page, 'customers 25')).toBeVisible()
  await page.locator('.tree-header button').click()
  await expect(page.locator('.el-message--success')).toBeVisible()
  await expect(row(page, 'customers 25')).toBeVisible()
  await expanded(page, engines[0].name, true)
  await expanded(page, 'schema 25', true)
  await expect(row(page, 'schema 25').locator('..')).toHaveClass(/is-current/)
  expect(backend.rootRequests).toEqual([25])
})

for (const selection of ['schema', 'customers']) {
  test(`keeps a collapsed engine closed when another engine loads with a selected ${selection}`, async ({ page }) => {
    await installBackend(page)
    await page.goto('/data-explorer')
    await row(page, engines[0].name).click()
    await row(page, 'schema 25').click()
    await expect(row(page, 'customers 25')).toBeVisible()
    if (selection === 'customers') await row(page, 'customers 25').click()
    await arrow(page, engines[0].name).click()
    await expanded(page, engines[0].name, false)
    await arrow(page, engines[1].name).click()
    await expect(row(page, 'schema 29')).toBeVisible()
    await expanded(page, engines[0].name, false)
    await expanded(page, engines[1].name, true)
    await expect(row(page, `${selection} 25`).locator('..')).toHaveClass(/is-current/)
    await arrow(page, 'schema 29').click()
    await expect(row(page, 'customers 29')).toBeVisible()
    await expanded(page, engines[0].name, false)
  })
}

test('a delayed child response does not reopen its collapsed engine', async ({ page }) => {
  const backend = await installBackend(page)
  await page.goto('/data-explorer')
  await row(page, engines[0].name).click()
  backend.holdChildren = true
  await row(page, 'schema 25').click()
  await expect.poll(() => typeof backend.releaseChildren).toBe('function')
  await arrow(page, engines[0].name).click()
  await expanded(page, engines[0].name, false)
  backend.holdChildren = false
  const response = page.waitForResponse(response => response.url().includes('/resource-tree/25/node'))
  backend.releaseChildren()
  await response
  await expect(row(page, 'customers 25')).toBeAttached()
  await expanded(page, engines[0].name, false)
  await arrow(page, engines[1].name).click()
  await expect(row(page, 'schema 29')).toBeVisible()
  await expanded(page, engines[0].name, false)
})
