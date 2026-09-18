import { test, expect } from '@playwright/test'
import { installBackend, root, editor } from './fixture.js'

test('real ontology iframe synchronizes public URL without reload and protects Console exit', async ({
  page,
  context
}) => {
  const state = await installBackend(context)
  await page.goto(`/ontology/e2e/host.html#${root}`)
  const frame = page.frameLocator('iframe')
  await frame
    .getByRole('button', { name: 'beijing_outdoor', exact: true })
    .click()
  await expect(page).toHaveURL(new RegExp(`#${root}/beijing_outdoor$`))
  await frame.getByRole('button', { name: '1', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`#${editor}$`))
  const runtimeFrame = page
    .frames()
    .find((f) => f.url().includes('/ontologies'))
  await runtimeFrame.evaluate(() => {
    window.ontologyFrameMarker = 'same-document'
  })
  await frame.getByRole('tab', { name: /^属性/ }).click()
  await expect(page).toHaveURL(new RegExp(`#${editor}\\?tab=properties$`))
  expect(await runtimeFrame.evaluate(() => window.ontologyFrameMarker)).toBe(
    'same-document'
  )
  await frame.getByRole('button', { name: '添加属性', exact: true }).click()
  await page.getByRole('button', { name: 'Other module', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '继续编辑', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`#${editor}\\?tab=properties$`))
  await expect(frame.getByTestId('properties-member')).toHaveCount(1)
  expect(state.unexpected).toEqual([])
})
