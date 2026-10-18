import { expect } from '@playwright/test'

export async function assertOntologyThemes(page) {
  const view = page.locator('.ontology-view')
  const canvas = view.locator('canvas')
  await expect(canvas).toBeVisible()
  const original = await canvas.elementHandle()
  const point = await view.evaluate(element => {
    const graph = element.__vueParentComponent.setupState.graphInstance
    const node = graph.getNodes()[0].getModel()
    return graph.getCanvasByPoint(node.x, node.y)
  })
  await canvas.click({ position: point })
  await expect(view.locator('.node-detail')).toBeVisible()
  const detail = await view.locator('.node-detail strong').innerText()
  await view.getByRole('button', { name: '放大', exact: true }).click()
  const snapshot = () => view.evaluate(element => {
    const graph = element.__vueParentComponent.setupState.graphInstance
    return { matrix: graph.get('group').getMatrix(), positions: graph.getNodes().map(node => ({ id: node.getID(), x: node.getModel().x, y: node.getModel().y })) }
  })
  const before = await snapshot()
  for (const theme of ['light', 'dark', 'blue', 'purple']) {
    await page.evaluate(theme => { document.documentElement.className = theme === 'light' ? '' : `dark ${theme === 'dark' ? '' : theme}` }, theme)
    await expect.poll(() => view.evaluate(element => {
      const graph = element.__vueParentComponent.setupState.graphInstance
      const style = getComputedStyle(element)
      const color = variable => style.getPropertyValue(variable).trim()
      return graph.getNodes().every(node => {
        const model = node.getModel()
        const fill = model._meta.color || color('--el-color-primary')
        const label = node.getContainer().find(shape => shape.get('name') === 'text-shape')
        return node.getKeyShape().attr('fill') === fill && node.getKeyShape().attr('stroke') === color('--addp-border-color') &&
          label?.attr('fill') === (fill.toLowerCase() === '#ffffff' ? color('--addp-graph-label-dark') : model.labelCfg.style.fill)
      }) && graph.getEdges().every(edge => {
        const model = edge.getModel()
        const inherited = model.id.startsWith('inherit_')
        const expected = model.id === 'rel_c' ? '#123456' : color(inherited ? '--addp-text-tertiary' : '--addp-graph-edge-default')
        const label = edge.getContainer().find(shape => shape.get('name') === 'text-shape')
        return edge.getKeyShape().attr('stroke') === expected && edge.getKeyShape().attr('endArrow').fill === expected &&
          label?.attr('fill') === color(inherited ? '--addp-text-tertiary' : '--addp-graph-edge-label')
      })
    })).toBe(true)
    expect(await snapshot()).toEqual(before)
    expect(await canvas.evaluate((element, original) => element === original, original)).toBe(true)
    await expect(view.locator('.node-detail strong')).toHaveText(detail)
  }
}
