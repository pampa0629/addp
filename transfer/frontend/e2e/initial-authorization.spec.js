import { test, expect } from '@playwright/test'

for (const language of ['zh-cn', 'en']) {
  for (const finalStatus of ['issued', 'denied']) {
    test(`successful output refreshes only authorization until ${finalStatus} in ${language}`, async ({ page }) => {
      const id = '11111111-1111-4111-8111-111111111111'
      await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), language)
      await page.clock.install()
      let reads = 0
      const writes = []
      await page.route('**/api/v1/**', route => {
        const request = route.request()
        const path = new URL(request.url()).pathname
        if (request.method() !== 'GET') { writes.push(path); return route.abort('blockedbyclient') }
        if (path === `/api/v1/transfer/executions/${id}`) {
          reads++
          return route.fulfill({ json: { execution_id: id, task_id: 900001, status: 'success', records_written: 4,
            metadata: { initial_authorization: { status: reads === 1 ? 'pending' : finalStatus } } } })
        }
        if (path === `/api/v1/transfer/executions/${id}/events`) return route.fulfill({ json: { items: [], has_more: false, next_cursor: 0 } })
        return route.abort('blockedbyclient')
      })
      await page.goto('/e2e/fixtures/config-check.html?execution=1')
      const feedback = page.getByTestId('initial-authorization-feedback')
      await expect(feedback).toContainText(language === 'en' ? 'Initial read authorization pending' : '初始读取授权待处理')
      await expect(feedback).toContainText(language === 'en' ? 'do not rerun' : '无需重新运行任务')
      await page.clock.runFor(5100)
      const title = language === 'en'
        ? (finalStatus === 'issued' ? 'Initial read authorization issued' : 'Initial read authorization not issued')
        : (finalStatus === 'issued' ? '初始读取授权已签发' : '初始读取授权未签发')
      await expect(feedback).toContainText(title)
      await page.clock.runFor(15000)
      expect(reads).toBe(2)
      expect(writes).toEqual([])
    })
  }
}
