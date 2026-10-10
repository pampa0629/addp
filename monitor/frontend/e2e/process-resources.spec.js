import { expect, test } from '@playwright/test'
import { identity, setIdentity } from './monitoring-targets.fixture'

import { backend } from './process-resources.fixture'

test('service resources use current System instances, local explanations and correct process units', async ({ page }) => {
  const state = await backend(page)
  await page.goto('/service-resources')
  const list = page.getByTestId('process-list')
  await expect(list).toContainText('1.50 核')
  await expect(list).toContainText('0.00 核')
  await expect(list).toContainText('1.00 GiB')
  await expect(list).toContainText('1小时')
  await expect(list).toContainText('未关联主机')
  await expect(list).toContainText('采样时间')
  await expect(list).toContainText('1 核 ≈ 占满一个逻辑核，可超过 1 核')
  expect(state.calls).toEqual(['1,2'])
  state.setMode('warmup')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(list).toContainText('等待完整一分钟有效采样')
  await expect(list).toContainText('1.00 GiB')
})

test('failed evidence clears metrics but keeps the authorized instance list; owner denial clears both', async ({ page }) => {
  const state = await backend(page)
  await page.goto('/service-resources')
  await expect(page.getByTestId('process-list')).toContainText('1.50 核')
  for (const mode of ['unavailable', 'wrong-owner']) {
    state.setMode(mode)
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await expect(page.getByTestId('process-error')).toBeVisible()
    await expect(page.getByTestId('process-list')).toContainText('mac-process-1')
    await expect(page.getByTestId('process-list')).not.toContainText('1.50 核')
  }
  state.setMode('denied')
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByTestId('process-list')).not.toContainText('mac-process-1')
})

test('revoked permissions cannot receive a delayed service response', async ({ page }) => {
  const state = await backend(page, { held: true })
  await page.goto('/service-resources')
  await expect(page.getByTestId('process-list')).toContainText('mac-process-1')
  await expect.poll(() => state.calls.length).toBe(1)
  await setIdentity(page, identity([]))
  state.release()
  await expect(page.getByText('当前身份无权查看服务监控。', { exact: false })).toBeVisible()
  await expect(page.getByTestId('process-list')).toHaveCount(0)
})

test('service filters and paging restore the exact System scope without manual targets', async ({ page }) => {
  const state = await backend(page, { locale: 'en' })
  await page.goto('/service-resources?module_name=monitor&role=worker&status=up')
  await expect(page.getByTestId('process-list')).toContainText('1.50 cores')
  expect(state.listCalls[0]).toMatchObject({ module_name: 'monitor', role: 'worker', status: 'up', page_size: '20' })
  await page.locator('.el-pagination button.btn-next').click()
  await expect(page).toHaveURL(/page=2/)
  await expect(page.getByTestId('process-list')).not.toContainText('mac-process-1')
  await page.reload()
  expect(state.listCalls.at(-1).page).toBe('2')
  expect(state.calls).toHaveLength(1)
})
