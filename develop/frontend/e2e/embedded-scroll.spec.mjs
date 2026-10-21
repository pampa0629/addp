import { expect, test } from '@playwright/test'

for (const embedded of [false, true]) {
  test(`develop long page scrolls to its last entry (${embedded ? 'Console iframe' : 'standalone'})`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 560 })
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.addInitScript(() => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', 'dark')
    })
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const path = new URL(route.request().url()).pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'scroll-fixture', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: 1, username: 'scroll-reader', local_account: { username: 'scroll-reader' } })
      if (path.endsWith('/system/auth/context')) return json(route, { context: { type: 'tenant', tenant_id: '1' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['develop.task.read', 'meta.catalog.read'] }] } })
      if (path.endsWith('/develop/engines')) return json(route, [])
      if (path.endsWith('/develop/task-definitions')) return json(route, { items: Array.from({ length: 20 }, (_, index) => ({ id: index + 1, name: `Task ${index}`, dev_type: 'query', status: 'active', created_at: '2026-10-21T00:00:00Z' })), total: 20, page: 1, page_size: 20 })
      return json(route, {})
    })
    let frame = page
    if (embedded) {
      const shared = new URL('../../../common-frontend/basic/src/', import.meta.url).pathname
      await page.route('**/embedded-scroll-harness', route => route.fulfill({ contentType: 'text/html', body: `
        <style>html,body{margin:0;height:100%;overflow:hidden}iframe{display:block;width:100%;height:100%;border:0}</style>
        <script type="module">
          import { createIframeAuthCoordinator } from '/@fs${shared}auth/authSession.js'
          createIframeAuthCoordinator({ allowedOrigins:[location.origin], getToken:()=> 'scroll-fixture',
            getExpiresAt:()=> Date.now()+300000, refreshToken:async()=> 'scroll-fixture', logout:async()=>{} })
          const iframe = document.createElement('iframe')
          iframe.src = '/tasks'
          document.body.append(iframe)
        </script>` }))
      await page.goto('/embedded-scroll-harness')
      frame = page.frameLocator('iframe')
    } else {
      await page.goto('/tasks')
    }

    const rows = frame.locator('.el-table__body-wrapper tbody tr')
    await expect(rows).toHaveCount(20)
    const container = frame.locator(embedded ? '.content-only' : '.content')
    const box = await container.boundingBox()
    await page.mouse.move(box.x + box.width / 2, box.y + Math.min(250, box.height / 2))
    await page.mouse.wheel(0, 8000)
    await expect.poll(() => container.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
    await expect.poll(() => rows.last().evaluate(element => {
      const box = element.getBoundingClientRect()
      return element.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2))
    })).toBe(true)
    await testInfo.attach(`develop-${embedded ? 'embedded' : 'standalone'}-scroll-bottom.png`, { body: await page.screenshot(), contentType: 'image/png' })
    await page.mouse.wheel(0, -8000)
    await expect.poll(() => container.evaluate(element => element.scrollTop)).toBe(0)
    if (embedded) {
      const child = page.frames().find(candidate => candidate.parentFrame() === page.mainFrame())
      await child.goto('/sql')
      await expect(frame.locator('.query-workbench')).toBeVisible()
      await expect.poll(() => frame.locator('.content-only').evaluate(element => element.scrollHeight <= element.clientHeight + 1)).toBe(true)
    }
    expect(errors).toEqual([])
  })
}

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
