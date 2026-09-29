import { test, expect } from '@playwright/test'
import { applicationPath, runtimePath, installMetricApplicationBackend } from './fixtures/metricApplication.js'

test('zero-permission account cannot load management URLs or request Workbench APIs', async ({ page, context }) => {
  await installMetricApplicationBackend(context, { permissions: [] })
  const workbenchRequests = []
  page.on('request', request => {
    if (new URL(request.url()).pathname.startsWith('/api/v1/workbench/')) workbenchRequests.push(request.url())
  })

  for (const path of ['/module-ui/workbench/', '/module-ui/workbench/applications', '/module-ui/workbench/applications/new', applicationPath]) {
    await page.goto(path)
    await expect(page).toHaveURL(/\/module-ui\/workbench\/forbidden$/)
    await expect(page.locator('.module-access-denied')).toBeVisible()
  }
  expect(workbenchRequests).toEqual([])
})

test('read-only Workbench account cannot open creation or editing URLs', async ({ page, context }) => {
  await installMetricApplicationBackend(context, { permissions: ['workbench.data_application.read'] })
  const workbenchRequests = []
  page.on('request', request => {
    if (new URL(request.url()).pathname.startsWith('/api/v1/workbench/')) workbenchRequests.push(request.url())
  })

  await page.goto('/module-ui/workbench/')
  await expect(page).toHaveURL(/\/module-ui\/workbench\/applications$/)
  await expect(page.getByTestId('data-application-list')).toBeVisible()
  workbenchRequests.length = 0

  for (const path of ['/module-ui/workbench/applications/new', applicationPath]) {
    await page.goto(path)
    await expect(page).toHaveURL(/\/module-ui\/workbench\/forbidden$/)
    await expect(page.locator('.module-access-denied')).toBeVisible()
  }
  expect(workbenchRequests).toEqual([])
})

test('delivery opens the Console same-origin runtime with Workbench assets', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context)
  await page.goto('/module-ui/workbench/applications')
  await expect(page.getByTestId('data-application-list').getByText('指标应用回归')).toBeVisible()

  await page.getByRole('button', { name: '交付', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('指标应用回归')).toBeVisible()
  const opened = context.waitForEvent('page')
  await dialog.getByRole('button', { name: '运行', exact: true }).click()
  const runtime = await opened
  await expect(runtime).toHaveURL(runtimePath)
  await expect(runtime.getByTestId('data-application-runtime')).toBeVisible()
  await expect(runtime.getByTestId('runtime-component')).toHaveCount(2)
  expect(backend.unexpected).toEqual([])
})

test('a creator with read permission but no execute permission sees no delivery action and cannot run the URL', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, {
    permissions: ['workbench.data_application.read'],
  })
  await page.goto('/module-ui/workbench/applications')
  await expect(page.getByTestId('data-application-list').getByText('指标应用回归')).toBeVisible()
  await expect(page.getByRole('button', { name: '交付', exact: true })).toHaveCount(0)

  const denied = page.waitForResponse(response => response.url().endsWith(`/data_applications/${backend.draft.id}/runtime`))
  await page.goto(runtimePath)
  expect((await denied).status()).toBe(403)
  await expect(page).toHaveURL(runtimePath)
  await expect(page.getByText('暂时无法运行这个数据应用')).toBeVisible()
  await expect(page.getByTestId('runtime-open-portal-action')).toBeVisible()
  await expect(page.getByTestId('runtime-component')).toHaveCount(0)
  expect(backend.unexpected).toEqual([])
})

test('an execute-only consumer needs a current application grant on each runtime load', async ({ page, context }) => {
  const backend = await installMetricApplicationBackend(context, {
    permissions: ['workbench.data_application.execute'],
    runtimeGranted: false,
  })
  const runtimeResponse = () => page.waitForResponse(response => response.url().endsWith(`/data_applications/${backend.draft.id}/runtime`))

  const deniedBeforeGrant = runtimeResponse()
  await page.goto(runtimePath)
  expect((await deniedBeforeGrant).status()).toBe(403)
  await expect(page.getByText('暂时无法运行这个数据应用')).toBeVisible()

  backend.setRuntimeGranted(true)
  const allowed = runtimeResponse()
  await page.reload()
  expect((await allowed).status()).toBe(200)
  await expect(page.getByTestId('runtime-component')).toHaveCount(2)

  backend.setRuntimeGranted(false)
  const deniedAfterRevoke = runtimeResponse()
  await page.reload()
  expect((await deniedAfterRevoke).status()).toBe(403)
  await expect(page).toHaveURL(runtimePath)
  await expect(page.getByText('暂时无法运行这个数据应用')).toBeVisible()
  await expect(page.getByTestId('runtime-component')).toHaveCount(0)
  expect(backend.unexpected).toEqual([])
})
