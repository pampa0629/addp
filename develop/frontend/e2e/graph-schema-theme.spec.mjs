import { expect, test } from '@playwright/test'

test('graph schema themes recolor the canvas, legend and selection in place', async ({ page }) => {
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/e2e/fixtures/graph-schema-theme.html')
  const canvas = page.locator('.graph-container canvas')
  await expect(canvas).toBeVisible()
  const originalCanvas = await canvas.elementHandle()

  for (const theme of ['dark', 'blue', 'purple', 'light']) {
    await page.getByLabel('主题', { exact: true }).selectOption(theme)
    const colors = await page.locator('.graph-schema-view').evaluate(element => {
      const style = getComputedStyle(element)
      const resolve = variable => {
        const probe = document.createElement('span')
        probe.style.color = style.getPropertyValue(variable)
        element.append(probe)
        const color = getComputedStyle(probe).color
        probe.remove()
        return color
      }
      return { background: resolve('--addp-bg-primary'), secondary: resolve('--addp-bg-secondary'), text: resolve('--addp-text-secondary') }
    })
    await expect(page.locator('.graph-container')).toHaveCSS('background-color', colors.background)
    await expect(page.locator('.legend')).toHaveCSS('background-color', colors.secondary)
    await expect(page.locator('.legend')).toHaveCSS('color', colors.text)
    await expect.poll(() => page.locator('.graph-schema-view').evaluate(element => {
      const graph = element.__vueParentComponent.setupState.graphInstance
      const fontFamily = getComputedStyle(element).fontFamily
      return [...graph.getNodes(), ...graph.getEdges()].every(item =>
        item.getContainer().find(shape => shape.get('type') === 'text')?.attr('fontFamily') === fontFamily)
    })).toBe(true)
    // Inspect actual G6 pixels so a CSS-only fix cannot pass.
    await expect.poll(() => canvas.evaluate(element => {
      const color = getComputedStyle(element).getPropertyValue('--addp-graph-edge-default').trim()
      const rgb = color.slice(1).match(/../g).map(value => parseInt(value, 16))
      const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
      for (let i = 0; i < pixels.length; i += 4) {
        if (pixels[i + 3] === 255 && rgb.every((value, index) => pixels[i + index] === value)) return true
      }
      return false
    })).toBe(true)
    if (theme === 'dark') {
      await expect(async () => {
        const position = await canvas.evaluate(element => {
          const style = getComputedStyle(element)
          const palette = Array.from({ length: 12 }, (_, i) => style.getPropertyValue(`--addp-graph-category-${i + 1}`).trim().toLowerCase())
          const pixels = element.getContext('2d').getImageData(0, 0, element.width, element.height).data
          const bounds = element.getBoundingClientRect()
          for (let i = 0; i < pixels.length; i += 4) {
            const hex = '#' + [...pixels.slice(i, i + 3)].map(value => value.toString(16).padStart(2, '0')).join('')
            if (pixels[i + 3] === 255 && palette.includes(hex)) {
              return { x: (i / 4 % element.width) * bounds.width / element.width, y: Math.floor(i / 4 / element.width) * bounds.height / element.height + 8 }
            }
          }
          return null
        })
        expect(position).not.toBeNull()
        await canvas.click({ position })
        await expect(page.locator('.detail-panel')).toBeVisible()
      }).toPass()
    }
    await expect(page.locator('.detail-panel')).toHaveCSS('background-color', colors.background)
    await expect(page.locator('.detail-header strong')).toHaveText(await page.getByLabel('选中节点').innerText())
    expect(await canvas.evaluate((element, original) => element === original, originalCanvas)).toBe(true)
  }

  await page.setViewportSize({ width: 620, height: 600 })
  await expect.poll(() => canvas.evaluate(element => Math.abs(parseFloat(element.style.width) - element.parentElement.clientWidth))).toBeLessThan(2)
  await page.getByRole('button', { name: '显示或卸载图' }).click()
  await expect(canvas).toHaveCount(0)
  await page.getByLabel('主题', { exact: true }).selectOption('dark')
  await page.getByRole('button', { name: '显示或卸载图' }).click()
  await expect(canvas).toBeVisible()
  expect(errors).toEqual([])
})
