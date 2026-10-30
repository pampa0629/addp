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

test.describe('last execution from the task definition contract', () => {
  test.use({ timezoneId: 'Asia/Shanghai' })

  for (const [locale, notExecuted] of [['zh-cn', '未执行'], ['en', 'Not executed']]) {
    test(`renders last_run_at and keeps unexecuted tasks distinct in ${locale}`, async ({ page }) => {
      const writes = []
      page.on('request', request => {
        if (new URL(request.url()).pathname.startsWith('/api/v1/develop/') && request.method() !== 'GET') {
          writes.push(`${request.method()} ${request.url()}`)
        }
      })
      await installMockBackend(page, {
        locale,
        taskItems: [
          { id: 101, name: 'completed_workflow', dev_type: 'workflow', status: 'active', last_run_at: '2026-10-30T04:00:00Z', last_execution_status: 'success' },
          { id: 102, name: 'failed_workflow', dev_type: 'workflow', status: 'active', last_run_at: '2026-10-30T04:01:00Z', last_execution_status: 'failed' },
          { id: 103, name: 'never_run_omitted', dev_type: 'workflow', status: 'active' },
          { id: 104, name: 'never_run_null', dev_type: 'workflow', status: 'active', last_run_at: null }
        ]
      })
      await page.goto('/tasks')

      const rows = page.locator('.el-table__body-wrapper tbody tr')
      await expect(rows).toHaveCount(4)
      for (const [index, time, status] of [[0, '2026/10/30 12:00:00', 'success'], [1, '2026/10/30 12:01:00', 'failed']]) {
        const execution = rows.nth(index).getByRole('cell').nth(5)
        await expect(execution).toContainText(time)
        await expect(execution.locator('.el-tag')).toHaveText(status)
        await expect(execution).not.toContainText(notExecuted)
      }
      for (const index of [2, 3]) {
        const execution = rows.nth(index).getByRole('cell').nth(5)
        await expect(execution).toHaveText(notExecuted)
        await expect(execution.locator('.el-tag')).toHaveCount(0)
      }
      expect(writes).toEqual([])
    })
  }
})

for (const embedded of [false, true]) {
  for (const width of [1280, 1024]) {
    // 1024 CSS pixels also covers a 1280-pixel window at 125% browser zoom.
    test(`keeps task columns reachable without action overlays (${embedded ? 'iframe' : 'standalone'}, ${width})`, async ({ page }, testInfo) => {
      await page.setViewportSize({ width, height: 576 })
      await installMockBackend(page, {
        taskItems: Array.from({ length: 20 }, (_, index) => ({
          id: index + 1, name: `completed_task_${index}`, dev_type: 'workflow', status: 'active',
          last_run_at: '2026-10-30T04:00:00Z', last_execution_status: 'success',
          created_at: '2026-10-29T04:00:00Z'
        }))
      })
      let surface = page
      if (embedded) {
        const shared = new URL('../../../common-frontend/basic/src/', import.meta.url).pathname
        await page.route('**/task-columns-harness', route => route.fulfill({ contentType: 'text/html', body: `
          <style>html,body{margin:0;height:100%;overflow:hidden}iframe{display:block;width:100%;height:100%;border:0}</style>
          <script type="module">
            import { createIframeAuthCoordinator } from '/@fs${shared}auth/authSession.js'
            createIframeAuthCoordinator({allowedOrigins:[location.origin],getToken:()=> 'develop-e2e-token',
              getExpiresAt:()=> Date.now()+300000,refreshToken:async()=> 'develop-e2e-token',logout:async()=>{}})
            const frame=document.createElement('iframe');frame.src='/tasks';document.body.append(frame)
          </script>` }))
        await page.goto('/task-columns-harness')
        surface = page.frameLocator('iframe')
      } else {
        await page.goto('/tasks')
      }
      const rows = surface.locator('.el-table__body-wrapper tbody tr')
      await expect(rows).toHaveCount(20)
      const execution = rows.first().getByRole('cell').nth(5)
      await execution.scrollIntoViewIfNeeded()
      await expect.poll(() => isUncovered(execution)).toBe(true)
      await expect(execution).toContainText('success')

      for (const control of [surface.getByRole('heading', { name: '任务管理', exact: true }), surface.locator('.toolbar-right .el-select').last()]) {
        await expect.poll(() => control.evaluate(element => {
          const rect = element.getBoundingClientRect()
          return rect.left >= 0 && rect.right <= window.innerWidth + 1
        })).toBe(true)
      }
      await testInfo.attach(`task-execution-${embedded}-${width}.png`, { body: await page.screenshot(), contentType: 'image/png' })

      const table = surface.locator('.el-table__body-wrapper .el-scrollbar__wrap')
      const overflows = await table.evaluate(element => element.scrollWidth > element.clientWidth + 1)
      if (width === 1024) expect(overflows).toBe(true)
      const box = await execution.boundingBox()
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
      await page.mouse.wheel(2000, 0)
      if (overflows) await expect.poll(() => table.evaluate(element => element.scrollLeft)).toBeGreaterThan(0)
      else await expect.poll(() => table.evaluate(element => element.scrollLeft)).toBe(0)
      const actions = rows.first().locator('.task-actions button')
      await expect(actions).toHaveCount(4)
      for (const action of await actions.all()) await expect.poll(() => isUncovered(action)).toBe(true)

      const container = surface.locator(embedded ? '.content-only' : '.content')
      await page.mouse.wheel(0, 8000)
      await expect.poll(() => container.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
      await expect.poll(() => isUncovered(rows.last().locator('.task-actions'))).toBe(true)
      await testInfo.attach(`task-columns-${embedded}-${width}.png`, { body: await page.screenshot(), contentType: 'image/png' })
    })
  }
}

function isUncovered(locator) {
  return locator.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return [rect.left + 4, (rect.left + rect.right) / 2, rect.right - 4].every(x =>
      element.contains(document.elementFromPoint(x, (rect.top + rect.bottom) / 2)))
  })
}

async function installMockBackend(page, { locale = 'zh-cn', statuses, taskItems } = {}) {
  let tasks = taskItems ?? [
    {
      id: 41,
      name: 'farm_buffer',
      dev_type: 'workflow',
      engine_id: null,
      status: 'active',
      last_run_at: null,
      created_at: '2026-08-04T15:36:49+08:00'
    },
    {
      id: 39,
      name: 'test',
      dev_type: 'script',
      engine_id: null,
      status: 'active',
      last_run_at: null,
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
