import { expect, test } from '@playwright/test'
import { managerAuthContext } from './managerAuthContext.js'

for (const engineType of ['neo4j', 'falkordb']) {
  for (const viewport of [{ width: 1280, height: 720 }, { width: 1920, height: 1080 }]) {
    test(`${engineType} graph overview scrolls to the final sample at ${viewport.width}x${viewport.height}`, async ({ page }, testInfo) => {
      await page.setViewportSize(viewport)
      const overviewCount = viewport.width === 1280 ? 3 : 26
      const errors = []
      page.on('pageerror', error => errors.push(error.message))
      const graphPath = engineType === 'neo4j' ? 'neo4j/graph' : '0/fixture_graph'
      const locator = `addp://engine/2/path/${graphPath}?type=graph&item_id=8`
      const rootLocator = 'addp://engine/2/path/?type=server&node_id=1'
      const item = { id: locator, locator, label: 'Long graph preview', type: 'graph', hasChildren: false }
      const root = { id: rootLocator, locator: rootLocator, label: `${engineType} scroll fixture`, type: 'server', hasChildren: true, children: [item] }
      await page.addInitScript(() => {
        localStorage.setItem('addp-lang', 'zh-cn')
        localStorage.setItem('theme-mode', 'dark')
      })
      await page.route('**/plugins/manifest.json', route => json(route, { scripts: ['/plugins/table-preview.js'] }))
      await page.route('**/api/v1/**', route => {
        const path = new URL(route.request().url()).pathname
        if (path.endsWith('/system/refresh')) return json(route, { access_token: 'graph-scroll-fixture', expires_in: 3600 })
        if (path.endsWith('/system/users/me')) return json(route, { id: '1', display_name: 'scroll-reader', local_account: { username: 'scroll-reader' } })
        if (path.endsWith('/system/auth/context')) return json(route, managerAuthContext)
        if (path.endsWith('/manager/engines')) return json(route, { data: [{ id: 2, name: root.label, engine_type: engineType, lifecycle_state: 'active', connection_status: 'online' }] })
        if (path.endsWith('/ancestors')) return json(route, { target_locator: locator, ancestors: [{ ...root, children: [] }, item] })
        if (path.endsWith('/meta/resource-tree/2') || path.endsWith('/meta/resource-tree/2/node')) return json(route, root)
        if (path.endsWith('/manager/preview')) return json(route, { preview_type: 'table', data: {
          mode: 'table', preview_kind: 'graph_overview', columns: ['类型', '名称', '数量'], total: overviewCount,
          rows: Array.from({ length: overviewCount }, (_, index) => ({ 类型: '节点', 名称: `Shape-${index + 1}`, 数量: 24 })),
          graph: {
            nodes: Array.from({ length: 24 }, (_, index) => ({ element_id: `n${index}`, labels: ['Shape'], properties: { name: `Sample-${index + 1}`, value: index } })),
            relationships: Array.from({ length: 24 }, (_, index) => ({ element_id: `r${index}`, type: `LINK-${index + 1}`, start_node_id: `n${index}`, end_node_id: 'n0', properties: { note: `Relation-${index + 1}` } }))
          }
        } })
        return json(route, {})
      })
      await page.goto(`/data-explorer?locator=${encodeURIComponent(locator)}`)
      const content = page.locator('.preview-content')
      const tree = page.locator('.split-container > .tree-container')
      const treeBefore = await tree.boundingBox()
      const treeScrollBefore = await tree.evaluate(element => element.scrollTop)
      await expect(page.locator('.graph-sample-table').first().getByRole('cell', { name: 'Sample-24', exact: true })).toHaveCount(1)
      const box = await content.boundingBox()
      await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
      await page.mouse.wheel(0, 6000)
      await expect.poll(() => content.evaluate(element => element.scrollTop)).toBeGreaterThan(0)
      const last = page.getByRole('cell', { name: 'LINK-24', exact: true })
      await expect.poll(() => last.evaluate(element => {
        const box = element.getBoundingClientRect()
        return element.contains(document.elementFromPoint(box.x + box.width / 2, box.y + box.height / 2))
      })).toBe(true)
      await expect(page.locator('.panel-header')).toBeInViewport()
      expect(await tree.boundingBox()).toEqual(treeBefore)
      expect(await tree.evaluate(element => element.scrollTop)).toBe(treeScrollBefore)
      await testInfo.attach(`${engineType}-${viewport.width}-scroll-bottom.png`, { body: await page.screenshot(), contentType: 'image/png' })
      await page.mouse.wheel(0, -6000)
      await expect.poll(() => content.evaluate(element => element.scrollTop)).toBe(0)
      if (overviewCount > 20) {
        await page.getByRole('button', { name: '下一页', exact: true }).click()
        await expect(page.getByRole('cell', { name: 'Shape-26', exact: true })).toBeVisible()
      }
      expect(errors).toEqual([])
    })
  }
}

function json(route, body) {
  return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) })
}
