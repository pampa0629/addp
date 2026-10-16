import { expect, test } from '@playwright/test'
import { developAuthContext } from './developAuthContext.js'

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

for (const [locale, labels] of [
  ['zh-cn', ['活跃', '停用', '归档']],
  ['en', ['Active', 'Inactive', 'Archived']]
]) {
  test(`translates task status cells and filter options in ${locale}`, async ({ page }) => {
    await installMockBackend(page, { locale, statuses: ['active', 'inactive', 'archived'] })
    await page.goto('/tasks')

    const rows = page.locator('.el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(3)
    for (let index = 0; index < labels.length; index += 1) {
      await expect(rows.nth(index).getByRole('cell').nth(4)).toHaveText(labels[index])
    }
    await page.locator('.toolbar-right .el-select').last().click()
    await expect(page.getByRole('option')).toHaveText(labels)
    await expect(page.locator('body')).not.toContainText('develop.taskManagement.')
  })
}

async function installMockBackend(page, { locale = 'zh-cn', statuses } = {}) {
  let tasks = [
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

  if (statuses) {
    tasks = statuses.map((status, index) => ({
      ...tasks[0], id: 100 + index, name: `status_${status}`, status
    }))
  }

  await page.addInitScript(lang => localStorage.setItem('addp-lang', lang), locale)
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname

    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'develop-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'develop-e2e' })
    }
    if (path === '/api/v1/system/auth/context') return fulfillJSON(route, developAuthContext)
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
