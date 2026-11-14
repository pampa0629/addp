import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

test.use({ trace: 'retain-on-failure' })

const engines = [25, 29].map(id => ({
  id, name: `Expansion engine ${id}`, engine_type: 'postgresql',
  lifecycle_state: 'active', connection_status: 'online'
}))
const rootKey = id => `addp://engine/${id}/path/?type=server&node_id=${id * 10}`
const branchKey = id => `addp://engine/${id}/path/business?type=schema&node_id=${id * 10 + 1}`
const customerKey = id => `addp://engine/${id}/path/business/customers?type=table&item_id=${id * 10 + 2}`
const branch = (id, loaded = false) => ({
  id: branchKey(id), locator: branchKey(id), label: `schema ${id}`, type: 'schema', hasChildren: true,
  children: loaded ? [{
    id: customerKey(id),
    locator: customerKey(id),
    label: `customers ${id}`, type: 'table', hasChildren: false, children: [],
    metadata: { item_id: id * 10 + 2, has_children: false }
  }] : []
})
const root = id => ({ id: rootKey(id), locator: rootKey(id), label: `Expansion engine ${id}`,
  type: 'server', hasChildren: true, children: [branch(id)] })
const row = (page, label) => page.locator('.el-tree-node__content').filter({ hasText: new RegExp(`^\\s*${label}\\s*$`) }).first()
const arrow = (page, label) => row(page, label).locator(':scope > .el-tree-node__expand-icon')
const expanded = (page, label, value) => expect(row(page, label).locator('..')).toHaveAttribute('aria-expanded', String(value))

async function installBackend(page) {
  const state = {
    holdChildren: false, releaseChildren: null, rootRequests: [], rootGates: new Map(),
    searchGates: new Map(), searchRequests: [], ancestorGates: new Map(),
    engines, nodeRequests: [], childGates: new Map(), root, node: id => branch(id, true), writes: []
  }
  await page.route('**/plugins/manifest.json', route => route.fulfill({ json: { scripts: [] } }))
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    const send = json => route.fulfill({ json })
    if (route.request().method() !== 'GET' && /^\/api\/v1\/(meta|manager)\//.test(url.pathname)) {
      state.writes.push(url.pathname)
    }
    if (url.pathname === '/api/v1/system/refresh') return send({ access_token: 'expansion-test', expires_in: 3600 })
    if (url.pathname === '/api/v1/system/users/me') return send({ id: '1', display_name: 'expansion-test' })
    if (url.pathname === '/api/v1/system/auth/context') return send(managerAuthContext)
    if (url.pathname === '/api/v1/manager/engines') return send({ data: state.engines })
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
        const locator = url.searchParams.get('locator')
        state.nodeRequests.push(locator)
        const node = state.node(id, locator)
        const gate = state.childGates.get(locator)
        if (gate) await new Promise(resolve => { gate.release = resolve })
        if (state.holdChildren) await new Promise(resolve => { state.releaseChildren = resolve })
        return send(node)
      }
      state.rootRequests.push(id)
      const gate = state.rootGates.get(id)
      if (gate) await new Promise(resolve => { gate.release = resolve })
      if (gate?.fail) return route.fulfill({ status: 500, json: { error: '引擎目录读取失败' } })
      return send(state.root(id))
    }
    return send({})
  })
  return state
}

