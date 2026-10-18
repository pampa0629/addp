import { expect } from '@playwright/test'

export async function expectSourceFactsLayout(facts, { engineName, engineType, itemName, itemType, fullName }) {
  const value = label => facts.locator('td.el-descriptions__label').filter({ hasText: label })
    .locator('xpath=following-sibling::td[1]')
  for (const name of [engineName, engineType, itemName, itemType, fullName]) {
    expect(typeof name).toBe('string')
    expect(name.trim()).not.toBe('')
  }
  await expect(value('来源引擎')).toContainText(engineName)
  await expect(value('来源引擎')).toContainText(engineType)
  await expect(value('来源数据项')).toContainText(itemName)
  await expect(value('来源数据项')).toContainText(itemType)
  await expect(value('来源完整路径')).toHaveText(fullName)
  const geometry = await facts.evaluate(element => {
    const cells = [...element.querySelector('tr').querySelectorAll('td')]
    const rect = element.getBoundingClientRect()
    return { left: cells[1].getBoundingClientRect().width, right: cells[3].getBoundingClientRect().width,
      width: rect.width, x: rect.x, end: rect.right, viewport: innerWidth,
      overflow: [...element.querySelectorAll('td')].some(cell => cell.scrollWidth > cell.clientWidth + 1) }
  })
  expect(Math.abs(geometry.left - geometry.right)).toBeLessThanOrEqual(2)
  expect(geometry.right).toBeGreaterThan(geometry.width / 5)
  expect(geometry.x).toBeGreaterThanOrEqual(0)
  expect(geometry.end).toBeLessThanOrEqual(geometry.viewport)
  expect(geometry.overflow).toBe(false)
}

export async function expectFingerprintHover(catalog, facts, fingerprint) {
  const identity = facts.locator('.source-identity')
  await expect(identity).toHaveText(fingerprint)
  const rendered = await identity.evaluate(element => ({
    width: element.clientWidth, fullWidth: element.scrollWidth,
    overflow: getComputedStyle(element).overflowX,
    ellipsis: getComputedStyle(element).textOverflow,
    wrapping: getComputedStyle(element).whiteSpace
  }))
  expect(rendered.width).toBeGreaterThan(0)
  expect(rendered.fullWidth).toBeGreaterThan(rendered.width)
  expect(rendered).toMatchObject({ overflow: 'hidden', ellipsis: 'ellipsis', wrapping: 'nowrap' })
  await identity.hover()
  const tooltip = catalog.getByRole('tooltip', { name: fingerprint, exact: true })
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toHaveText(fingerprint)
  expect(await tooltip.evaluate(element => {
    const rect = element.getBoundingClientRect()
    return rect.x >= 0 && rect.right <= innerWidth && element.scrollWidth <= element.clientWidth + 1
  })).toBe(true)
  await facts.getByText('来源模块', { exact: true }).hover()
  await expect(tooltip).not.toBeVisible()
}

export async function expectFingerprintFocus(catalog, facts, fingerprint) {
  const identity = facts.locator('.source-identity')
  await identity.focus()
  await expect(identity).toBeFocused()
  const tooltip = catalog.getByRole('tooltip', { name: fingerprint, exact: true })
  await expect(tooltip).toBeVisible()
  await expect(tooltip).toHaveText(fingerprint)
  await identity.press('Tab')
  await expect(tooltip).not.toBeVisible()
}
