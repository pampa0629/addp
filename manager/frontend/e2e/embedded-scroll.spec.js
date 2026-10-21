import { expect, test } from '@playwright/test'

for (const embedded of [false, true]) {
  test(`manager long page scrolls to its last entry (${embedded ? 'Console iframe' : 'standalone'})`, async ({ page }, testInfo) => {
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
      if (path.endsWith('/system/auth/context')) return json(route, { context: { type: 'tenant', tenant_id: '1' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '1' }, permissions: ['manager.search.execute', 'manager.data_item.read'] }] } })
      if (path.endsWith('/manager/engines')) return json(route, { data: [] })
      if (path.endsWith('/manager/search/history')) return json(route, { data: { items: [] } })
      if (path.endsWith('/manager/search')) return json(route, { data: { total: 20, page: 1, page_size: 20, results: Array.from({ length: 20 }, (_, index) => ({ document_id: `item-${index}`, name: `Result ${index}`, file_name: `Result ${index}`, full_name: `fixture.Result${index}`, engine_id: 2, locator: `addp://engine/2/path/fixture/Result${index}?type=table&item_id=${index + 1}`, match_methods: ['keyword'] })) } })
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
          iframe.src = '/data-retrieval'
          document.body.append(iframe)
        </script>` }))
      await page.goto('/embedded-scroll-harness')
      frame = page.frameLocator('iframe')
    } else {
      await page.goto('/data-retrieval')
    }
    await frame.locator('.search-input input').fill('fixture')
    await frame.getByRole('button', { name: '搜索', exact: true }).click()
    const rows = frame.locator('.result-item')
    await expect(rows).toHaveCount(20)
    const container = frame.locator(embedded ? '.content-only' : '.main-content')
    const box = await container.boundingBox()
    await page.mouse.move(box.x + box.width / 2, box.y + Math.min(250, box.height / 2))
    await page.mouse.wheel(0, 8000)
    await expect.poll(() => container.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
    await expect.poll(() => rows.last().evaluate(element => {
      const box = element.getBoundingClientRect()
      return element.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2))
    })).toBe(true)
    await testInfo.attach(`manager-${embedded ? 'embedded' : 'standalone'}-scroll-bottom.png`, { body: await page.screenshot(), contentType: 'image/png' })
    await page.mouse.wheel(0, -8000)
    await expect.poll(() => container.evaluate(element => element.scrollTop)).toBe(0)
    if (embedded) {
      const child = page.frames().find(candidate => candidate.parentFrame() === page.mainFrame())
      await child.goto('/data-explorer')
      await expect(frame.locator('.data-explorer')).toBeVisible()
      await expect.poll(() => frame.locator('.content-only').evaluate(element => element.scrollHeight <= element.clientHeight + 1)).toBe(true)
    }
    expect(errors).toEqual([])
  })
}

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