for (const target of ['name', 'folder', 'arrow']) {
  test(`an unopened MinIO prefix uses the same expansion lifecycle via its ${target}`, async ({ page }) => {
    const backend = await installBackend(page)
    const locator = (path, type, identity) => `addp://engine/25/path/${path}?type=${type}&${identity}`
    const prefixLocator = locator('addp/movie', 'prefix', 'node_id=253')
    const bucketLocator = locator('addp', 'bucket', 'node_id=251')
    const movie = { id: prefixLocator, locator: prefixLocator, label: 'movie', type: 'prefix', hasChildren: true, children: [] }
    const bucket = { id: bucketLocator, locator: bucketLocator, label: 'addp', type: 'bucket', hasChildren: true, children: [movie] }
    const itemLocator = locator('addp/movie/video.mov', 'object', 'item_id=254')
    const item = { id: itemLocator, locator: itemLocator, label: 'video.mov', type: 'object', hasChildren: false, children: [] }
    const catalogLocator = locator('', 'service', 'node_id=250')
    backend.engines = [{ ...engines[0], engine_type: 'minio' }, engines[1]]
    backend.root = id => id === 25 ? { ...root(id), id: catalogLocator, locator: catalogLocator, type: 'service', children: [bucket] } : root(id)
    backend.node = (id, key) => key === prefixLocator ? { ...movie, children: [item] } : bucket
    const gate = {}
    backend.childGates.set(prefixLocator, gate)
    await page.goto('/data-explorer')
    await arrow(page, engines[0].name).click()
    await arrow(page, 'addp').click()
    const control = target === 'arrow' ? arrow(page, 'movie')
      : target === 'folder' ? row(page, 'movie').locator('.node-main > .el-icon')
        : row(page, 'movie').locator('.explorer-node-label__text')
    await control.click()
    await expect.poll(() => typeof gate.release).toBe('function')
    await expanded(page, 'movie', true)
    if (target !== 'arrow') {
      await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(prefixLocator)
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    }
    expect(backend.nodeRequests.filter(key => key === prefixLocator)).toHaveLength(1)
    await control.click()
    await expanded(page, 'movie', false)
    const response = page.waitForResponse(response => response.url().includes('/25/node') && new URL(response.url()).searchParams.get('locator') === prefixLocator)
    gate.release()
    await response
    await expect(row(page, 'video.mov')).toBeHidden()
    await expanded(page, 'movie', false)
    await control.click()
    await expect(row(page, 'video.mov')).toBeVisible()
    expect(backend.nodeRequests.filter(key => key === prefixLocator)).toHaveLength(1)
    await expect(arrow(page, 'video.mov')).toHaveClass(/is-leaf/)
  })
}

test('catalog roots start collapsed without fetching any resource tree', async ({ page }) => {
  const backend = await installBackend(page)
  await page.goto('/data-explorer')
  await expanded(page, engines[0].name, false)
  await expanded(page, engines[1].name, false)
  expect(backend.rootRequests).toEqual([])
})

