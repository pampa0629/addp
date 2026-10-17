import { expect, test } from '@playwright/test'

const entryID = '00000000-0000-4000-8000-000000000041'
const categoryID = '9007199254740993'
const unitID = '9007199254740995'
const entryURL = `/catalog/entries/${entryID}?tab=professional`
const readPermissions = ['catalog.entry.read', 'standard.metric.read', 'standard.unit.read']
const unavailableName = '引用名称暂时无法解析'

async function openMetric(page, { deniedPermission, failedReference } = {}) {
  let permissions = readPermissions.filter(permission => permission !== deniedPermission)
  let tokenGeneration = 0
  const referenceRequests = [], unexpected = [], writes = [], errors = []
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/api/v1/**', route => {
    const request = route.request(), path = new URL(request.url()).pathname
    if (path === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: `catalog-metric-reader-token-${++tokenGeneration}`, expires_in: 900 } })
    }
    if (path === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: '32', username: 'catalog-metric-reader', display_name: '指标只读验收账号' } })
    }
    if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
      principal: { id: '32', type: 'user' },
      context: { type: 'tenant', tenant_id: '3', tenant_membership_id: '7' },
      authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions }] }
    } })
    if (request.method() !== 'GET') writes.push(`${request.method()} ${path}`)
    if (path === `/api/v1/catalog/entries/${entryID}`) return route.fulfill({ json: {
      id: entryID, display_name: '目录引用名称展示验收指标', entry_type: 'metric',
      entry_status: 'active', governance_status: 'curated', visibility: 'tenant', version: 3,
      semantic_links: [], responsibilities: [], components: [],
      source: {
        source_module: 'standard', source_type: 'metric', source_identity: '4', source_status: 'active',
        observed_snapshot: { category_id: '8', unit_id: '9' }
      },
      source_resolution: { status: 'current', owner_status: 'draft', owner_version: 1,
        detail_path: '/standard/metrics/4', summary: {
          name: '目录引用名称展示验收指标', code: 'catalog_reference_display_check',
          metric_type: 'atomic', scope_type: 'tenant_common', category_id: categoryID, unit_id: unitID
        } }
    } })
    if (path === `/api/v1/catalog/me/entries/${entryID}/marks`) {
      return route.fulfill({ json: { favorite: false, following: false } })
    }
    if (path === '/api/v1/standard/metrics/4/relations') {
      return route.fulfill({ json: { nodes: [], edges: [] } })
    }
    const reference = path === '/api/v1/standard/metric-categories' ? 'category'
      : path === `/api/v1/standard/units/${unitID}` ? 'unit' : null
    if (reference) {
      referenceRequests.push({ reference, authorization: request.headers().authorization })
      if (reference === failedReference) return route.fulfill({ status: 403, json: { error: 'reference_read_denied' } })
      return route.fulfill({ json: reference === 'category'
        ? [{ id: '9007199254740992', name: '相邻编号分类' }, { id: categoryID, name: '目录展示验收' }]
        : { id: unitID, name: '次' } })
    }
    unexpected.push(`${request.method()} ${path}`)
    return route.fulfill({ status: 500, json: { error: 'unexpected_request' } })
  })
  await page.goto(entryURL)
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  const detail = catalog.getByTestId('catalog-entry-detail')
  await expect(detail).toHaveAttribute('data-load-state', 'loaded')
  const value = label => detail.locator('td.el-descriptions__label').filter({ hasText: label })
    .locator('xpath=following-sibling::td[1]')
  return {
    catalog, detail, value, referenceRequests,
    async refreshPermissions(next) {
      permissions = next
      await page.evaluate(async () => {
        const { useAuthStore } = await import('/src/store/auth.js')
        await useAuthStore().refreshAccessToken({ force: true })
      })
      await expect(detail).toBeVisible()
    },
    verifyReadOnly() {
      expect(writes).toEqual([])
      expect(unexpected).toEqual([])
      expect(errors).toEqual([])
      expect(referenceRequests.every(request => /^Bearer catalog-metric-reader-token-[1-9]\d*$/.test(request.authorization))).toBe(true)
    }
  }
}

test('Catalog metric reader sees current category and unit names through the real iframe after reload', async ({ page }) => {
  const fixture = await openMetric(page)
  for (let attempt = 0; attempt < 2; attempt++) {
    await expect(fixture.detail).toHaveAttribute('data-governance-status', 'curated')
    await expect(fixture.catalog.getByRole('tab', { name: '专业事实' })).toHaveAttribute('aria-selected', 'true')
    await expect(fixture.value('指标分类')).toHaveText('目录展示验收')
    await expect(fixture.value('计量单位')).toHaveText('次')
    await expect(fixture.value('动态解析状态')).toHaveText('已读取当前事实')
    await expect(fixture.value('专业对象状态')).toHaveText('draft')
    await expect(fixture.catalog.getByTestId('catalog-curation-action')).toHaveCount(0)
    await expect(fixture.catalog.getByTestId('catalog-more-actions')).toHaveCount(0)
    await expect(page).toHaveURL(new RegExp(`${entryID}\\?tab=professional$`))
    if (attempt === 0) await page.reload()
  }
  expect(fixture.referenceRequests.map(request => request.reference).sort()).toEqual(['category', 'category', 'unit', 'unit'])
  fixture.verifyReadOnly()
})

for (const { deniedPermission, absent, category, unit } of [
  { deniedPermission: 'standard.metric.read', absent: 'category', category: unavailableName, unit: '次' },
  { deniedPermission: 'standard.unit.read', absent: 'unit', category: '目录展示验收', unit: unavailableName }
]) {
  test(`Catalog keeps reference permissions independent when ${deniedPermission} is absent`, async ({ page }) => {
    const fixture = await openMetric(page, { deniedPermission })
    await expect(fixture.value('指标分类')).toHaveText(category)
    await expect(fixture.value('计量单位')).toHaveText(unit)
    expect(fixture.referenceRequests.map(request => request.reference)).toEqual([absent === 'category' ? 'unit' : 'category'])
    fixture.verifyReadOnly()
  })
}

for (const failedReference of ['category', 'unit']) {
  test(`Catalog retains the other reference name when ${failedReference} reading is rejected`, async ({ page }) => {
    const fixture = await openMetric(page, { failedReference })
    await expect(fixture.value('指标分类')).toHaveText(failedReference === 'category' ? unavailableName : '目录展示验收')
    await expect(fixture.value('计量单位')).toHaveText(failedReference === 'unit' ? unavailableName : '次')
    await expect(fixture.value('动态解析状态')).toHaveText('已读取当前事实')
    expect(fixture.referenceRequests.map(request => request.reference).sort()).toEqual(['category', 'unit'])
    fixture.verifyReadOnly()
  })
}

test('Catalog removes resolved unit names when the current authorization loses unit read permission', async ({ page }) => {
  const fixture = await openMetric(page)
  await expect(fixture.value('指标分类')).toHaveText('目录展示验收')
  await expect(fixture.value('计量单位')).toHaveText('次')
  fixture.referenceRequests.length = 0
  await fixture.refreshPermissions(['catalog.entry.read', 'standard.metric.read'])
  await expect(fixture.value('指标分类')).toHaveText('目录展示验收')
  await expect(fixture.value('计量单位')).toHaveText(unavailableName)
  expect(fixture.referenceRequests.some(request => request.reference === 'unit')).toBe(false)
  await fixture.refreshPermissions(readPermissions)
  await expect(fixture.value('指标分类')).toHaveText('目录展示验收')
  await expect(fixture.value('计量单位')).toHaveText('次')
  fixture.verifyReadOnly()
})
