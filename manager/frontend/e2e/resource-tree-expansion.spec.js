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
  const state = { holdChildren: false, releaseChildren: null }
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
      if (match[2] === '/ancestors') return send({ ancestors: [root(id), branch(id)], target_locator: branchKey(id) })
      if (match[2] === '/node') {
        if (state.holdChildren) await new Promise(resolve => { state.releaseChildren = resolve })
        return send(branch(id, true))
      }
      return send(root(id))
    }
    return send({})
  })
  return state
}

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
