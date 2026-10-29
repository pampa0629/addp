import { expect, test } from '@playwright/test'
import { resourceBackend } from './node-resources.fixture'
import { engineBackend } from './engine-resources.fixture'
import { backend as processBackend } from './process-resources.fixture'
import { fixture as alertBackend } from './platform-alerts.fixture'

for (const entry of [
  { name: 'host', backend: resourceBackend, path: '/node-resources', ready: 'resource-node-list', input: '搜索主机名称或地址', key: 'search', value: 'Node' },
  { name: 'engine', backend: engineBackend, path: '/engine-resources', ready: 'engine-list', input: '搜索部署名称', key: 'search', value: 'PostgreSQL' },
  { name: 'service', backend: processBackend, path: '/service-resources', ready: 'process-list', input: '模块名称（精确匹配）', key: 'module_name', value: 'meta' },
  { name: 'alert', backend: alertBackend, path: '/platform-alerts', ready: 'pipeline-incidents', input: '告警名称', key: 'search', value: 'cpu' }
]) test(`${entry.name} Enter submits before the text debounce and never submits during IME composition`, async ({ page }) => {
  await entry.backend(page)
  await page.clock.install()
  await page.goto(`${entry.path}?refresh=off`)
  await expect(page.getByTestId(entry.ready)).toBeVisible()
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000))
  const input = page.getByRole('textbox', { name: entry.input, exact: true })
  await input.dispatchEvent('compositionstart')
  await input.fill(entry.value)
  await input.dispatchEvent('keydown', { key: 'Enter', code: 'Enter', isComposing: true })
  expect(new URL(page.url()).searchParams.has(entry.key)).toBe(false)
  await input.dispatchEvent('compositionend')
  await input.press('Enter')
  await expect(page).toHaveURL(url => url.searchParams.get(entry.key) === entry.value)
  const url = page.url()
  await page.clock.fastForward(301)
  await expect(page).toHaveURL(url)
})
