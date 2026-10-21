import { expect, test } from '@playwright/test'

for (const embedded of [false, true]) {
  test(`orchestrator long page scrolls to its last entry (${embedded ? 'Console iframe' : 'standalone'})`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width: 1280, height: 560 })
    const errors = []
    page.on('pageerror', error => errors.push(error.stack))
    await page.addInitScript(() => {
      localStorage.setItem('addp-lang', 'zh-cn')
      localStorage.setItem('theme-mode', 'dark')
    })
    await page.route('**/plugins/manifest.json', route => json(route, { scripts: [] }))
    await page.route('**/api/v1/**', route => {
      const path = new URL(route.request().url()).pathname
      if (path.endsWith('/system/refresh')) return json(route, { access_token: 'scroll-fixture', expires_in: 3600 })
      if (path.endsWith('/system/users/me')) return json(route, { id: 1, username: 'scroll-reader', local_account: { username: 'scroll-reader' } })
      if (path.endsWith('/system/auth/context')) return json(route, { context: { type: 'tenant', tenant_id: '1' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['orchestrator.workflow.read', 'orchestrator.workflow.create', 'orchestrator.workflow.execute', 'system.execution_authorization.create'] }] } })
      if (path.endsWith('/orchestrator/task-providers')) return json(route, [])
      if (path.endsWith('/orchestrator/tasks')) return json(route, { items: [], total: 0 })
      if (path.endsWith('/orchestrator/orchestrations/scroll-workspace')) return json(route, { id: 'scroll-workspace', name: 'Scroll workspace', steps: [], editor_layout: {} })
      if (path.endsWith('/orchestrator/orchestrations')) return json(route, Array.from({ length: 20 }, (_, index) => ({ id: `fixture-${index}`, name: `Orchestration ${index}`, enabled: false, schedule: '', steps: [] })))
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
          iframe.src = '/orchestrations'
          document.body.append(iframe)
        </script>` }))
      await page.goto('/embedded-scroll-harness')
      frame = page.frameLocator('iframe')
    } else {
      await page.goto('/orchestrations')
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
    await testInfo.attach(`orchestrator-${embedded ? 'embedded' : 'standalone'}-scroll-bottom.png`, { body: await page.screenshot(), contentType: 'image/png' })
    await page.mouse.wheel(0, -8000)
    await expect.poll(() => container.evaluate(element => element.scrollTop)).toBe(0)
    if (embedded) {
      const child = page.frames().find(candidate => candidate.parentFrame() === page.mainFrame())
      await child.goto('/orchestrations/scroll-workspace/edit')
      await expect(frame.locator('.orchestration-form')).toBeVisible()
      await expect(frame.locator('#dag-container canvas')).toBeVisible()
      await expect.poll(() => frame.locator('.content-only').evaluate(element => element.scrollHeight <= element.clientHeight + 1)).toBe(true)
    }
    expect(errors).toEqual([])
  })
}

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
