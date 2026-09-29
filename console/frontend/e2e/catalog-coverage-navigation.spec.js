import { expect, test } from '@playwright/test'

const consoleURL = 'http://127.0.0.1:4170'
const dimensions = [
  'business_definition', 'primary_domain', 'accountable_department', 'business_owner',
  'data_steward', 'glossary', 'component_standard_mapping'
]

test('Catalog coverage drill-down closes department, definition and glossary gaps independently', async ({ page }) => {
  const entryQueries = []
  const candidateQueries = []
  const batchRequests = []
  const curationRequests = []
  const pageErrors = []
  let departmentAssigned = false
  let curated = false
  let glossaryLinked = false
  page.on('pageerror', error => pageErrors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/**', route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    if (path === '/api/v1/system/refresh') {
      return route.fulfill({ json: { access_token: 'catalog-coverage-token', expires_in: 300 } })
    }
    if (path === '/api/v1/system/users/me') {
      return route.fulfill({ json: { id: 32, username: 'catalog-coverage-user' } })
    }
    if (path === '/api/v1/system/auth/context') {
      return route.fulfill({ json: {
        context: { type: 'tenant', tenant_id: '3' },
        authorization: { role_assignments: [{
          scope: { type: 'tenant', tenant_id: '3' },
          permissions: ['catalog.entry.read', 'catalog.inventory.read', 'catalog.entry.update']
        }] }
      } })
    }
    if (path === '/api/v1/catalog/governance/coverage') {
      return route.fulfill({ json: {
        view: 'inventory', total_entries: 1,
        governance_statuses: [
          { status: 'discovered', count: curated ? 0 : 1 }, { status: 'curated', count: curated ? 1 : 0 },
          { status: 'certified', count: 0 }, { status: 'deprecated', count: 0 }
        ],
        dimensions: dimensions.map(key => {
          if (key === 'component_standard_mapping') {
            return { key, covered: 0, applicable: 0, not_covered: 0, not_applicable: 1, coverage_rate: 0 }
          }
          const missing = (key === 'accountable_department' && !departmentAssigned) ||
            (key === 'business_definition' && !curated) || (key === 'glossary' && !glossaryLinked)
          return { key, covered: missing ? 0 : 1, applicable: 1, not_covered: missing ? 1 : 0,
            not_applicable: 0, coverage_rate: missing ? 0 : 100 }
        })
      } })
    }
    if (path === '/api/v1/catalog/entries/facets') {
      return route.fulfill({ json: {
        view: 'inventory', primary_domains: { status: 'current', options: [] },
        accountable_departments: { status: 'current', options: [] },
        entry_types: [{ entry_type: 'data_item', count: 1 }],
        source_engines: { status: 'current', options: [] }
      } })
    }
    if (path === '/api/v1/catalog/domains') return route.fulfill({ json: [] })
    if (path === '/api/v1/catalog/reference-candidates') {
      candidateQueries.push(url.searchParams)
      const candidates = {
        department: [{ id: '12', name: '户外数据治理部' }],
        domain: [{ id: '1', name: '户外域', code: 'outdoor' }],
        glossary: [{ id: '30', name: '户外活动', code: 'outdoor_activity' }],
        user: [{ id: '21', name: '户外业务负责人' }, { id: '22', name: '户外数据管理员' }]
      }
      return route.fulfill({ json: {
        data: candidates[url.searchParams.get('reference_type')] || [], total: 1, page: 1, page_size: 20, total_pages: 1
      } })
    }
    if (path === '/api/v1/catalog/entries/batch_governance') {
      batchRequests.push(request.postDataJSON())
      departmentAssigned = true
      return route.fulfill({ json: { entries: [{ id: '00000000-0000-4000-8000-000000000001', version: 2 }] } })
    }
    if (path === '/api/v1/catalog/entries/00000000-0000-4000-8000-000000000001/data-dictionary') {
      return route.fulfill({ json: {
        as_of: '2026-09-29T00:00:00Z', generated_at: '2026-09-29T00:00:00Z', fields: []
      } })
    }
    if (path === '/api/v1/catalog/entries/00000000-0000-4000-8000-000000000001') {
      if (request.method() === 'PUT') {
        const payload = request.postDataJSON()
        curationRequests.push(payload)
        curated = true
        glossaryLinked = payload.glossary_ids.includes('30')
      }
      return route.fulfill({ json: {
        id: '00000000-0000-4000-8000-000000000001', display_name: curated ? '户外活动数据' : '待分配责任部门的数据项',
        business_name: curated ? '户外活动数据' : null,
        business_description: curated ? '记录户外活动及其参与情况' : null,
        entry_type: 'data_item', entry_status: 'active', governance_status: curated ? 'curated' : 'discovered',
        visibility: 'inventory', version: glossaryLinked ? 4 : curated ? 3 : departmentAssigned ? 2 : 1,
        source: { source_module: 'meta', source_type: 'data_item', source_status: 'active', source_identity: 'fixture-data-item' },
        semantic_links: curated ? [
          { semantic_type: 'domain', semantic_id: '1', relation_role: 'primary', observed_snapshot: { name: '户外域' } },
          ...(glossaryLinked ? [{ semantic_type: 'glossary', semantic_id: '30', observed_snapshot: { name: '户外活动', code: 'outdoor_activity' } }] : [])
        ] : [],
        responsibilities: curated ? [
          { role: 'accountable_department', subject_type: 'department', subject_id: '12', status: 'active', observed_snapshot: { name: '户外数据治理部' } },
          { role: 'business_owner', subject_type: 'user', subject_id: '21', status: 'active', observed_snapshot: { name: '户外业务负责人' } },
          { role: 'data_steward', subject_type: 'user', subject_id: '22', status: 'active', observed_snapshot: { name: '户外数据管理员' } }
        ] : departmentAssigned ? [
          { role: 'accountable_department', subject_type: 'department', subject_id: '12', status: 'active', observed_snapshot: { name: '户外数据治理部' } }
        ] : [],
        components: []
      } })
    }
    if (path === '/api/v1/catalog/me/entries/00000000-0000-4000-8000-000000000001/marks') {
      return route.fulfill({ json: { favorite: false, following: false } })
    }
    if (path === '/api/v1/catalog/entries') {
      entryQueries.push(url.searchParams)
      const missingDepartment = url.searchParams.get('coverage_dimension') === 'accountable_department' &&
        url.searchParams.get('coverage_state') === 'missing'
      const missingDefinition = url.searchParams.get('coverage_dimension') === 'business_definition' &&
        url.searchParams.get('coverage_state') === 'missing'
      const missingGlossary = url.searchParams.get('coverage_dimension') === 'glossary' &&
        url.searchParams.get('coverage_state') === 'missing'
      const requestedGovernance = url.searchParams.get('governance_status')
      const visible = (!missingDepartment || !departmentAssigned) && (!missingDefinition || !curated) && (!missingGlossary || !glossaryLinked) &&
        (!requestedGovernance || requestedGovernance === (curated ? 'curated' : 'discovered')) &&
        (url.searchParams.get('view') !== 'governance' || curated)
      return route.fulfill({ json: {
        data: visible ? [{
          id: '00000000-0000-4000-8000-000000000001', display_name: curated ? '户外活动数据' : '待分配责任部门的数据项',
          entry_type: 'data_item', source_status: 'active', governance_status: curated ? 'curated' : 'discovered', version: glossaryLinked ? 4 : curated ? 3 : departmentAssigned ? 2 : 1,
          visibility: 'inventory', updated_at: '2026-09-01T00:00:00Z'
        }] : [],
        total: visible ? 1 : 0, page: 1, page_size: 20, total_pages: visible ? 1 : 0
      } })
    }
    return route.fulfill({ status: 500, json: { error: `unexpected ${request.method()} ${path}` } })
  })

  await page.goto('/catalog/governance/coverage')
  const catalog = page.frameLocator('iframe[data-testid="module-iframe"]')
  await expect(catalog.getByTestId('catalog-governance-coverage')).toHaveAttribute('data-load-state', 'loaded')
  const mappingDimension = catalog.getByTestId('catalog-coverage-dimension').filter({ hasText: '组件标准映射' }).locator('xpath=ancestor::tr')
  await expect(mappingDimension).toContainText('0%')
  await expect(mappingDimension.locator('td').last()).toHaveText('1')
  await expect(mappingDimension.getByTestId('catalog-coverage-missing-link')).toHaveCount(0)
  await catalog.getByRole('button', { name: '查看责任部门未覆盖条目，共 1 条' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=accountable_department&coverage_state=missing`)
  await expect(catalog.getByTestId('catalog-entry-list')).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-coverage-gap-alert')).toContainText('可在缺口列表勾选条目并批量分配责任部门')
  await expect(catalog.getByTestId('catalog-entry-results')).toContainText('待分配责任部门的数据项')
  expect(entryQueries.at(-1)?.get('coverage_dimension')).toBe('accountable_department')
  expect(entryQueries.at(-1)?.get('coverage_state')).toBe('missing')
  expect(entryQueries.at(-1)?.get('search')).toBe('')

  await catalog.getByTestId('catalog-entry-results').getByText('待分配责任部门的数据项').click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries/00000000-0000-4000-8000-000000000001?view=inventory&coverage_dimension=accountable_department&coverage_state=missing`)
  await expect(catalog.getByTestId('catalog-entry-detail')).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-entry-coverage-context')).toContainText('目录筛选：“责任部门”未覆盖')
  await expect(catalog.getByTestId('catalog-entry-coverage-context')).toContainText('可在缺口列表勾选条目并批量分配责任部门')
  await catalog.getByTestId('catalog-entry-coverage-context').getByRole('button', { name: '返回缺口列表' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=accountable_department&coverage_state=missing`)

  await page.goto('/catalog/governance/coverage')
  await expect(catalog.getByTestId('catalog-governance-coverage')).toHaveAttribute('data-load-state', 'loaded')
  await catalog.getByRole('button', { name: '查看已发现条目，共 1 条' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&governance_status=discovered`)
  await expect(catalog.getByTestId('catalog-discovered-guidance')).toContainText('批量分配主业务域或责任部门不会改变编目状态')
  await expect(catalog.getByTestId('catalog-entry-results')).toContainText('待分配责任部门的数据项')
  expect(entryQueries.at(-1)?.get('governance_status')).toBe('discovered')
  expect(entryQueries.at(-1)?.get('coverage_dimension')).toBe('')
  await catalog.getByTestId('catalog-entry-results').getByText('待分配责任部门的数据项').click()
  await expect(catalog.getByTestId('catalog-entry-detail')).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-entry-coverage-context')).toHaveCount(0)
  await catalog.getByRole('button', { name: '返回目录' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&governance_status=discovered`)

  await page.goto('/catalog/governance/coverage')
  await expect(catalog.getByTestId('catalog-governance-coverage')).toHaveAttribute('data-load-state', 'loaded')
  await catalog.getByRole('button', { name: '查看责任部门未覆盖条目，共 1 条' }).click()
  const gapList = catalog.getByTestId('catalog-entry-list')
  await expect(gapList).toHaveAttribute('data-load-state', 'loaded')
  await gapList.getByTestId('catalog-entry-results').locator('tbody .el-checkbox').first().click()
  await gapList.getByTestId('catalog-batch-governance-open').click()
  const batchDialog = catalog.getByTestId('catalog-batch-governance-dialog')
  await expect(batchDialog).toBeVisible()
  await expect(catalog.getByTestId('catalog-batch-governance-operation')).toContainText('分配责任部门')
  await catalog.getByTestId('catalog-batch-governance-target').click()
  await catalog.getByRole('option', { name: '户外数据治理部' }).click()
  expect(candidateQueries.at(-1)?.get('reference_type')).toBe('department')
  await catalog.getByTestId('catalog-batch-governance-submit').click()
  await expect(batchDialog).toBeHidden()
  expect(batchRequests).toEqual([{
    entries: [{ id: '00000000-0000-4000-8000-000000000001', version: 1 }],
    operation: 'assign_accountable_department', reference_id: '12'
  }])
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=accountable_department&coverage_state=missing`)
  await expect(gapList).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-view-scope')).toHaveAttribute('data-result-total', '0')
  await expect(gapList).toContainText('当前条件下没有“责任部门”未覆盖条目')

  await page.goto('/catalog/entries?view=inventory&governance_status=discovered')
  await expect(gapList).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-view-scope')).toHaveAttribute('data-result-total', '1')
  await expect(gapList.getByTestId('catalog-entry-results')).toContainText('待分配责任部门的数据项')

  await page.goto('/catalog/governance/coverage')
  const departmentDimension = catalog.getByTestId('catalog-coverage-dimension').filter({ hasText: '责任部门' }).locator('xpath=ancestor::tr')
  await expect(departmentDimension).toContainText('100%')
  await expect(departmentDimension.getByRole('button', { name: '查看责任部门未覆盖条目，共 1 条' })).toHaveCount(0)

  await catalog.getByRole('button', { name: '查看业务定义未覆盖条目，共 1 条' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=business_definition&coverage_state=missing`)
  await expect(gapList.getByTestId('catalog-entry-results')).toContainText('待分配责任部门的数据项')
  await gapList.getByTestId('catalog-entry-results').getByText('待分配责任部门的数据项').click()
  const detail = catalog.getByTestId('catalog-entry-detail')
  await expect(detail).toHaveAttribute('data-load-state', 'loaded')
  await expect(detail.getByTestId('catalog-entry-coverage-context')).toContainText('还需满足主业务域和责任关系的必填条件')
  await detail.getByTestId('catalog-curation-action').click()
  const editor = detail.locator('.editor-card')
  await expect(editor).toBeVisible()
  await editor.getByPlaceholder('填写业务人员能理解的名称').fill('户外活动数据')
  await editor.getByPlaceholder('说明这条资源是什么、适用于什么业务场景').fill('记录户外活动及其参与情况')
  async function selectEditorCandidate(select, name) {
    await select.click()
    const listboxID = await select.getByRole('combobox').getAttribute('aria-controls')
    await catalog.locator(`[id="${listboxID}"]`).getByRole('option', { name }).click()
  }
  await editor.getByRole('button', { name: '添加业务域' }).click()
  await selectEditorCandidate(editor.locator('.domain-row .el-select').first(), '户外域')
  await selectEditorCandidate(editor.locator('.responsibility-row').nth(1).locator('.el-select').nth(1), '户外业务负责人')
  await selectEditorCandidate(editor.locator('.responsibility-row').nth(2).locator('.el-select').nth(1), '户外数据管理员')
  await editor.getByRole('button', { name: '保存编目' }).click()
  await expect(editor).toHaveCount(0)
  await expect(detail).toHaveAttribute('data-governance-status', 'curated')
  expect(curationRequests).toEqual([{
    version: 2, business_name: '户外活动数据', business_description: '记录户外活动及其参与情况',
    governance_status: 'curated', visibility: 'inventory',
    domains: [{ id: '1', role: 'primary' }], glossary_ids: [],
    responsibilities: [
      { role: 'accountable_department', subject_type: 'department', subject_id: '12' },
      { role: 'business_owner', subject_type: 'user', subject_id: '21' },
      { role: 'data_steward', subject_type: 'user', subject_id: '22' }
    ]
  }])
  await detail.getByTestId('catalog-entry-coverage-context').getByRole('button', { name: '返回缺口列表' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=business_definition&coverage_state=missing`)
  await expect(gapList).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-view-scope')).toHaveAttribute('data-result-total', '0')
  await expect(gapList).toContainText('当前条件下没有“业务定义”未覆盖条目')

  await page.goto('/catalog/governance/coverage')
  const definitionDimension = catalog.getByTestId('catalog-coverage-dimension').filter({ hasText: '业务定义' }).locator('xpath=ancestor::tr')
  await expect(definitionDimension).toContainText('100%')
  await expect(definitionDimension.getByRole('button', { name: '查看业务定义未覆盖条目，共 1 条' })).toHaveCount(0)
  await catalog.getByRole('button', { name: '查看已编目条目，共 1 条' }).click()
  await expect(catalog.getByTestId('catalog-entry-results')).toContainText('户外活动数据')
  await page.goto('/catalog/governance/coverage')
  const glossaryDimension = catalog.getByTestId('catalog-coverage-dimension').filter({ hasText: '企业术语' }).locator('xpath=ancestor::tr')
  await expect(glossaryDimension).toContainText('0%')
  await catalog.getByRole('button', { name: '查看企业术语未覆盖条目，共 1 条' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=glossary&coverage_state=missing`)
  await expect(gapList.getByTestId('catalog-entry-results')).toContainText('户外活动数据')
  await gapList.getByTestId('catalog-entry-results').getByText('户外活动数据').click()
  await expect(detail).toHaveAttribute('data-governance-status', 'curated')
  await expect(detail.getByTestId('catalog-entry-coverage-context')).toContainText('术语关联不是完成编目的强制条件')
  await detail.getByTestId('catalog-curation-action').click()
  await expect(editor).toBeVisible()
  await editor.getByRole('button', { name: '添加术语' }).click()
  await selectEditorCandidate(editor.locator('.id-row .el-select').first(), '户外活动 · outdoor_activity')
  expect(candidateQueries.some(query => query.get('reference_type') === 'glossary')).toBe(true)
  await editor.getByRole('button', { name: '保存编目' }).click()
  await expect(editor).toHaveCount(0)
  expect(curationRequests).toHaveLength(2)
  expect(curationRequests[1]).toEqual({
    ...curationRequests[0], version: 3, glossary_ids: ['30']
  })
  await expect(detail).toHaveAttribute('data-governance-status', 'curated')
  await detail.getByTestId('catalog-entry-coverage-context').getByRole('button', { name: '返回缺口列表' }).click()
  await expect(page).toHaveURL(`${consoleURL}/catalog/entries?view=inventory&coverage_dimension=glossary&coverage_state=missing`)
  await expect(gapList).toHaveAttribute('data-load-state', 'loaded')
  await expect(catalog.getByTestId('catalog-view-scope')).toHaveAttribute('data-result-total', '0')
  await expect(gapList).toContainText('当前条件下没有“企业术语”未覆盖条目')
  await page.goto('/catalog/governance/coverage')
  await expect(glossaryDimension).toContainText('100%')
  await expect(glossaryDimension.getByRole('button', { name: '查看企业术语未覆盖条目，共 1 条' })).toHaveCount(0)
  expect(pageErrors).toEqual([])
})
