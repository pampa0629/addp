import { expect, test } from '@playwright/test'

test('keeps every task action on one line', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/tasks')

  await expect(page.getByRole('heading', { name: '任务管理', exact: true })).toBeVisible()
  await expect(page.locator('.task-actions')).toHaveCount(2)

  const actionRows = await page.evaluate(() => (
    Array.from(document.querySelectorAll('.task-actions')).map(container => {
      const containerRect = container.getBoundingClientRect()
      const buttonRects = Array.from(container.querySelectorAll('button')).map(button => {
        const rect = button.getBoundingClientRect()
        return { top: rect.top, right: rect.right }
      })
      const style = getComputedStyle(container)
      return {
        buttonCount: buttonRects.length,
        buttonTops: buttonRects.map(rect => Math.round(rect.top)),
        maxButtonRight: Math.max(...buttonRects.map(rect => rect.right)),
        containerRight: containerRect.right,
        flexWrap: style.flexWrap,
        whiteSpace: style.whiteSpace
      }
    })
  ))

  for (const row of actionRows) {
    expect(row.buttonCount).toBe(4)
    expect(new Set(row.buttonTops).size).toBe(1)
    expect(row.maxButtonRight).toBeLessThanOrEqual(row.containerRight + 1)
    expect(row.flexWrap).toBe('nowrap')
    expect(row.whiteSpace).toBe('nowrap')
  }
})

async function installMockBackend(page) {
  const tasks = [
    {
      id: 41,
      name: 'farm_buffer',
      dev_type: 'workflow',
      engine_id: null,
      status: 'active',
      last_executed_at: null,
      created_at: '2026-08-04T15:36:49+08:00'
    },
    {
      id: 39,
      name: 'test',
      dev_type: 'script',
      engine_id: null,
      status: 'active',
      last_executed_at: null,
      created_at: '2026-08-02T21:25:59+08:00'
    }
  ]

  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'develop-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'develop-e2e' })
    }
    if (path === '/api/v1/develop/task-definitions') {
      return fulfillJSON(route, { items: tasks, total: tasks.length, page: 1, page_size: 20 })
    }
    if (path === '/api/v1/develop/engines') {
      return fulfillJSON(route, [])
    }

    return fulfillJSON(route, {})
  })
}

async function fulfillJSON(route, body) {
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
