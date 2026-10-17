import { browserTestOrigin } from '../../../common-frontend/basic/src/utils/browserTestIsolation.mjs'
import { expect, test } from '@playwright/test'
import { HOST_NODE_ID, mockHostNodesAPI } from '../../../system/frontend/e2e/host-nodes.fixture'

for (const [query, expectedQuery] of [
  ['search=Node&page=2', { search: 'Node', page: '2', page_size: '20' }],
  ['search=Node&node_kind=virtual&enabled=true&page_size=50', { search: 'Node', node_kind: 'virtual', enabled: 'true', page: '1', page_size: '50' }]
]) test(`node details restore Console filters with one iframe document (${query})`, async ({ page }) => {
  const { calls } = await mockHostNodesAPI(page)
  await page.goto(`/system/host-nodes?${query}`)
  const system = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(system.getByRole('cell', { name: 'Node A', exact: true })).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const initialDocument = await frame.evaluate(() => performance.timeOrigin)
  await system.getByRole('row').filter({ hasText: 'Node A' }).getByRole('button', { name: '主机详情', exact: true }).click()
  await expect(page).toHaveURL(`${browserTestOrigin('console')}/system/host-nodes/${HOST_NODE_ID}?${query}`)
  await expect(system.getByTestId('node-name')).toHaveValue('Node A')
  await page.screenshot({ path: '/tmp/addp-host-node-console-detail.png', fullPage: true, animations: 'disabled' })
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(initialDocument)
  await page.reload()
  await expect(system.getByTestId('node-name')).toHaveValue('Node A')
  await system.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL(`${browserTestOrigin('console')}/system/host-nodes?${query}`)
  await expect(system.getByRole('textbox', { name: '搜索主机名称或地址' })).toHaveValue('Node')
  await expect(system.getByRole('textbox', { name: '搜索主机名称或地址' })).toBeFocused()
  expect(calls.filter(call => call.path.endsWith('/host_nodes')).at(-1).query).toEqual(expectedQuery)
})
