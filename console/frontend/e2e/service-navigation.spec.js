import { expect, test } from '@playwright/test'

const hostURL = '/e2e/fixtures/leave-guard.html?editor=service-navigation'
const standaloneURL = '/e2e/service-fixture/e2e/navigation.html'
const entries = [
  { label: '查询服务', path: '/query-services', heading: '查询服务' },
  { label: '瓦片服务', path: '/tile', heading: '瓦片服务' },
  { label: '图查询服务', path: '/graph-services', heading: '图查询服务' },
  { label: '服务注册', path: '/services', heading: '注册服务' },
  { label: '服务目录', path: '/catalog', heading: '服务目录' }
]
const registered = {
  id: 49, service_name: 'fixture_registered', title: 'Fixture registered service',
  service_type: 'rest', endpoint_url: 'https://example.invalid/fixture',
  auth_type: 'none', status: 'active', created_at: '2026-09-01T00:00:00Z'
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => { throw error })
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    if (request.method() === 'GET') {
      if (path === '/api/v1/service/registered/49') return route.fulfill({ json: registered })
      if (['query', 'tile', 'graph', 'registered'].some(kind => path === `/api/v1/service/${kind}`)) {
        const data = path.endsWith('/registered') ? [registered] : []
        return route.fulfill({ json: { data, total: data.length, page: 1, limit: 20, offset: 0 } })
      }
    }
    await route.abort()
    throw new Error(`Unexpected API request: ${request.method()} ${path}`)
  })
})

async function expectEmbedded(page, entry, path = entry.path, heading = entry.heading) {
  await expect(page).toHaveURL(`${hostURL}#/service${path}`)
  const frame = page.frameLocator('iframe')
  await expect(frame.getByRole('heading', { name: heading, exact: true })).toBeVisible()
  await expect(page.getByRole('menubar')).toHaveCount(1)
  await expect(page.getByRole('menuitem', { name: entry.label, exact: true })).toHaveClass(/is-active/)
  // App and Layout both run here: an accidental second sidebar must fail this check.
  await expect(frame.getByRole('menubar')).toHaveCount(0)
  await expect(frame.locator('.sidebar, .layout > .header')).toHaveCount(0)
  await expect(page.getByRole('menuitem').filter({ hasNot: page.getByRole('menu') })).toHaveText(entries.map(item => item.label))
  return frame
}

for (const entry of entries) {
  test(`Console ${entry.label} menu keeps one navigation through reload, back and forward`, async ({ page }) => {
    const start = entries[entry.path === '/catalog' ? 0 : 4]
    await page.goto(`${hostURL}#/service${start.path}`)
    await expectEmbedded(page, start)
    await page.getByRole('menuitem', { name: entry.label, exact: true }).click()
    await expectEmbedded(page, entry)
    await page.reload()
    await expectEmbedded(page, entry)
    await page.goBack()
    await expectEmbedded(page, start)
    await page.goForward()
    await expectEmbedded(page, entry)
  })
}

for (const [button, entry] of [['查询服务管理', entries[0]], ['注册服务管理', entries[3]]]) {
  test(`catalog ${button} uses the canonical route without reloading the iframe or duplicating history`, async ({ page }) => {
    await page.goto(`${hostURL}#/service/catalog`)
    const frame = await expectEmbedded(page, entries[4])
    // A document marker survives router navigation but not a hidden iframe reload.
    await frame.locator('body').evaluate(element => element.dataset.navigationDocument = 'catalog')
    const historyLength = await page.evaluate(() => history.length)
    await frame.getByRole('button', { name: button, exact: true }).click()
    await expectEmbedded(page, entry)
    await expect(frame.locator('body')).toHaveAttribute('data-navigation-document', 'catalog')
    expect(await page.evaluate(() => history.length)).toBe(historyLength + 1)
    await page.goBack()
    await expectEmbedded(page, entries[4])
    await page.goForward()
    await expectEmbedded(page, entry)
  })
}

test('registered catalog card opens the real detail under the registry menu and returns to its canonical list', async ({ page }) => {
  await page.goto(`${hostURL}#/service/catalog`)
  const frame = await expectEmbedded(page, entries[4])
  await frame.getByRole('heading', { name: registered.title, exact: true }).click()
  await expectEmbedded(page, entries[3], '/services/49', registered.title)
  await page.reload()
  await expectEmbedded(page, entries[3], '/services/49', registered.title)
  await frame.getByRole('button', { name: '← 返回', exact: true }).click()
  await expectEmbedded(page, entries[3])
})

