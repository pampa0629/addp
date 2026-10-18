import { test, expect } from '@playwright/test'

for (const entry of ['vue', 'wc']) {
  test(`DAG ${entry} entry recolors nodes, ports and arrows in place and owns its lifecycle`, async ({ page }) => {
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.goto(`/module-ui/agent/e2e/dag-theme.html${entry === 'wc' ? '?wc' : ''}`)
    const viewer = page.locator('.dag-viewer'), canvas = viewer.locator('canvas')
    await expect(canvas).toBeVisible()
    expect(await viewer.evaluate(element => element.__vueParentComponent.setupState.graph.getZoom())).toBeLessThanOrEqual(1)
    const originalCanvas = await canvas.elementHandle()
    await canvas.hover()
    await page.mouse.wheel(0, -120)
    const snapshot = () => viewer.evaluate(element => {
      const graph = element.__vueParentComponent.setupState.graph
      return { matrix: graph.get('group').getMatrix(), nodes: graph.getNodes().map(node => ({ id: node.getID(), x: node.getModel().x, y: node.getModel().y })) }
    })
    const original = await snapshot()
    for (const theme of ['light', 'dark', 'blue', 'purple']) {
      await page.evaluate(theme => { document.documentElement.className = theme === 'light' ? '' : `dark ${theme === 'dark' ? '' : theme}` }, theme)
      await expect.poll(() => viewer.evaluate(element => {
        const graph = element.__vueParentComponent.setupState.graph
        const style = getComputedStyle(element)
        const color = variable => style.getPropertyValue(variable).trim()
        return graph.getNodes().every(node => {
          const shape = name => node.getContainer().find(shape => shape.get('name') === name)
          return shape('main-rect').attr('fill') === color('--el-color-primary') &&
            shape('main-rect').attr('stroke') === color('--addp-border-color') &&
            [color('--addp-graph-label-dark'), color('--addp-graph-label-light')].includes(shape('node-label').attr('fill')) &&
            shape('input-port').attr('stroke') === color('--el-color-success') &&
            shape('output-port-default').attr('stroke') === color('--el-color-warning') &&
            shape('input-port').attr('fill') === color('--addp-bg-primary')
        }) && graph.getEdges().every(edge => edge.getKeyShape().attr('stroke') === color('--addp-graph-edge-default') && edge.getKeyShape().attr('endArrow').fill === color('--addp-graph-edge-default'))
      })).toBe(true)
      expect(await snapshot()).toEqual(original)
      expect(await canvas.evaluate((element, original) => element === original, originalCanvas)).toBe(true)
    }
    await page.getByRole('button', { name: '清空', exact: true }).click()
    await expect.poll(() => viewer.evaluate(element => element.__vueParentComponent.setupState.graph.getNodes().length)).toBe(0)
    await page.getByRole('button', { name: '加载', exact: true }).click()
    await expect.poll(() => viewer.evaluate(element => element.__vueParentComponent.setupState.graph.getEdges().length)).toBe(1)
    await page.setViewportSize({ width: 640, height: 700 })
    await expect.poll(() => canvas.evaluate(element => Math.abs(parseFloat(element.style.width) - element.parentElement.clientWidth))).toBeLessThan(2)
    await page.getByRole('button', { name: '挂载或卸载' }).click()
    await expect(canvas).toHaveCount(0)
    // A hidden remount must wait for measurable dimensions, without a delayed timer creating a detached canvas.
    await page.getByRole('button', { name: '隐藏或显示' }).click()
    await page.getByRole('button', { name: '挂载或卸载' }).click()
    await expect(canvas).toHaveCount(0)
    await page.evaluate(() => { document.documentElement.className = 'dark' })
    await page.getByRole('button', { name: '隐藏或显示' }).click()
    await expect(canvas).toBeVisible()
    expect(errors).toEqual([])
  })
}