test('a PostgreSQL DataItem stays a leaf without Node aggregate fields', async ({ page }) => {
  await installBackend(page)
  await page.goto('/data-explorer')
  await arrow(page, engines[0].name).click()
  await arrow(page, 'schema 25').click()
  await expect(row(page, 'customers 25')).toBeVisible()
  await expect(arrow(page, 'customers 25')).toHaveClass(/is-leaf/)
  await expect(arrow(page, 'customers 25')).toHaveCSS('color', 'rgba(0, 0, 0, 0)')
  await expect(row(page, 'customers 25').locator('.tree-node-wrapper')).not.toHaveClass(/resource-tree-unloaded-children-inline/)
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

test('catalog refresh reads open branches and retains a visible selection without opening other engines', async ({ page }) => {
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
  expect(backend.rootRequests).toEqual([25, 25])
  expect(backend.nodeRequests).toEqual([branchKey(25), branchKey(25)])
  expect(backend.writes).toEqual([])
})

test('one catalog refresh clears revoked selection, details and locator but keeps its visible sibling', async ({ page }) => {
  const backend = await installBackend(page)
  const authorized = branch(25, true)
  const sibling = { ...authorized.children[0], id: customerKey(25).replace('customers', 'members').replace('252', '253'),
    label: 'members 25', metadata: { item_id: 253, has_children: false } }
  sibling.locator = sibling.id
  backend.node = () => ({ ...authorized, children: [...authorized.children, sibling] })
  await page.goto('/data-explorer')
  await arrow(page, engines[0].name).click()
  await arrow(page, 'schema 25').click()
  await row(page, 'customers 25').click()
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(customerKey(25))
  await expect(page.locator('.preview-container').getByText(/business\/customers/)).toBeVisible()

  backend.node = () => ({ ...authorized, children: [sibling] })
  await page.locator('.tree-header button').click()
  await expect(page.locator('.el-message--success')).toBeVisible()
  await expect(row(page, 'customers 25')).toHaveCount(0)
  await expect(row(page, 'members 25')).toBeVisible()
  await expect(page.locator('.preview-container').getByText(/business\/customers/)).toHaveCount(0)
  await expect.poll(() => new URL(page.url()).searchParams.has('locator')).toBe(false)
  await expanded(page, 'schema 25', true)
  expect(backend.writes).toEqual([])
})

test('a child response started before catalog refresh cannot restore a revoked item', async ({ page }) => {
  const backend = await installBackend(page)
  const gate = {}
  backend.childGates.set(branchKey(25), gate)
  await page.goto('/data-explorer')
  await arrow(page, engines[0].name).click()
  await arrow(page, 'schema 25').click()
  await expect.poll(() => typeof gate.release).toBe('function')

  backend.childGates.delete(branchKey(25))
  backend.node = () => ({ ...branch(25), hasChildren: false })
  await page.locator('.tree-header button').click()
  await expect(page.locator('.el-message--success')).toBeVisible()
  const late = page.waitForResponse(response => new URL(response.url()).pathname.endsWith('/25/node'))
  gate.release()
  await late
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
  await expect(row(page, 'customers 25')).toHaveCount(0)
})

test('catalog refresh checks a selected item under a collapsed engine without reopening it', async ({ page }) => {
  const backend = await installBackend(page)
  await page.goto('/data-explorer')
  await arrow(page, engines[0].name).click()
  await arrow(page, 'schema 25').click()
  await row(page, 'customers 25').click()
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(customerKey(25))
  await arrow(page, engines[0].name).click()
  await expanded(page, engines[0].name, false)

  await page.locator('.tree-header button').click()
  await expect(page.locator('.el-message--success')).toBeVisible()
  await expanded(page, engines[0].name, false)
  expect(new URL(page.url()).searchParams.get('locator')).toBe(customerKey(25))
  await expect(page.locator('.preview-container').getByText(/business\/customers/)).toBeVisible()
  expect(backend.rootRequests).toEqual([25, 25])
  expect(backend.nodeRequests).toEqual([branchKey(25), branchKey(25)])
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

test('authorized catalog keeps an empty node and removes the last visible engine on refresh', async ({ page }) => {
  const backend = await installBackend(page)
  backend.engines = [engines[0]]
  const empty = { ...branch(25), id: branchKey(25).replace('business', 'empty').replace('251', '253'),
    label: 'empty directory', hasChildren: false, children: [] }
  empty.locator = empty.id
  backend.root = id => ({ ...root(id), children: [branch(id), empty] })
  await page.goto('/data-explorer')
  await expect(row(page, engines[0].name)).toBeVisible()
  await expect(row(page, engines[1].name)).toBeHidden()
  expect(backend.rootRequests).toEqual([])
  await arrow(page, engines[0].name).click()
  await expect(arrow(page, 'empty directory')).toHaveClass(/is-leaf/)
  await arrow(page, 'schema 25').click()
  await row(page, 'customers 25').click()

  backend.root = root
  await page.locator('.tree-header button').click()
  await expect(row(page, 'empty directory')).toBeHidden()
  await expect(row(page, 'customers 25')).toBeVisible()
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(customerKey(25))

  backend.engines = []
  await page.locator('.tree-header button').click()
  await expect(row(page, engines[0].name)).toBeHidden()
  await expect(row(page, 'customers 25')).toBeHidden()
  await expect.poll(() => new URL(page.url()).searchParams.get('locator')).toBe(null)
  expect(backend.writes).toEqual([])
})
