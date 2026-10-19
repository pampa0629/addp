import { expect, test } from '@playwright/test'

async function mockGraph(page) {
  const unexpected = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'graph-fixture-token', expires_in: 300 } })
    if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: 42, username: 'graph-reader' } })
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { type: 'user', id: '42' }, context: { type: 'tenant', tenant_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '7' }, permissions: ['graph.ontology.read'] }] }
    } })
    const ontology = { id: 1, name: '导航测试本体', status: 'active',
      entity_types: [{ id: 11, name: 'Node', label: 'Node', properties: [] }], relation_types: [] }
    if (path === '/api/v1/graph/ontologies') return route.fulfill({ json: [ontology] })
    if (path === '/api/v1/graph/ontologies/1') return route.fulfill({ json: ontology })
    if (path === '/api/v1/graph/ontologies/1/versions') return route.fulfill({ json: [] })
    unexpected.push(`${route.request().method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: `unexpected ${path}` } })
  })
  return unexpected
}

test('Graph list opens a stable ontology detail and synchronizes tabs without reloading', async ({ page }) => {
  const unexpected = await mockGraph(page)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/graph/ontologies')
  const graph = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(graph.getByText('导航测试本体', { exact: true })).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  const historyLength = await page.evaluate(() => history.length)
  await graph.getByRole('button', { name: '查看', exact: true }).click()
  await expect(page).toHaveURL('/graph/ontologies/1')
  await expect(graph.getByRole('heading', { name: '导航测试本体' })).toBeVisible()
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  expect(await page.evaluate(() => history.length)).toBe(historyLength + 1)
  await graph.getByRole('tab', { name: '关系类型', exact: true }).click()
  await expect(page).toHaveURL('/graph/ontologies/1?tab=relations')
  await expect(graph.getByRole('tab', { name: '关系类型', exact: true })).toHaveAttribute('aria-selected', 'true')
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  expect(await page.evaluate(() => history.length)).toBe(historyLength + 1)
  await page.goBack()
  await expect(page).toHaveURL('/graph/ontologies')
  await expect(graph.getByText('导航测试本体', { exact: true })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL('/graph/ontologies/1?tab=relations')
  await expect(graph.getByRole('tab', { name: '关系类型', exact: true })).toHaveAttribute('aria-selected', 'true')
  await page.reload()
  await expect(graph.getByRole('heading', { name: '导航测试本体' })).toBeVisible()
  await expect(graph.getByRole('tab', { name: '关系类型', exact: true })).toHaveAttribute('aria-selected', 'true')
  expect(unexpected).toEqual([])
  expect(errors).toEqual([])
})

test('Graph ontology name opens the graph tab, shares its URL and returns to the list', async ({ page, context }) => {
  const unexpected = await mockGraph(page)
  await page.goto('/graph/ontologies')
  const graph = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(graph.getByText('导航测试本体', { exact: true })).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  await graph.getByText('导航测试本体', { exact: true }).click()
  await expect(page).toHaveURL('/graph/ontologies/1')
  await expect(graph.getByRole('heading', { name: '导航测试本体' })).toBeVisible()
  await graph.getByRole('tab', { name: '图形视图', exact: true }).click()
  await expect(page).toHaveURL('/graph/ontologies/1?tab=graph')
  await expect(graph.locator('.ontology-view canvas')).toBeVisible()
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  const sharedPage = await context.newPage()
  const sharedUnexpected = await mockGraph(sharedPage)
  await sharedPage.goto(page.url())
  const sharedGraph = sharedPage.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(sharedGraph.getByRole('tab', { name: '图形视图', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(sharedGraph.locator('.ontology-view canvas')).toBeVisible()
  await sharedPage.close()
  await graph.getByRole('button', { name: '返回', exact: true }).click()
  await expect(page).toHaveURL('/graph/ontologies')
  await expect(graph.getByRole('button', { name: '查看', exact: true })).toBeVisible()
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
  expect([...unexpected, ...sharedUnexpected]).toEqual([])
})
