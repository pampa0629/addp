import { expect, test } from '@playwright/test'

const dialog = page => page.getByRole('dialog', { name: '有未保存的修改' })
async function openEditor(page) {
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.goto('/e2e/fixtures/leave-guard.html#/')
  await page.getByRole('button', { name: 'Open editor', exact: true }).click()
  const editor = page.frameLocator('iframe')
  await expect(editor.getByRole('textbox', { name: 'Draft' })).toBeVisible()
  return editor
}

test('Console leave protection cancels menu navigation and restores back/forward history', async ({ page }) => {
  const editor = await openEditor(page)
  await editor.getByRole('textbox').fill('unsaved')
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(dialog(page)).toBeVisible()
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(page).toHaveURL(/#\/orchestrator$/)
  await expect(editor.getByRole('textbox')).toHaveValue('unsaved')
  await page.evaluate(() => history.back())
  await expect(dialog(page)).toBeVisible()
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(page).toHaveURL(/#\/orchestrator$/)
  await page.evaluate(() => history.back())
  await dialog(page).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/#\/$/)
  await page.goForward()
  await expect(page).toHaveURL(/#\/orchestrator$/)
  await expect(page.frameLocator('iframe').getByRole('textbox')).toHaveValue('')
})

test('internal navigation prompts once and synchronizes the public route without reloading the frame', async ({ page }) => {
  const editor = await openEditor(page)
  await editor.getByRole('textbox').fill('unsaved')
  await editor.getByRole('button', { name: 'Internal leave' }).click()
  const childDialog = editor.getByRole('dialog')
  await expect(childDialog).toBeVisible()
  await expect(dialog(page)).toHaveCount(0)
  await childDialog.getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/#\/orchestrator\/list$/)
  await expect(editor.getByText('Editor list')).toBeVisible()
  await expect(dialog(page)).toHaveCount(0)
})

test('cross-module bridge waits for user confirmation beyond transport timeout and reports cancellation', async ({ page }) => {
  const editor = await openEditor(page)
  await editor.getByRole('textbox').fill('unsaved')
  await editor.getByRole('button', { name: 'Cross module' }).click()
  await expect(dialog(page)).toBeVisible()
  // A human decision may take longer than the 1500 ms transport timeout.
  await page.waitForTimeout(1700)
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(editor.locator('output')).toHaveText('false')
  await editor.getByRole('button', { name: 'Cross module' }).click()
  await dialog(page).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/#\/other$/)
})

test('saved and pristine editors do not prompt; inactive windows cannot mark Console dirty', async ({ page }) => {
  const editor = await openEditor(page)
  await editor.getByRole('textbox').fill('unsaved')
  await editor.getByRole('button', { name: 'Save', exact: true }).click()
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(page).toHaveURL(/#\/other$/)
  await page.evaluate(() => window.postMessage({ type: 'addp:unsaved-changes', active: true, dirty: true, id: 'forged' }, location.origin))
  await page.getByRole('button', { name: 'Third page' }).click()
  await expect(page).toHaveURL(/#\/third$/)
  await expect(dialog(page)).toHaveCount(0)
})

test('refresh uses native protection and saving clears it', async ({ page }) => {
  const editor = await openEditor(page)
  await editor.getByRole('textbox').fill('unsaved')
  const prompt = page.waitForEvent('dialog')
  await page.evaluate(() => { setTimeout(() => location.reload(), 0) })
  const unload = await prompt
  expect(unload.type()).toBe('beforeunload')
  await unload.dismiss()
  await expect(editor.getByRole('textbox')).toHaveValue('unsaved')
  await editor.getByRole('button', { name: 'Save', exact: true }).click()
  await page.reload()
  await expect(page.frameLocator('iframe').getByRole('textbox')).toHaveValue('')
})

test('overlapping navigation attempts share one confirmation and discard no state on cancel', async ({ page }) => {
  const editor = await openEditor(page)
  await editor.getByRole('textbox').fill('unsaved')
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(dialog(page)).toHaveCount(1)
  await page.getByRole('button', { name: 'Third page' }).dispatchEvent('click')
  await expect(dialog(page)).toHaveCount(1)
  await dialog(page).getByRole('button', { name: '继续编辑' }).click()
  await expect(dialog(page)).toHaveCount(0)
  await expect(page).toHaveURL(/#\/orchestrator$/)
  await expect(editor.getByRole('textbox')).toHaveValue('unsaved')
})

test('Console rejects a dirty-state message with the wrong origin', async ({ page }) => {
  await openEditor(page)
  await page.evaluate(() => window.dispatchEvent(new MessageEvent('message', {
    source: document.querySelector('iframe').contentWindow,
    origin: 'https://untrusted.invalid',
    data: { type: 'addp:unsaved-changes', active: true, dirty: true, id: 'forged' }
  })))
  await page.getByRole('button', { name: 'Other page' }).click()
  await expect(page).toHaveURL(/#\/other$/)
  await expect(dialog(page)).toHaveCount(0)
})
