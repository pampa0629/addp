import { expect, test } from '@playwright/test'
import { mockModuleQueryAPI, moduleQueryLink } from './module-query.fixture'

test('standalone instance links restore filters, ranges and pagination after reload and history navigation', async ({ page }) => {
  const queries = await mockModuleQueryAPI(page)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto(`/modules${moduleQueryLink}`)
  const list = page.locator('.module-instances')
  await expect(page.getByRole('tab', { name: '服务实例' })).toHaveAttribute('aria-selected', 'true')
  await expect(list.getByText('page-2-0', { exact: true })).toBeVisible()
  await expect(list.getByPlaceholder('登记主机名或 IP')).toHaveValue('manager.local')
  await expect(list.getByPlaceholder('宿主节点或运行环境主机名')).toHaveValue('host-a')
  await expect.poll(() => queries.at(-1)).toEqual({
    module_name: 'manager', registered_host: 'manager.local', node_name: 'host-a', role: 'backend',
    registered_from: '2026-10-01T00:00:00.000Z', registered_to: '2026-10-02T00:00:00.000Z', page: '2', page_size: '20'
  })
  await page.reload()
  await expect(list.getByText('page-2-0', { exact: true })).toBeVisible()
  const historyLength = await page.evaluate(() => history.length)
  await list.getByPlaceholder('登记主机名或 IP').fill('host-b')
  await list.getByPlaceholder('登记主机名或 IP').press('Enter')
  await expect.poll(() => new URL(page.url()).searchParams.get('registered_host')).toBe('host-b')
  await expect(list.getByText('page-1-0', { exact: true })).toBeVisible()
  expect(new URL(page.url()).searchParams.has('page')).toBe(false)
  expect(await page.evaluate(() => history.length)).toBe(historyLength)
  const restoredURL = page.url()
  await page.goto('/modules')
  await expect(page.getByRole('tab', { name: '模块概览' })).toHaveAttribute('aria-selected', 'true')
  await page.goBack()
  await expect(page).toHaveURL(restoredURL)
  await expect(list.getByPlaceholder('登记主机名或 IP')).toHaveValue('host-b')
  await page.goForward()
  await expect(page.getByRole('tab', { name: '模块概览' })).toHaveAttribute('aria-selected', 'true')
  await page.goto('/modules?tab=instances&status=invalid&page=-1&registered_period=custom&registered_from=invalid&token=secret')
  await expect(page).toHaveURL(/\/modules\?tab=instances$/)
  await expect.poll(() => queries.at(-1)).toEqual({ page: '1', page_size: '10', status: 'up' })
  expect(errors).toEqual([])
})
