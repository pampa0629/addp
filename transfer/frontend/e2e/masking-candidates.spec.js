import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'

const messages = Object.fromEntries(['zh-cn', 'en'].map(language => [language, {
  task: JSON.parse(readFileSync(new URL(`../src/i18n/${language}.json`, import.meta.url), 'utf8')).transfer.taskWizard,
  choices: JSON.parse(readFileSync(new URL(`../../../common-frontend/basic/src/i18n/${language}.json`, import.meta.url), 'utf8')).fieldMaskingChoices
}]))
const fields = identity => [{
  component_key: 'phone', target: { resource_identity: identity }, policy_id: 'synthetic-policy',
  assessment_revision: 1, policy_revision: 1, options: [{ id: `${identity}-option`, name: `${identity} masking option` }]
}]
const tick = page => page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))))

async function setup(page, language) {
  const pending = [], writes = []
  await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url())
    if (request.method() === 'GET' && url.pathname === '/api/v1/transfer/source-protection-options') {
      pending.push({ route, locator: url.searchParams.get('source_locator') })
      return
    }
    writes.push({ method: request.method(), path: url.pathname })
    return route.abort('blockedbyclient')
  })
  await page.goto('/e2e/fixtures/config-check.html?masking')
  const panel = page.locator('.field-masking-choices')
  const toggle = page.getByRole('button', { name: messages[language].task.exportMaskingTitle, exact: true })
  await toggle.click()
  await expect.poll(() => pending.length).toBe(1)
  expect(pending[0].locator).toBe('addp://engine/9000001/path/synthetic/source?type=table')
  return { pending, writes, panel, toggle, text: messages[language].choices }
}
const respond = (request, identity) => request.route.fulfill({ json: { fields: fields(identity) } })

for (const language of ['zh-cn', 'en']) {
  test(`masking candidates keep the new source and preserve the old saved choice (${language})`, async ({ page }) => {
    const h = await setup(page, language)
    await respond(h.pending[0], 'first')
    await h.panel.locator('.el-select').click()
    await page.getByRole('option', { name: 'first masking option', exact: true }).click()
    await expect(page.getByTestId('masking-saved-choices')).toContainText('first-option')
    await h.panel.getByRole('button', { name: h.text.refresh, exact: true }).click()
    await expect.poll(() => h.pending.length).toBe(2)
    await page.getByTestId('masking-source-second').click()
    await expect.poll(() => h.pending.length).toBe(3)
    expect(h.pending[2].locator).toBe('addp://engine/9000001/path/synthetic/second?type=table')
    await respond(h.pending[2], 'second')
    await expect(h.panel.getByText(`phone: ${h.text.stale}`, { exact: true })).toBeVisible()
    await respond(h.pending[1], 'first'); await tick(page)
    await h.panel.locator('.el-select').click()
    await expect(page.getByRole('option', { name: 'second masking option', exact: true })).toBeVisible()
    await expect(page.getByRole('option', { name: 'first masking option', exact: true })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await expect(page.getByTestId('masking-saved-choices')).toContainText('first-option')
    await h.panel.getByRole('button', { name: h.text.remove, exact: true }).click()
    await expect(page.getByTestId('masking-saved-choices')).toHaveText('[]')
    expect(h.writes).toEqual([])
  })

  test(`obsolete refusal leaves the replacement candidates loading (${language})`, async ({ page }) => {
    const h = await setup(page, language)
    await page.getByTestId('masking-source-second').click()
    await expect.poll(() => h.pending.length).toBe(2)
    await h.pending[0].route.fulfill({ status: 409, json: { error: 'obsolete protection refusal' } }); await tick(page)
    await expect(h.panel.getByRole('button', { name: h.text.refresh, exact: true })).toBeDisabled()
    await expect(h.panel.getByText('obsolete protection refusal')).toHaveCount(0)
    await respond(h.pending[1], 'second')
    await expect(h.panel.getByRole('button', { name: h.text.refresh, exact: true })).toBeEnabled()
    await h.panel.locator('.el-select').click()
    await expect(page.getByRole('option', { name: 'second masking option', exact: true })).toBeVisible()
    expect(h.writes).toEqual([])
  })

  test(`collapsed candidates ignore the old failure and current refusal can recover (${language})`, async ({ page }) => {
    const h = await setup(page, language)
    await h.toggle.click()
    await page.getByTestId('masking-source-second').click()
    await h.pending[0].route.fulfill({ status: 409, json: { error: 'hidden obsolete refusal' } }); await tick(page)
    expect(h.pending.length).toBe(1)
    await h.toggle.click()
    await expect.poll(() => h.pending.length).toBe(2)
    await h.pending[1].route.fulfill({ status: 409, json: { error: 'current protection refusal' } })
    await expect(h.panel.getByText('current protection refusal')).toBeVisible()
    await expect(h.panel.getByText('hidden obsolete refusal')).toHaveCount(0)
    await h.panel.getByRole('button', { name: h.text.refresh, exact: true }).click()
    await expect.poll(() => h.pending.length).toBe(3)
    await respond(h.pending[2], 'second')
    await expect(h.panel.getByText('current protection refusal')).toHaveCount(0)
    await h.panel.locator('.el-select').click()
    await expect(page.getByRole('option', { name: 'second masking option', exact: true })).toBeVisible()
    expect(h.writes).toEqual([])
  })
}
