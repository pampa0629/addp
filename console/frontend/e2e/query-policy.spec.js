import { expect, test } from '@playwright/test'

async function openPolicy(page, scope = 'platform') {
 await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
 let saved = { version: 1, default_query_timeout: 30, max_query_timeout: 300, query_result_limit: 500, query_concurrency: 20, query_per_engine_concurrency: 5 }
 const writes = []
 await page.route('**/develop/settings/query-policy', async route => {
  if (route.request().method() === 'PUT') {
   const input = route.request().postDataJSON()
   writes.push(input)
   saved = { ...saved, ...input, version: saved.version + 1 }
  }
  await route.fulfill({ json: saved })
 })
 await page.goto('/e2e/fixtures/query-policy.html?scope=' + scope)
 await expect(page.getByRole('spinbutton').first()).toHaveValue('30')
 return writes
}

test('platform saves hot concurrency with the version and keeps the form within a narrow viewport', async ({ page }) => {
 await page.setViewportSize({ width: 480, height: 900 })
 const writes = await openPolicy(page)
 const fields = page.getByRole('spinbutton')
 await expect(fields).toHaveCount(5)
 await expect(fields.nth(3)).toHaveValue('20')
 await expect(fields.nth(4)).toHaveValue('5')
 await fields.nth(3).fill('12')
 await fields.nth(4).fill('3')
 await page.getByRole('button', { name: '保存', exact: true }).click()
 await expect.poll(() => writes.length).toBe(1)
 expect(writes[0]).toEqual({ version: 1, default_query_timeout: 30, max_query_timeout: 300, query_result_limit: 500, query_concurrency: 12, query_per_engine_concurrency: 3 })
 await expect(page.getByText('保存后无需重启。', { exact: false })).toBeVisible()
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
})

test('tenant edits only its timeout and does not submit shared capacity', async ({ page }) => {
 const writes = await openPolicy(page, 'tenant')
 await expect(page.getByRole('spinbutton')).toHaveCount(1)
 await page.getByRole('spinbutton').fill('60')
 await page.getByRole('button', { name: '保存', exact: true }).click()
 await expect.poll(() => writes.length).toBe(1)
 expect(writes[0]).toEqual({ version: 1, default_query_timeout: 60 })
})
