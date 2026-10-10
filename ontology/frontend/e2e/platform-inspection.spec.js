import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { installBackend } from './fixture.js'

const root = '/ontology/platform/definitions'
const detail = `${root}/transfer.task.create`
const definition = JSON.parse(readFileSync(new URL('../../backend/internal/platform/transfer.json', import.meta.url)))
const review = JSON.parse(readFileSync(new URL('../../backend/internal/platform/transfer.review.json', import.meta.url)))
const release = { context:{...definition,digest:'e'.repeat(64)}, review }
async function fixture(context, options = {}) {
  const backend = await installBackend(context, { contextType:'platform', permissions:['ontology.platform_definition.read'], ...options })
  const state = { backend, reads:[], hold:null, expireOnce:false, fail:false }
  await context.route('**/api/v1/ontology/platform/definitions**', async route => {
    const request = route.request(), url = new URL(request.url())
    state.reads.push(url.pathname)
    expect(request.method()).toBe('GET')
    expect(url.search).toBe('')
    if (state.hold) await state.hold
    if (state.expireOnce) { state.expireOnce = false; return route.fulfill({status:401,json:{error:'expired'}}) }
    if (state.fail) return route.fulfill({status:500,json:{error:'Frozen release unavailable'}})
    return route.fulfill({ json:url.pathname.endsWith('/transfer.task.create') ? release : {capabilities:[release.context]} })
  })
  return state
}

test('readonly inspection restores a selected release, graph, coverage and all evidence at narrow width', async ({ page, context }) => {
  const state = await fixture(context)
  await page.goto('/ontology/')
  await expect(page).toHaveURL(new RegExp(`${root}$`))
  await page.getByRole('button',{name:'transfer.task.create',exact:true}).click()
  await expect(page).toHaveURL(new RegExp(`${detail}$`))
  await expect(page.getByTestId('platform-release')).toContainText('e'.repeat(64))
  await expect(page.getByTestId('platform-graph').locator('canvas')).toHaveCount(1)
  await expect(page.getByTestId('platform-bindings').locator('tbody tr')).toHaveCount(49)
  await expect(page.getByTestId('platform-source')).toHaveCount(9)
  await expect(page.getByText('未建模',{exact:true})).toHaveCount(4)
  await page.reload()
  await expect(page.getByTestId('platform-release')).toContainText('transfer-generation')
  await page.setViewportSize({width:640,height:900})
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  expect(state.backend.writes).toEqual([])
  expect(state.backend.unexpected).toEqual([])
})

test('English and dark theme reuse translated evidence and the shared graph', async ({page,context}) => {
  const state = await fixture(context,{locale:'en'})
  await context.addInitScript(() => localStorage.setItem('theme-mode','dark'))
  await page.goto(detail)
  await expect(page.getByRole('heading',{name:'Platform ontology inspection',exact:true})).toBeVisible()
  await expect(page.getByText('Not modeled',{exact:true})).toHaveCount(4)
  await expect(page.getByTestId('platform-graph').locator('canvas')).toHaveCount(1)
  await expect(page.getByTestId('platform-source')).toHaveCount(9)
  expect(state.backend.writes).toEqual([])
})

test('platform sidebar enters the real iframe, synchronizes capability selection and returns without reload', async ({ page, context }) => {
  const state = await fixture(context)
  await page.goto('/ontology/e2e/host.html?context=platform#/other')
  await page.getByRole('menuitem',{name:'领域本体',exact:true}).click()
  await page.getByRole('menuitem',{name:'平台本体核实',exact:true}).click()
  const frame = page.frameLocator('iframe')
  await frame.getByRole('button',{name:'transfer.task.create',exact:true}).click()
  await expect(page).toHaveURL(new RegExp(`#${detail}$`))
  await expect(frame.getByTestId('platform-release')).toBeVisible()
  const runtime = page.frames().find(item => item.url().includes('/platform/definitions'))
  await runtime.evaluate(() => { window.platformInspectionMarker = 'same' })
  await frame.getByRole('button',{name:'返回',exact:true}).click()
  await expect(page).toHaveURL(new RegExp(`#${root}$`))
  expect(await runtime.evaluate(() => window.platformInspectionMarker)).toBe('same')
  expect(state.backend.writes).toEqual([])
  expect(state.backend.unexpected).toEqual([])
})

test('Tenant cannot enter inspection even with a misplaced read candidate', async ({page,context}) => {
  const state = await fixture(context,{contextType:'tenant'})
  await page.goto(detail)
  await expect(page).toHaveURL(/forbidden$/)
  expect(state.reads).toEqual([])
})

for (const change of ['permission','context','auth_failure']) {
  test(`reauthorization clears frozen evidence on ${change}`, async ({page,context}) => {
    const state = await fixture(context)
    await page.goto(detail)
    await expect(page.getByTestId('platform-source')).toHaveCount(9)
    if (change === 'permission') state.backend.authContext.authorization.role_assignments = []
    if (change === 'context') state.backend.authContext.context.type = 'tenant'
    if (change === 'auth_failure') state.backend.authFailure = true
    state.expireOnce = true
    await page.getByRole('button',{name:'重新加载',exact:true}).click()
    await expect.poll(() => state.backend.refreshCount).toBe(2)
    await expect(page.getByTestId('platform-source')).toHaveCount(0)
    await expect(page.getByTestId('platform-release')).toHaveCount(0)
    expect(state.backend.writes).toEqual([])
  })
}

test('leaving a pending detail read prevents a late result from replacing the catalog', async ({page,context}) => {
  const state = await fixture(context)
  await page.goto(detail)
  await expect(page.getByTestId('platform-release')).toBeVisible()
  let resolve
  state.hold = new Promise(done => { resolve = done })
  const reads = state.reads.length
  await page.getByRole('button',{name:'重新加载',exact:true}).click()
  await expect.poll(() => state.reads.length).toBeGreaterThan(reads)
  await page.getByRole('button',{name:'返回',exact:true}).click()
  state.hold = null
  resolve()
  await expect(page.getByTestId('platform-catalog')).toBeVisible()
  await expect(page.getByTestId('platform-source')).toHaveCount(0)
  await expect(page.getByTestId('platform-release')).toHaveCount(0)
})

test('backend read failure leaves no stale definition and no embedded fallback', async ({page,context}) => {
  const state = await fixture(context)
  await page.goto(detail)
  await expect(page.getByTestId('platform-release')).toBeVisible()
  state.fail = true
  await page.getByRole('button',{name:'重新加载',exact:true}).click()
  await expect(page.getByText('Frozen release unavailable',{exact:true})).toBeVisible()
  await expect(page.getByTestId('platform-source')).toHaveCount(0)
})