test('standalone Service renders exactly the five Console entries and supports both catalog management buttons', async ({ page }) => {
  await page.goto(`${standaloneURL}#/catalog`)
  for (const entry of entries) {
    await expect(page.getByRole('menubar')).toHaveCount(1)
    await expect(page.getByRole('menuitem')).toHaveText(entries.map(item => item.label))
    await page.getByRole('menuitem', { name: entry.label, exact: true }).click()
    await expect(page).toHaveURL(`${standaloneURL}#${entry.path}`)
    await expect(page.getByRole('heading', { name: entry.heading, exact: true })).toBeVisible()
    await expect(page.getByRole('menuitem', { name: entry.label, exact: true })).toHaveClass(/is-active/)
  }
  for (const [button, entry] of [['查询服务管理', entries[0]], ['注册服务管理', entries[3]]]) {
    await page.getByRole('button', { name: button, exact: true }).click()
    await expect(page).toHaveURL(`${standaloneURL}#${entry.path}`)
    await expect(page.getByRole('heading', { name: entry.heading, exact: true })).toBeVisible()
    await page.goBack()
    await expect(page).toHaveURL(`${standaloneURL}#/catalog`)
    await expect(page.getByRole('heading', { name: '服务目录', exact: true })).toBeVisible()
  }
  await page.reload()
  await expect(page.getByRole('menubar')).toHaveCount(1)
  await expect(page.getByRole('menuitem', { name: '服务目录', exact: true })).toHaveClass(/is-active/)
})

const secondRegistered = { ...registered, id: 50, service_name: 'second_registered', title: 'Second registered service' }

async function switchStandaloneDetail(page, id) {
  await page.evaluate(id => { window.location.hash = `/services/${id}` }, id)
  await expect(page).toHaveURL(`${standaloneURL}#/services/${id}`)
}

test('reused registered detail switches identity and restores it through back and forward', async ({ page }) => {
  await page.route('**/api/v1/service/registered/50', route => route.fulfill({ json: secondRegistered }))
  await page.goto(`${standaloneURL}#/services/49`)
  const detail = page.locator('.registered-service-detail')
  await expect(detail.getByRole('heading', { name: registered.title, exact: true })).toBeVisible()
  await detail.evaluate(element => element.dataset.reusedDetail = 'original')
  await switchStandaloneDetail(page, 50)
  await expect(detail.getByRole('heading', { name: secondRegistered.title, exact: true })).toBeVisible()
  await expect(detail).toHaveAttribute('data-reused-detail', 'original')
  await page.goBack()
  await expect(page).toHaveURL(`${standaloneURL}#/services/49`)
  await expect(detail.getByRole('heading', { name: registered.title, exact: true })).toBeVisible()
  await page.goForward()
  await expect(page).toHaveURL(`${standaloneURL}#/services/50`)
  await expect(detail.getByRole('heading', { name: secondRegistered.title, exact: true })).toBeVisible()
  await expect(detail).toHaveAttribute('data-reused-detail', 'original')
})

for (const outcome of ['success', 'failure']) {
  test(`registered detail discards a previous identity's late ${outcome} while the next identity loads`, async ({ page }) => {
    let oldRequest, nextRequest
    await page.route('**/api/v1/service/registered/49', route => { oldRequest = route })
    await page.route('**/api/v1/service/registered/50', route => { nextRequest = route })
    await page.goto(`${standaloneURL}#/services/49`)
    await expect.poll(() => Boolean(oldRequest)).toBe(true)
    await switchStandaloneDetail(page, 50)
    await expect.poll(() => Boolean(nextRequest)).toBe(true)
    const oldFinished = page.waitForEvent('requestfinished', request => new URL(request.url()).pathname.endsWith('/registered/49'))
    await oldRequest.fulfill(outcome === 'success'
      ? { json: registered }
      : { status: 500, json: { error: 'Old identity failed' } })
    await oldFinished
    const detail = page.locator('.registered-service-detail')
    await expect(detail.locator('.loading')).toBeVisible()
    for (const label of ['刷新元数据', '健康检查', '编辑', '删除']) {
      await expect(detail.getByRole('button', { name: label, exact: true })).toBeDisabled()
    }
    await nextRequest.fulfill({ json: secondRegistered })
    await expect(detail.getByRole('heading', { name: secondRegistered.title, exact: true })).toBeVisible()
    await expect(page).toHaveURL(`${standaloneURL}#/services/50`)
    await expect(page.locator('.el-message--error')).toHaveCount(0)
    await expect(detail.getByRole('button', { name: '删除', exact: true })).toBeEnabled()
  })
}
