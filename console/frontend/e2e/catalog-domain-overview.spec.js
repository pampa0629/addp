import { expect, test } from '@playwright/test'

async function fixture(page, { inventory = true, quality = false, localFirstPage = false, emptyReferences = false } = {}) {
 const reads = [], writes = [], errors = []
 page.on('pageerror', error => errors.push(error.message))
 await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
 await page.route('**/api/v1/**', route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname
  if (request.method() !== 'GET' && !path.endsWith('/refresh') && !path.endsWith('/resolve-sources')) writes.push(`${request.method()} ${path}`)
  if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'overview-user', expires_in: 300 } })
  if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: 21, username: 'overview-user' } })
  if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
   context: { type: 'tenant', tenant_id: '3' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions: ['catalog.entry.read', ...(inventory ? ['catalog.inventory.read'] : []), ...(quality ? ['quality.rule.read', 'quality.plan.read', 'quality.issue.read'] : [])] }] }
  } })
  reads.push(`${request.method()} ${path}${url.search}`)
  if (path === '/api/v1/catalog/domains') return route.fulfill({ json: [{ id: '31', name: '户外域', code: 'outdoor', description: '户外业务语义和责任边界', children: [{ id: '32', name: '子域', code: 'child', description: '', children: [] }] }, { id: '33', name: '空域', code: 'empty_domain', description: '', children: [] }] })
  if (path === '/api/v1/catalog/domains/31/overview') {
   const view = url.searchParams.get('view') || 'governance'
   return route.fulfill({ json: { domain_id: '31', view, total_entries: view === 'inventory' ? 5 : 0,
    entry_types: view === 'inventory' ? ['glossary', 'element', 'code_set', 'standard_document', 'metric'].map(entry_type => ({ entry_type, count: 1 })) : [],
    governance_statuses: [{ status: 'discovered', count: view === 'inventory' ? 5 : 0 }],
    ...(view === 'inventory' ? { coverage: { dimensions: [{ key: 'business_definition', covered: 5, applicable: 5, not_covered: 0, coverage_rate: 100 }] } } : {})
   } })
  }
  if (path === '/api/v1/catalog/domain_standard_references') {
   const currentPage = Number(url.searchParams.get('page')) || 1
   const identities = emptyReferences ? [] : localFirstPage ? currentPage === 1 ? ['71', '72', '73', '74', '75'] : ['76'] : ['71']
   return route.fulfill({ json: { data: identities.map(source_identity => ({ source_type: 'element', source_identity, evidence: [{ owner_module: 'catalog', resource_type: 'entry', resource_id: 'entry', resource_name: '户外活动表', position: 'activity_id', revision_id: '72', state: 'approved', relation_kind: 'catalog.component.standard_mapping' }] })), total: emptyReferences ? 0 : localFirstPage ? 6 : 1, page: currentPage, page_size: 5, total_pages: localFirstPage ? 2 : 1 } })
  }
  if (path === '/api/v1/catalog/entries/resolve-sources') return route.fulfill({ json: { results: request.postDataJSON().references.map(ref => ({ ...ref, found: true,
   ...(localFirstPage ? { owner_domain_id: ref.source_identity === '76' ? '32' : '31' } : { scope_type: 'tenant_common' }),
   entry: { id: `00000000-0000-4000-8000-0000000000${ref.source_identity}`, display_name: localFirstPage ? `${ref.source_identity === '76' ? '跨域' : '本域'}活动标识${ref.source_identity}` : '公共活动标识' }
  })) } })
  if (quality && /^\/api\/v1\/quality\/(rules|plans|issues)$/.test(path)) return route.fulfill({ json: { data: [{ id: 1, name: '户外质量检查'.repeat(30), table_name: 'dim_outdoor_person_'.repeat(18), column_name: 'person_nickname_'.repeat(15) }], total: 1 } })
  if (path === '/api/v1/catalog/entries/facets') return route.fulfill({ json: { view: 'inventory', primary_domains: { status: 'current', options: [] }, accountable_departments: { status: 'current', options: [] }, source_engines: { status: 'current', options: [] }, entry_types: [{ entry_type: 'element', count: 1 }] } })
  if (path === '/api/v1/catalog/entries') return route.fulfill({ json: { data: [{ id: '00000000-0000-4000-8000-000000000071', version: 1, display_name: '户外活动标识', entry_type: 'element', governance_status: 'discovered', visibility: 'inventory', source_status: 'active' }], total: 1, page: 1, page_size: 20, total_pages: 1 } })
  return route.fulfill({ status: 500, json: { error: `unexpected ${request.method()} ${path}` } })
 })
 return { reads, writes, errors, catalog: page.frameLocator('iframe[data-testid="module-iframe"]') }
}

