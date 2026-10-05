import { expect, test } from '@playwright/test'
import { HOST_NODE_ID, mockHostNodesAPI } from '../../../system/frontend/e2e/host-nodes.fixture'

test('node details use the Console address, restore filters, and keep one iframe document', async ({ page }) => {
  const { calls } = await mockHostNodesAPI(page)
  await page.goto('/system/host-nodes?search=Node&page=2')
  const system = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(system.getByRole('cell', { name: 'Node A', exact: true })).toBeVisible()
  const frame = page.frames().find(frame => frame.parentFrame())
  const initialDocument = await frame.evaluate(() => performance.timeOrigin)
  await system.getByRole('row').filter({ hasText: 'Node A' }).getByRole('button', { name: '节点详情', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/system/host-nodes/${HOST_NODE_ID}\\?search=Node&page=2$`))
  await expect(system.getByTestId('node-name')).toHaveValue('Node A')
  await page.screenshot({ path: '/tmp/addp-host-node-console-detail.png', fullPage: true, animations: 'disabled' })
  expect(await frame.evaluate(() => performance.timeOrigin)).toBe(initialDocument)
  await page.reload()
  await expect(system.getByTestId('node-name')).toHaveValue('Node A')
  await system.getByRole('button', { name: '关闭', exact: true }).click()
  await expect(page).toHaveURL('http://127.0.0.1:4170/system/host-nodes?search=Node&page=2')
  await expect(system.getByRole('textbox', { name: '搜索节点名称或地址' })).toHaveValue('Node')
  await expect(system.getByRole('textbox', { name: '搜索节点名称或地址' })).toBeFocused()
  expect(calls.filter(call => call.path.endsWith('/host_nodes')).at(-1).query).toEqual({ search: 'Node', page: '2', page_size: '20' })
})
