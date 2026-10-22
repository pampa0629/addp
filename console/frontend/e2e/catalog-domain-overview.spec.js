import { expect, test } from '@playwright/test'

async function fixture(page, { inventory = true } = {}) {
 const reads = [], writes = [], errors = []
 page.on('pageerror', error => errors.push(error.message))
 await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
 await page.route('**/api/v1/**', route => {
  const request = route.request(), url = new URL(request.url()), path = url.pathname
  if (request.method() !== 'GET' && !path.endsWith('/refresh') && !path.endsWith('/resolve-sources')) writes.push(`${request.method()} ${path}`)
  if (path === '/api/v1/system/refresh') return route.fulfill({ json: { access_token: 'overview-user', expires_in: 300 } })
  if (path === '/api/v1/system/users/me') return route.fulfill({ json: { id: 21, username: 'overview-user' } })
  if (path === '/api/v1/system/auth/context') return route.fulfill({ json: {
   context: { type: 'tenant', tenant_id: '3' }, authorization: { role_assignments: [{ scope: { type: 'tenant', tenant_id: '3' }, permissions: ['catalog.entry.read', ...(inventory ? ['catalog.inventory.read'] : [])] }] }
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
  if (path === '/api/v1/catalog/domain_standard_references') return route.fulfill({ json: { data: [{ source_type: 'element', source_identity: '71', evidence: [{ owner_module: 'catalog', resource_type: 'entry', resource_id: 'entry', resource_name: '户外活动表', position: 'activity_id', revision_id: '72', state: 'approved', relation_kind: 'catalog.component.standard_mapping' }] }], total: 1, page: 1, page_size: 5, total_pages: 1 } })
  if (path === '/api/v1/catalog/entries/resolve-sources') return route.fulfill({ json: { results: [{ source_module: 'standard', source_type: 'element', source_identity: '71', found: true, scope_type: 'tenant_common', entry: { id: '00000000-0000-4000-8000-000000000071', display_name: '公共活动标识' } }] } })
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