test('Catalog domain overview shares inventory counts, resource drill-down and a single browser history', async ({ page }) => {
 const { reads, writes, errors, catalog } = await fixture(page)
 await page.goto('/catalog/domains?domain_id=31&view=inventory')
 const overview = catalog.getByTestId('catalog-domain-overview')
 await expect(overview).toBeVisible()
 await expect(overview).toContainText('户外业务语义和责任边界')
 await expect(overview.getByTestId('catalog-domain-resource-summary')).toContainText('标准文档 · 1')
 await expect(overview.getByRole('button', { name: '业务定义 · 5/5', exact: true })).toBeDisabled()
 await expect(overview.getByTestId('catalog-domain-reference-summary')).toContainText('公共活动标识')
 await expect(overview).toContainText('无权查看此类引用')
 expect(reads.some(path => path.includes('/api/v1/model/') || path.includes('/api/v1/standard/'))).toBe(false)
 await overview.getByRole('button', { name: '数据元 · 1', exact: true }).click()
 await expect(page).toHaveURL(/\/catalog\/entries\?view=inventory&entry_type=element&primary_domain_id=31$/)
 await expect(catalog.getByTestId('catalog-entry-results')).toContainText('户外活动标识')
 await expect(catalog.getByTestId('catalog-domain-context')).not.toContainText('本域专业内容')
 await catalog.getByTestId('catalog-domain-overview-link').click()
 await expect(page).toHaveURL(/\/catalog\/domains\?domain_id=31&view=inventory$/)
 await page.reload()
 await expect(overview).toContainText('公共活动标识')
 await page.goBack()
 await expect(page).toHaveURL(/\/catalog\/entries\?view=inventory&entry_type=element&primary_domain_id=31$/)
 await page.goForward()
 await expect(overview).toBeVisible()
 expect(writes).toEqual([]); expect(errors).toEqual([])
})

test('Catalog domain overview restores governance scope for a reader without inventory permission', async ({ page }) => {
 const { reads, writes, errors, catalog } = await fixture(page, { inventory: false })
 await page.goto('/catalog/domains?domain_id=31&view=inventory')
 await expect(page).toHaveURL(/\/catalog\/domains\?domain_id=31$/)
 await expect(catalog.getByTestId('catalog-domain-resource-summary')).toContainText('当前范围暂无可见资源')
 expect(reads.some(path => path.includes('/overview?view=inventory'))).toBe(false)
 await expect(catalog.getByRole('radio', { name: '资源盘点', exact: true })).toHaveCount(0)
 expect(writes).toEqual([]); expect(errors).toEqual([])
})

test('Catalog standard references keep local targets visible across owner pages', async ({ page }) => {
 const { catalog, reads, errors } = await fixture(page, { localFirstPage: true })
 await page.goto('/catalog/domains?domain_id=31&view=inventory')
 const references = catalog.getByTestId('catalog-domain-references-catalog')
 await expect(catalog.getByTestId('catalog-domain-reference-summary')).toContainText('本域使用的标准')
 await expect(references.locator('article')).toHaveCount(5)
 await expect(references).toContainText('本域活动标识71')
 await expect(references.locator('article').first()).toContainText('本业务域')
 await references.getByRole('listitem', { name: '第 2 页', exact: true }).click()
 await expect(references.locator('article')).toHaveCount(1)
 await expect(references).toContainText('跨域活动标识76')
 await expect(references).toContainText('其他业务域')
 expect(reads.some(path => path.includes('domain_standard_references') && path.includes('page=2'))).toBe(true)
 expect(errors).toEqual([])
})

test('Catalog reference sections with no results have no pagination', async ({ page }) => {
 const { catalog } = await fixture(page, { emptyReferences: true })
 await page.goto('/catalog/domains?domain_id=31&view=inventory')
 const references = catalog.getByTestId('catalog-domain-references-catalog')
 await expect(references).toContainText('本域暂无此类明确引用')
 await expect(references.locator('.el-pagination')).toHaveCount(0)
})

for (const width of [1280, 1040]) {
 test(`Catalog overview layout separates domain context and wraps Quality identifiers at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  const { catalog, writes, errors } = await fixture(page, { quality: true })
  await page.goto('/catalog/domains?domain_id=31&view=inventory')
  const overview = catalog.getByTestId('catalog-domain-overview')
  await expect(overview.getByRole('heading', { name: '户外域', exact: true })).toBeVisible()
  const selectors = await overview.locator('.selectors').boundingBox()
  const heading = await overview.getByRole('heading', { name: '户外域', exact: true }).boundingBox()
  expect(heading.y - (selectors.y + selectors.height)).toBeGreaterThanOrEqual(16)
  const quality = catalog.getByTestId('catalog-domain-quality-summary')
  await expect(quality.locator('section[data-state="ready"]')).toHaveCount(3)
  await expect(quality.locator('li').last()).toHaveText('dim_outdoor_person_'.repeat(18) + '.' + 'person_nickname_'.repeat(15))
  expect(await quality.evaluate(root => [root, ...root.querySelectorAll('section, li')].every(node => node.scrollWidth <= node.clientWidth + 1))).toBe(true)
  expect(await overview.evaluate(root => root.ownerDocument.documentElement.scrollWidth <= root.ownerDocument.documentElement.clientWidth + 1)).toBe(true)
  expect(writes).toEqual([]); expect(errors).toEqual([])
 })
}
