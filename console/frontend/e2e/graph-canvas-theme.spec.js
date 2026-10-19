import { expect, test } from '@playwright/test'
import { mockGraph } from './graph.fixture'
import { graphBrowserDiagnostic, graphCanvasHasThemePixels } from './graph-theme.fixture.js'

test('Graph browser retains selection, viewport and readable shapes across Console themes', async ({ page }) => {
  const errors = []
  let stage = 'graph:load'
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => {
    if (['warning', 'error'].includes(message.type())) errors.push(graphBrowserDiagnostic(message, stage, ['graph-fixture-token']))
  })
  const unexpected = await mockGraph(page, {
    permissions: ['graph.graph.read'],
    responses: {
      '/api/v1/graph/graphs/7': { id: 7, name: '主题测试图谱' },
      '/api/v1/graph/graphs/7/browse-snapshot': {
        schema: { node_shapes: [{ name: 'Person', labels: ['Person'] }], relationship_shapes: [{ type: 'Link', directed: true }] },
        stats: { node_count: 2, relationship_count: 1 },
        overview: {
          nodes: [
            { id: 'a', labels: ['Person'], display_name: '节点甲', properties: { name: '节点甲' } },
            { id: 'b', labels: ['Person'], display_name: '节点乙', properties: { name: '节点乙' } }
          ],
          edges: [{ id: 'ab', source: 'a', target: 'b', type: 'Link', directed: true }]
        }
      }
    }
  })
  await page.goto('/graph/graphs/7/browse')
  const graph = page.frameLocator('iframe[data-testid="module-iframe"]')
  const view = graph.locator('.graph-canvas')
  await expect(view.locator('canvas')).toBeVisible()
  await expect(view.locator('.el-loading-mask')).toHaveCount(0)
  await expect.poll(() => view.evaluate(element => element.__vueParentComponent.setupState.graphInstance?.get('group').getMatrix() !== null)).toBe(true)
  await graph.getByRole('button', { name: '关系名', exact: true }).click()
  const snapshot = () => view.evaluate(element => {
    const instance = element.__vueParentComponent.setupState.graphInstance
    return { matrix: instance.get('group').getMatrix(), positions: instance.getNodes().map(node => ({ id: node.getID(), x: node.getModel().x, y: node.getModel().y })) }
  })
  const position = await view.evaluate(element => {
    const instance = element.__vueParentComponent.setupState.graphInstance
    const node = instance.getNodes()[0].getModel()
    return instance.getCanvasByPoint(node.x, node.y)
  })
  await view.locator('canvas').click({ position })
  await expect(graph.locator('.node-panel .panel-name')).toHaveText('节点甲')
  const before = await snapshot()
  const frame = page.frames().find(frame => frame.parentFrame())
  const documentID = await frame.evaluate(() => performance.timeOrigin)
  for (const [mode, label] of [['light', '浅色模式'], ['dark', '深色模式'], ['blue', '蓝色模式'], ['purple', '紫色模式']]) {
    stage = `theme:${mode}`
    await page.getByRole('button', { name: /^当前主题:/ }).click()
    await page.getByRole('menuitem', { name: label, exact: true }).click()
    await expect.poll(() => graph.locator('html').evaluate(element => ({
      dark: element.classList.contains('dark'), blue: element.classList.contains('blue'), purple: element.classList.contains('purple')
    }))).toEqual({ dark: mode !== 'light', blue: mode === 'blue', purple: mode === 'purple' })
    await expect.poll(() => view.evaluate(element => {
      const instance = element.__vueParentComponent.setupState.graphInstance
      const css = getComputedStyle(element)
      const color = name => css.getPropertyValue(name).trim()
      return instance.getNodes().every(node => node.getKeyShape().attr('stroke') === color(node.hasState('selected') ? '--addp-graph-selection' : '--addp-graph-related')) &&
        instance.getEdges().every(edge => {
          const text = edge.getContainer().find(shape => shape.get('type') === 'text')
          return text?.attr('fill') === color('--addp-graph-edge-label') && text?.attr('stroke') === color('--addp-graph-edge-label-stroke')
        })
    })).toBe(true)
    expect(await snapshot()).toEqual(before)
    await expect(graph.locator('.node-panel .panel-name')).toHaveText('节点甲')
    expect(await frame.evaluate(() => performance.timeOrigin)).toBe(documentID)
    await view.locator('canvas').click({ position: { x: 5, y: 5 } })
    await expect(graph.locator('.node-panel')).toHaveCount(0)
    await expect.poll(() => view.evaluate(element => {
      const instance = element.__vueParentComponent.setupState.graphInstance
      const stroke = getComputedStyle(element).getPropertyValue('--addp-graph-node-stroke').trim()
      return instance.getNodes().every(node => node.getKeyShape().attr('stroke') === stroke)
    })).toBe(true)
    await expect.poll(() => view.evaluate(graphCanvasHasThemePixels)).toBe(true)
    await view.locator('canvas').click({ position })
    await expect(graph.locator('.node-panel .panel-name')).toHaveText('节点甲')
  }
  expect(unexpected).toEqual([])
  expect(errors).toEqual([])
})
