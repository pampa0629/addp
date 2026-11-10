import assert from 'node:assert/strict'

// Capture the first frame with a visible title, before a theme transition can finish.
export function observeFirstHeading(page, title) {
  return page.addInitScript(text => {
    const observe = () => {
      const heading = [...document.querySelectorAll('h1,h2,h3')].find(element =>
        element.textContent.trim() === text && element.getBoundingClientRect().height > 0 &&
        getComputedStyle(element).visibility === 'visible')
      if (!heading) {
        requestAnimationFrame(observe)
        return
      }

      const style = getComputedStyle(heading)
      const themeColor = style.getPropertyValue('--addp-text-primary').trim()
      const canvas = document.createElement('canvas').getContext('2d')
      canvas.fillStyle = style.color
      const actual = canvas.fillStyle
      canvas.fillStyle = themeColor
      window.firstHeadingSample = {
        actual,
        expected: CSS.supports('color', themeColor) ? canvas.fillStyle : '',
        dark: document.documentElement.classList.contains('dark')
      }
    }
    requestAnimationFrame(observe)
  }, title)
}

export async function assertDarkFirstHeading(heading) {
  const sample = await heading.evaluate(() => new Promise(resolve => {
    requestAnimationFrame(() => resolve(window.firstHeadingSample))
  }))
  assert.ok(sample, 'The first visible heading must be sampled')
  assert.equal(sample.dark, true, 'Dark theme must be active when the heading first appears')
  assert.equal(sample.actual, sample.expected, 'The first visible heading must use the theme foreground')
}
