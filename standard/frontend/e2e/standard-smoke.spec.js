import { expect, test } from '@playwright/test'

const allStandardPermissions = [
  'standard.code_set.create',
  'standard.code_set.delete',
  'standard.code_set.publish',
  'standard.code_set.read',
  'standard.code_set.update',
  'standard.document.create',
  'standard.document.delete',
  'standard.document.publish',
  'standard.document.read',
  'standard.document.update',
  'standard.document_extraction.create',
  'standard.domain.create',
  'standard.domain.delete',
  'standard.domain.read',
  'standard.domain.update',
  'standard.element.create',
  'standard.element.delete',
  'standard.element.publish',
  'standard.element.read',
  'standard.element.update',
  'standard.glossary.create',
  'standard.glossary.delete',
  'standard.glossary.publish',
  'standard.glossary.read',
  'standard.glossary.update',
  'standard.metric.create',
  'standard.metric.delete',
  'standard.metric.publish',
  'standard.metric.read',
  'standard.metric.update',
  'standard.unit.create',
  'standard.unit.delete',
  'standard.unit.read',
  'standard.unit.update'
]

const readOnlyStandardPermissions = allStandardPermissions.filter(permission => permission.endsWith('.read'))

const domains = [
  { id: 2, name: '户外域', code: 'outdoor', description: '户外业务', version: 1, children: [] },
  { id: 1, name: '客户域', code: 'customer', description: '客户业务', version: 1, children: [] }
]

const glossaries = [
  {
    id: 21,
    code: 'leader',
    scope_type: 'domain',
    owner_domain_id: 2,
    lifecycle_state: 'active',
    version: 1,
    has_publication_history: false,
    tags: [],
    draft_revision_id: 211,
    current_revision: null,
    draft_revision: {
      id: 211,
      glossary_id: 21,
      revision_no: 1,
      name: '领队',
      alias: ['leader'],
      definition: '发起并组织户外活动的人',
      example: '',
      note: '',
      related_ids: [],
      change_summary: '初始修订',
      effective_from: '2026-08-12T08:00:00Z',
      status: 'draft'
    },
    created_at: '2026-08-12T08:00:00Z'
  }
]

const createDocumentFixture = (overrides = {}) => {
  const { revision: revisionOverrides = {}, ...identityOverrides } = overrides
  const id = identityOverrides.id || 71
  const revision = {
    id: id * 10 + 1,
    document_id: id,
    revision_no: 1,
    status: 'draft',
    name: '户外数据标准',
    version_label: 'v1',
    description: '户外业务数据标准',
    file_name: 'outdoor-standard.md',
    file_size: 2048,
    media_type: 'text/markdown',
    content_sha256: 'f'.repeat(64),
    change_summary: '初始修订',
    created_at: '2026-08-12T08:00:00Z',
    ...revisionOverrides
  }
  return {
    id,
    code: 'outdoor_data_standard',
    scope_type: 'domain',
    owner_domain_id: 2,
    doc_type: 'reference',
    source_org: '标准组',
    tags: [],
    lifecycle_state: 'active',
    version: 1,
    draft_revision_id: revision.id,
    draft_revision: revision,
    current_revision: null,
    has_publication_history: false,
    created_at: '2026-08-12T08:00:00Z',
    updated_at: '2026-08-12T08:00:00Z',
    ...identityOverrides
  }
}

const createCandidateGroups = (candidates, { extractionID = 81, revisionID = 711, extractedAt = '2026-09-06T08:00:00Z' } = {}) => candidates.map(candidate => ({
  semantic_fingerprint: `fixture-${candidate.id}`,
  state: candidate.formalization ? 'formalized' : candidate.status,
  occurrence_count: 1,
  first_seen_at: extractedAt,
  last_seen_at: extractedAt,
  candidate,
  occurrences: [{
    candidate_id: candidate.id,
    extraction_id: extractionID,
    document_revision_id: revisionID,
    requested_by: 1,
    extracted_at: extractedAt,
    status: candidate.status,
    version: candidate.version,
    evidences: candidate.evidences || [],
    ...(candidate.formalization ? { formalization: candidate.formalization } : {})
  }]
}))

const createCandidateFamilySnapshotToken = family => {
  const seed = [family.candidate_type, family.code, ...family.variants.flatMap(group => [group.candidate.id, group.candidate.version, group.semantic_fingerprint])].join('|')
  let hash = 0
  for (const character of seed) hash = (hash * 31 + character.codePointAt(0)) >>> 0
  return hash.toString(16).padStart(64, '0')
}

const groupCandidateFamilies = groups => {
  const familiesByKey = new Map()
  groups.forEach(group => {
    const familyKey = `${group.candidate.candidate_type}:${group.candidate.code}`
    let family = familiesByKey.get(familyKey)
    if (!family) {
      family = {
        family_key: familyKey,
        candidate_type: group.candidate.candidate_type,
        code: group.candidate.code,
        representative_name: group.candidate.name,
        variant_count: 0,
        total_variant_count: 0,
        decision_count: 0,
        occurrence_count: 0,
        first_seen_at: group.first_seen_at,
        last_seen_at: group.last_seen_at,
        variants: []
      }
      familiesByKey.set(familyKey, family)
    }
    family.variants.push(group)
    family.variant_count += 1
    family.total_variant_count += 1
    family.occurrence_count += group.occurrence_count
    if (group.first_seen_at < family.first_seen_at) family.first_seen_at = group.first_seen_at
    if (group.last_seen_at > family.last_seen_at) family.last_seen_at = group.last_seen_at
  })
  return [...familiesByKey.values()].map(family => ({ ...family, snapshot_token: createCandidateFamilySnapshotToken(family) }))
}

const createFamilyComparisonCounts = groups => {
  const families = groupCandidateFamilies(groups)
  return families.reduce((counts, family) => {
    const results = new Set(family.variants.map(group => group.candidate.comparison?.result).filter(Boolean))
    results.forEach(result => { counts[result] += 1 })
    return counts
  }, { all: families.length, new: 0, exact: 0, content_conflict: 0, scope_conflict: 0 })
}

const createCandidateFamilyResponse = (candidates, options = {}) => {
  const groups = createCandidateGroups(candidates, options)
  const families = groupCandidateFamilies(groups)
  return {
    data: families,
    total: families.length,
    variant_total: groups.length,
    page: 1,
    page_size: 20,
    total_pages: 1,
    variant_status_counts: candidates.reduce((counts, candidate) => {
      counts[candidate.formalization ? 'formalized' : candidate.status] += 1
      return counts
    }, { pending: 0, retained: 0, rejected: 0, formalized: 0 }),
    family_comparison_counts: createFamilyComparisonCounts(groups)
  }
}

const filterCandidateFamilyResponse = (response, url) => {
  const state = url.searchParams.get('state') || ''
  const candidateType = url.searchParams.get('candidate_type') || ''
  const keyword = (url.searchParams.get('keyword') || '').trim().replace(/\s+/g, ' ').toLocaleLowerCase()
  const comparisonResult = url.searchParams.get('comparison_result') || ''
  const page = Number(url.searchParams.get('page')) || 1
  const pageSize = Number(url.searchParams.get('page_size')) || response.page_size || 20
  const allVariants = response.data.flatMap(family => family.variants)
  const totalVariantCounts = new Map(response.data.map(family => [family.family_key, family.total_variant_count]))
  const decisionCounts = new Map(response.data.map(family => [family.family_key, family.decision_count || 0]))
  const snapshotTokens = new Map(response.data.map(family => [family.family_key, family.snapshot_token]))
  const comparisonSource = allVariants.filter(group => {
    if (state && group.state !== state) return false
    if (candidateType && group.candidate.candidate_type !== candidateType) return false
    if (!keyword) return true
    return [group.candidate.code, group.candidate.name].some(value => String(value || '').trim().replace(/\s+/g, ' ').toLocaleLowerCase().includes(keyword))
  })
  const comparisonCounts = createFamilyComparisonCounts(comparisonSource)
  const filtered = comparisonResult ? comparisonSource.filter(group => group.candidate.comparison?.result === comparisonResult) : comparisonSource
  const families = groupCandidateFamilies(filtered)
  families.forEach(family => {
    family.total_variant_count = totalVariantCounts.get(family.family_key) || family.total_variant_count
    family.decision_count = decisionCounts.get(family.family_key) || 0
    family.snapshot_token = snapshotTokens.get(family.family_key) || family.snapshot_token
  })
  const totalPages = Math.max(1, Math.ceil(families.length / pageSize))
  const data = page > totalPages ? [] : families.slice((page - 1) * pageSize, page * pageSize)
  return { ...response, data, total: families.length, variant_total: filtered.length, page, page_size: pageSize, total_pages: totalPages, family_comparison_counts: comparisonCounts }
}

const listPages = [
  ['/domains', '业务域管理'],
  ['/glossaries', '业务术语词典'],
  ['/elements', '数据元管理'],
  ['/code-sets', '新建码值集'],
  ['/units', '计量单位管理'],
  ['/metrics', '指标定义'],
  ['/documents', '全局文档库']
]

const visualPages = [
  ['/domains', '业务域管理', 'domains'],
  ['/glossaries', '业务术语词典', 'glossaries'],
  ['/elements', '数据元管理', 'elements'],
  ['/code-sets', '新建码值集', 'code-sets'],
  ['/units', '计量单位管理', 'units'],
  ['/metrics', '指标定义', 'metrics'],
  ['/documents', '全局文档库', 'documents']
]

const narrowVisualPages = visualPages.filter(([, , name]) => (
  ['domains', 'code-sets', 'documents'].includes(name)
))

const themeVisualPages = narrowVisualPages
const themeVisualModes = ['dark', 'blue', 'purple']

test('loads every Standard management page', async ({ page }) => {
  await installMockBackend(page)
  for (const [path, visibleText] of listPages) {
    await page.goto(path)
    await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
  }
})

test.describe('Standard management page visual baselines', () => {
  test.describe('desktop', () => {
    test.use({ viewport: { width: 1280, height: 800 } })

    for (const [path, visibleText, name] of visualPages) {
      test(`${name} desktop`, async ({ page }) => {
        await installMockBackend(page)
        await page.goto(path)
        await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
        await expect(page.locator('.el-loading-mask:visible')).toHaveCount(0)
        await expect(page).toHaveScreenshot(`standard-${name}-desktop.png`, {
          animations: 'disabled',
          caret: 'hide',
          fullPage: true
        })
      })
    }
  })

  test.describe('narrow', () => {
    test.use({ viewport: { width: 720, height: 760 } })

    for (const [path, visibleText, name] of narrowVisualPages) {
      test(`${name} narrow`, async ({ page }) => {
        await installMockBackend(page)
        await page.goto(path)
        await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
        await expect(page.locator('.el-loading-mask:visible')).toHaveCount(0)
        await expect(page).toHaveScreenshot(`standard-${name}-narrow.png`, {
          animations: 'disabled',
          caret: 'hide',
          fullPage: true
        })
      })
    }
  })
})

test.describe('Standard theme visual baselines', () => {
  test.use({ viewport: { width: 1280, height: 800 } })

  for (const theme of themeVisualModes) {
    for (const [path, visibleText, name] of themeVisualPages) {
      test(`${theme} ${name}`, async ({ page }) => {
        await installMockBackend(page, { theme })
        await page.goto(path)
        await expect(page.getByText(visibleText, { exact: true }).first()).toBeVisible()
        await expect.poll(() => page.evaluate(() => document.documentElement.className)).toContain(theme)
        await expect(page.locator('.el-loading-mask:visible')).toHaveCount(0)
        await expect(page).toHaveScreenshot(`standard-${theme}-${name}.png`, {
          animations: 'disabled',
          caret: 'hide',
          fullPage: true
        })
      })
    }
  }
})

for (const path of ['/glossaries', '/elements', '/code-sets', '/metrics', '/documents']) {
  test(`business domain hierarchy, search and selection on ${path}`, async ({ page }) => {
    await installMockBackend(page)
    await page.route('**/api/v1/standard/domains', route => fulfillJSON(route, [
      { id: 2, name: '户外域', code: 'outdoor' },
      { id: 1, name: '客户域', code: 'customer', children: [
        { id: 3, name: 'VIP', code: 'customer_vip', children: [
          { id: 4, name: '国内', code: 'domestic' }
        ] }
      ] }
    ]))
    await page.goto(path)
    const selector = page.locator('.el-select').filter({ has: page.locator('.el-select__placeholder', { hasText: '选择业务域' }) }).first()
    await selector.locator('.el-select__wrapper').click()
    const rows = page.locator('.el-select-dropdown:visible .business-domain-option')
    await expect(rows).toHaveCount(4)
    await expect(rows.nth(0)).toHaveText('户外域')
    await expect(rows.nth(1)).toHaveText('客户域')
    await expect(rows.nth(2)).toHaveCSS('padding-inline-start', '16px')
    await expect(rows.nth(3)).toHaveCSS('padding-inline-start', '32px')
    if (path === '/elements') await page.screenshot({ path: test.info().outputPath('business-domain-hierarchy.png'), animations: 'disabled' })
    await selector.getByRole('combobox').fill('DOMESTIC')
    await expect(rows).toHaveCount(1)
    await expect(rows.first()).toHaveText('客户域 / VIP / 国内')
    await rows.first().click()
    const selected = page.locator('.el-select[title="客户域 / VIP / 国内"]').first()
    await expect(selected.locator('.el-select__selected-item').filter({ hasText: '国内' })).toHaveText('国内')
    await selected.locator('.el-select__wrapper').click()
    await expect(rows).toHaveCount(4)
    await page.getByRole('option', { name: '客户域', exact: true }).click()
    await expect(page.locator('.el-select[title="客户域"]').first()).toBeVisible()
  })
}

test('inherits the selected domain when creating a glossary and preserves filters through detail', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries?owner_domain_id=2&status=draft')
  await expect(page.getByText('户外域', { exact: true }).first()).toBeVisible()

  const actions = page.locator('.table-actions').first()
  await expect(actions).toBeVisible()
  const actionLayout = await actions.evaluate(element => ({
    whiteSpace: getComputedStyle(element).whiteSpace,
    buttonTops: Array.from(element.querySelectorAll('button')).map(button => button.getBoundingClientRect().top)
  }))
  expect(actionLayout.whiteSpace).toBe('nowrap')
  expect(new Set(actionLayout.buttonTops).size).toBe(1)

  await page.getByRole('button', { name: '新建术语' }).click()
  const createDialog = page.getByRole('dialog', { name: '新建业务术语' })
  await expect(createDialog.getByText('户外域', { exact: true })).toBeVisible()
  await createDialog.getByRole('button', { name: '取消' }).click()

  await page.getByRole('button', { name: '详情' }).click()
  await expect(page).toHaveURL(/\/glossaries\/21\?owner_domain_id=2&status=draft$/)
  await expect(page.getByRole('textbox', { name: '术语名称' })).toHaveValue('领队')
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/glossaries\?owner_domain_id=2&status=draft$/)
})

test('protects unsaved glossary changes when leaving the detail page', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries/21?owner_domain_id=2&status=draft')

  const nameInput = page.getByRole('textbox', { name: '术语名称' })
  await nameInput.fill('尚未保存的领队')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: /返回/ }).click()
  const confirm = page.getByRole('dialog', { name: '有未保存的修改' })
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '继续编辑' }).click()
  await expect(page).toHaveURL(/\/glossaries\/21\?owner_domain_id=2&status=draft$/)
  await expect(nameInput).toHaveValue('尚未保存的领队')

  await page.getByRole('button', { name: /返回/ }).click()
  await page.getByRole('dialog', { name: '有未保存的修改' }).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/\/glossaries\?owner_domain_id=2&status=draft$/)
})

test('keeps local glossary edits when a stale version is rejected', async ({ page }) => {
  await installMockBackend(page, { glossaryVersionConflict: true })
  await page.goto('/glossaries/21')

  const nameInput = page.getByRole('textbox', { name: '术语名称' })
  await nameInput.fill('本地尚未保存的领队')
  const tags = page.getByRole('combobox', { name: '标签', exact: true })
  await tags.fill('尚未保存的治理标签')
  await tags.press('Enter')
  await page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true }).click()

  await expect(page.getByText('资源已被其他用户修改，请刷新后重试')).toBeVisible()
  await expect(nameInput).toHaveValue('本地尚未保存的领队')
  await expect(page.getByText('尚未保存的治理标签', { exact: true })).toBeVisible()
  await nameInput.fill('领队')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => window.dispatchEvent(new Event('beforeunload', { cancelable: true })))).toBe(false)
})

test('protects glossary history switching and restores the exact revision without dirtying the form', async ({ page }) => {
  await installMockBackend(page, { glossaryHistory: true })
  await page.goto('/glossaries/21?owner_domain_id=2&status=draft')
  const name = page.getByRole('textbox', { name: '术语名称' })
  const history = page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '修订历史' }) })
  await name.fill('尚未保存的领队')
  await history.getByRole('row').filter({ hasText: '草稿' }).click()
  await expect(name).toHaveValue('尚未保存的领队')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await history.getByRole('row').filter({ hasText: '已发布' }).click()
  const confirm = page.getByRole('dialog', { name: '有未保存的修改' })
  await confirm.getByRole('button', { name: '继续编辑' }).click()
  await expect(page).toHaveURL(/\/glossaries\/21\?owner_domain_id=2&status=draft$/)
  await expect(name).toHaveValue('尚未保存的领队')
  await history.getByRole('row').filter({ hasText: '已发布' }).click()
  await confirm.getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page).toHaveURL(/owner_domain_id=2&status=draft&revision_id=210$/)
  await expect(name).toHaveValue('历史领队')
  await expect(name).toBeDisabled()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await page.reload()
  await expect(name).toHaveValue('历史领队')
  await history.getByRole('row').filter({ hasText: '草稿' }).click()
  await expect(page).toHaveURL(/revision_id=211$/)
  await expect(name).toHaveValue('领队')
  await expect(name).toBeEditable()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/glossaries\?owner_domain_id=2&status=draft$/)
})

for (const revisionQuery of ['revision_id=999', 'revision_id=210', 'revision_id=0', 'revision_id=', 'revision_id=211&revision_id=210']) {
  test(`fails closed for unavailable or invalid glossary revision ${revisionQuery}`, async ({ page }) => {
    await installMockBackend(page, { glossaryHistory: true, glossaryRevisionDenied: true })
    await page.goto(`/glossaries/21?${revisionQuery}`)
    await expect(page.getByRole('alert')).toBeVisible()
    await expect(page).toHaveURL(new RegExp(`${revisionQuery}$`))
    await expect(page.getByRole('textbox', { name: '术语名称' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  })
}

test('opens a newly created glossary draft from a pinned published revision', async ({ page }) => {
  await installMockBackend(page, { glossaryPublicationHistory: true })
  await page.goto('/glossaries/21?owner_domain_id=2&revision_id=211')
  await page.getByRole('button', { name: '创建新修订' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox').fill('修订领队定义')
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page).toHaveURL(/owner_domain_id=2&revision_id=212$/)
  await expect(page.getByRole('textbox', { name: '术语名称' })).toBeEditable()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
})

for (const delayedPart of ['identity', 'revision']) {
test(`keeps a switched glossary history page when an earlier ${delayedPart} save completes`, async ({ page }) => {
  await installMockBackend(page, { glossaryHistory: true })
  let releaseSave
  const pendingSave = new Promise(resolve => { releaseSave = resolve })
  const delayedPath = delayedPart === 'identity' ? '/glossaries/21' : '/glossaries/21/revisions/211'
  await page.route(`**/standard${delayedPath}`, async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    await pendingSave
    await route.fallback()
  })
  await page.goto('/glossaries/21?revision_id=211')
  const name = page.getByRole('textbox', { name: '术语名称' })
  await name.fill('保存中的领队')
  const saveStarted = page.waitForRequest(request => request.method() === 'PUT' && request.url().endsWith(delayedPath))
  const saveButton = delayedPart === 'identity'
    ? page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true })
    : page.locator('.page-header').getByRole('button', { name: '保存', exact: true })
  await saveButton.click()
  await saveStarted
  await expect(name).toBeDisabled()
  const otherSaveButton = delayedPart === 'identity'
    ? page.locator('.page-header').getByRole('button', { name: '保存', exact: true })
    : page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true })
  await expect(otherSaveButton).toBeDisabled()
  await page.getByRole('row').filter({ hasText: '已发布' }).click()
  await page.getByRole('dialog', { name: '有未保存的修改' }).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(name).toHaveValue('历史领队')
  const saveFinished = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().endsWith(delayedPath))
  releaseSave()
  const saved = await saveFinished
  if (delayedPart === 'revision') expect(saved.request().postDataJSON().name).toBe('保存中的领队')
  else expect(saved.request().postDataJSON()).not.toHaveProperty('name')
  await expect(page).toHaveURL(/revision_id=210$/)
  await expect(name).toHaveValue('历史领队')
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
})
}

test('keeps failed glossary revision edits after independently saving ownership', async ({ page }) => {
  await installMockBackend(page)
  let revisionSaveFailed = false
  const identityVersions = []
  await page.route('**/standard/glossaries/21', async route => {
    if (route.request().method() === 'PUT') identityVersions.push(route.request().postDataJSON().version)
    return route.fallback()
  })
  await page.route('**/standard/glossaries/21/revisions/211', async route => {
    if (route.request().method() !== 'PUT' || revisionSaveFailed) return route.fallback()
    revisionSaveFailed = true
    return fulfillJSON(route, { error: '修订保存失败' }, 409)
  })
  await page.goto('/glossaries/21?revision_id=211')
  const name = page.getByRole('textbox', { name: '术语名称' })
  await name.fill('修订后的领队')
  const identitySave = page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true })
  const revisionSave = page.locator('.page-header').getByRole('button', { name: '保存', exact: true })
  await identitySave.click()
  await expect(revisionSave).toBeEnabled()
  await revisionSave.click()
  await expect(page.getByText('修订保存失败', { exact: true })).toBeVisible()
  await expect(name).toHaveValue('修订后的领队')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await revisionSave.click()
  expect(identityVersions).toEqual([1])
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/glossaries$/)
})

for (const firstSection of ['identity', 'revision']) {
  test(`glossary saves ${firstSection} independently and preserves the other dirty section`, async ({ page }) => {
    await installMockBackend(page)
    const writes = []
    page.on('request', request => {
      if (request.method() === 'PUT' && request.url().includes('/glossaries/21')) writes.push({ path: new URL(request.url()).pathname, body: request.postDataJSON() })
    })
    await page.goto('/glossaries/21?revision_id=211')
    const name = page.getByRole('textbox', { name: '术语名称' })
    await name.fill('更新后的领队')
    const tags = page.getByRole('combobox', { name: '标签', exact: true })
    await tags.fill('治理标签')
    await tags.press('Enter')
    const buttons = {
      identity: page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true }),
      revision: page.locator('.page-header').getByRole('button', { name: '保存', exact: true })
    }
    await buttons[firstSection].click()
    await expect(name).toBeEnabled()
    expect(writes).toHaveLength(1)
    await expect(name).toHaveValue('更新后的领队')
    await expect(page.getByText('治理标签', { exact: true })).toBeVisible()
    await expect(page.getByText('未保存', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: /返回/ }).click()
    await page.getByRole('dialog', { name: '有未保存的修改' }).getByRole('button', { name: '继续编辑' }).click()
    await buttons[firstSection === 'identity' ? 'revision' : 'identity'].click()
    await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
    expect(writes).toHaveLength(2)
    expect(writes.map(write => write.body.version)).toEqual([1, 2])
    const identityWrite = writes.find(write => write.path.endsWith('/21')).body
    const revisionWrite = writes.find(write => write.path.endsWith('/211')).body
    expect(Object.keys(identityWrite).sort()).toEqual(['owner_domain_id', 'scope_type', 'tags', 'version'])
    expect(identityWrite.tags).toEqual(['治理标签'])
    expect(revisionWrite.name).toBe('更新后的领队')
    expect(revisionWrite).not.toHaveProperty('tags')
    await page.reload()
    await expect(name).toHaveValue('更新后的领队')
    await expect(page.getByText('治理标签', { exact: true })).toBeVisible()
  })
}

for (const language of ['zh-cn', 'en']) {
  test(`published glossary ownership is independently editable in ${language}`, async ({ page }) => {
    await installMockBackend(page, { glossaryPublicationHistory: true, language })
    if (language === 'en') await page.setViewportSize({ width: 760, height: 1000 })
    const writes = []
    page.on('request', request => { if (request.method() === 'PUT') writes.push(request) })
    await page.goto('/glossaries/21?revision_id=211')
    const en = language === 'en'
    const name = page.getByRole('textbox', { name: en ? 'Term Name' : '术语名称', exact: true })
    await expect(name).toBeDisabled()
    const save = page.getByRole('button', { name: en ? 'Save' : '保存', exact: true })
    await expect(save).toHaveCount(1)
    const tags = page.getByRole('combobox', { name: en ? 'Tags' : '标签', exact: true })
    await tags.fill('ownership-only')
    await tags.press('Enter')
    await save.click()
    await expect(page.getByText(en ? 'Unsaved' : '未保存', { exact: true })).toHaveCount(0)
    expect(writes).toHaveLength(1)
    expect(writes[0].url()).toMatch(/\/glossaries\/21$/)
    await expect(page).toHaveURL(/revision_id=211$/)
    await expect(name).toHaveValue('领队')
    await page.reload()
    await expect(page.getByText('ownership-only', { exact: true })).toBeVisible()
    await expect(name).toBeDisabled()
  })
}

test('glossary ownership save does not validate an unfinished draft and revision save does not validate ownership edits', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/glossaries/21')
  const name = page.getByRole('textbox', { name: '术语名称' })
  await name.fill('')
  const identitySave = page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true })
  await identitySave.click()
  await expect(page.getByText('保存成功', { exact: true })).toBeVisible()
  await expect(name).toHaveValue('')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await name.fill('领队')
  const scope = page.locator('.el-form-item').filter({ has: page.getByText('适用范围', { exact: true }) }).locator('.el-select')
  await scope.click()
  await page.getByRole('option', { name: '租户公共', exact: true }).click()
  await scope.click()
  await page.getByRole('option', { name: '业务域专属', exact: true }).click()
  await page.locator('.page-header').getByRole('button', { name: '保存', exact: true }).click()
  await expect(name).toBeEnabled()
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await identitySave.click()
  await expect(page.getByText('选择业务域', { exact: true })).toBeVisible()
})

const mappingFixtureElements = [{ id: 41, code: 'person_id', name: '人员标识', data_type: 'string', status: 'draft' }]

async function selectGlossaryElement(page, language = 'zh-cn') {
  const en = language === 'en'
  await page.getByRole('button', { name: en ? 'Manage Data Elements' : '管理关联数据元', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: en ? 'Manage Data Elements' : '管理关联数据元', exact: true })
  await dialog.locator('.el-select').click()
  await page.getByRole('option', { name: '人员标识 (person_id)', exact: true }).click()
  await dialog.getByRole('heading').click()
  return dialog
}

test('glossary mapping save preserves both dirty forms and updates only the mapping list', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements })
  const reads = [], writes = []
  page.on('request', request => {
    if (!request.url().includes('/standard/glossaries/21')) return
    if (request.method() === 'GET') reads.push(new URL(request.url()).pathname)
    if (request.method() === 'PUT') writes.push({ path: new URL(request.url()).pathname, body: request.postDataJSON() })
  })
  await page.goto('/glossaries/21?revision_id=211')
  const name = page.getByRole('textbox', { name: '术语名称' })
  await name.fill('未保存的领队说明')
  const tags = page.getByRole('combobox', { name: '标签', exact: true })
  await tags.fill('未保存的标签')
  await tags.press('Enter')
  const dialog = await selectGlossaryElement(page)
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'person_id' })).toBeVisible()
  await expect(name).toHaveValue('未保存的领队说明')
  await expect(page.getByText('未保存的标签', { exact: true })).toBeVisible()
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  expect(reads.filter(path => path.endsWith('/21'))).toHaveLength(1)
  expect(reads.filter(path => path.endsWith('/revisions/211'))).toHaveLength(1)
  expect(reads.filter(path => path.endsWith('/elements'))).toHaveLength(2)
  expect(writes).toEqual([{ path: '/api/v1/standard/glossaries/21/elements', body: { version: 1, element_ids: [41] } }])
  await page.locator('.section-card').filter({ has: page.getByRole('heading', { name: '稳定身份与治理归属' }) }).getByRole('button', { name: '保存', exact: true }).click()
  await expect(name).toBeEnabled()
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await page.locator('.page-header').getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  expect(writes.map(write => write.body.version)).toEqual([1, 2, 3])
})

test('failed glossary mapping save retains the selection and retries with the unchanged version', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements })
  const writes = []
  await page.route('**/glossaries/21/elements', async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    writes.push(route.request().postDataJSON())
    if (writes.length === 1) return fulfillJSON(route, { error: '关联保存失败' }, 409)
    return route.fallback()
  })
  await page.goto('/glossaries/21')
  const name = page.getByRole('textbox', { name: '术语名称' })
  await name.fill('仍需保存的定义')
  const dialog = await selectGlossaryElement(page)
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page.getByText('关联保存失败', { exact: true })).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('.el-tag')).toContainText('人员标识')
  await expect(name).toHaveValue('仍需保存的定义')
  await expect(page.getByText('未保存', { exact: true })).toBeVisible()
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'person_id' })).toBeVisible()
  expect(writes).toEqual([{ version: 1, element_ids: [41] }, { version: 1, element_ids: [41] }])
})

test('glossary mapping submission is single flight and cancel sends no write', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements })
  const writes = []
  let releaseSave
  const pending = new Promise(resolve => { releaseSave = resolve })
  await page.route('**/glossaries/21/elements', async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    writes.push(route.request().postDataJSON())
    await pending
    return route.fallback()
  })
  await page.goto('/glossaries/21')
  let dialog = await selectGlossaryElement(page)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(writes).toHaveLength(0)
  dialog = await selectGlossaryElement(page)
  await dialog.getByRole('button', { name: '确定', exact: true }).evaluate(button => { button.click(); button.click() })
  await expect.poll(() => writes.length).toBe(1)
  await expect(dialog.locator('.el-select__wrapper')).toHaveClass(/is-disabled/)
  await expect(dialog.getByRole('combobox')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '取消', exact: true })).toBeDisabled()
  await expect(page.locator('.page-header').getByRole('button', { name: '保存', exact: true })).toBeDisabled()
  releaseSave()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'person_id' })).toBeVisible()
  expect(writes).toHaveLength(1)
})

for (const language of ['zh-cn', 'en']) {
  test(`saved glossary mapping has read-only recovery after list failure in ${language}`, async ({ page }) => {
    await installMockBackend(page, { elements: mappingFixtureElements, language })
    let reads = 0, writes = 0
    await page.route('**/glossaries/21/elements', async route => {
      if (route.request().method() === 'PUT') writes += 1
      else if (++reads === 2) return fulfillJSON(route, { error: 'mapping list unavailable' }, 503)
      return route.fallback()
    })
    await page.goto('/glossaries/21')
    const en = language === 'en'
    const name = page.getByRole('textbox', { name: en ? 'Term Name' : '术语名称', exact: true })
    await name.fill('unsaved term')
    const dialog = await selectGlossaryElement(page, language)
    await dialog.getByRole('button', { name: en ? 'Confirm' : '确定', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    await expect(page.locator('.glossary-detail').getByRole('alert')).toContainText(en ? 'Links were saved' : '关联已保存')
    await expect(name).toHaveValue('unsaved term')
    await expect(page.getByText(en ? 'Unsaved' : '未保存', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: en ? 'Manage Data Elements' : '管理关联数据元', exact: true })).toBeDisabled()
    await page.getByRole('button', { name: en ? 'Refresh' : '刷新', exact: true }).click()
    await expect(page.getByRole('row').filter({ hasText: 'person_id' })).toBeVisible()
    await expect(page.locator('.glossary-detail').getByRole('alert')).toHaveCount(0)
    expect(writes).toBe(1)
    expect(reads).toBe(3)
    await expect(name).toHaveValue('unsaved term')
  })
}

test('late mapping refresh does not overwrite a newly selected glossary revision', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements, glossaryHistory: true })
  let reads = 0, releaseRead
  const pending = new Promise(resolve => { releaseRead = resolve })
  await page.route('**/glossaries/21/elements', async route => {
    if (route.request().method() === 'GET' && ++reads === 2) {
      await pending
      return fulfillJSON(route, { error: 'stale mapping failure' }, 503)
    }
    return route.fallback()
  })
  await page.goto('/glossaries/21?revision_id=211')
  const dialog = await selectGlossaryElement(page)
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect.poll(() => reads).toBe(2)
  await page.getByRole('row').filter({ hasText: '已发布' }).click()
  const name = page.getByRole('textbox', { name: '术语名称' })
  await expect(name).toHaveValue('历史领队')
  const response = page.waitForResponse(res => res.url().endsWith('/glossaries/21/elements') && res.status() === 503)
  releaseRead()
  await response
  await expect(page).toHaveURL(/revision_id=210$/)
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.getByRole('row').filter({ hasText: 'person_id' })).toBeVisible()
  await expect(name).toHaveValue('历史领队')
})

test('leaving glossary detail ignores a late mapping write response', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements })
  let releaseSave
  const pending = new Promise(resolve => { releaseSave = resolve })
  await page.route('**/glossaries/21/elements', async route => {
    if (route.request().method() !== 'PUT') return route.fallback()
    await pending
    return route.fallback()
  })
  await page.goto('/glossaries')
  await page.getByRole('row').filter({ hasText: 'leader' }).getByRole('button', { name: '详情', exact: true }).click()
  const dialog = await selectGlossaryElement(page)
  const started = page.waitForRequest(request => request.method() === 'PUT' && request.url().endsWith('/glossaries/21/elements'))
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await started
  await page.goBack()
  await expect(page).toHaveURL(/\/glossaries$/)
  const finished = page.waitForResponse(response => response.request().method() === 'PUT' && response.url().endsWith('/glossaries/21/elements'))
  releaseSave()
  await finished
  await expect(page.getByRole('heading', { name: '业务术语词典' })).toBeVisible()
  await expect(page.getByText('保存成功', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

for (const language of ['zh-cn', 'en']) {
  test(`glossary mappings distinguish effectiveness and preserve every identity in ${language}`, async ({ page }) => {
    const mappings = [
      { id: 41, code: 'draft_person', name: '草稿人员', status: 'draft', is_effective: false },
      { id: 42, code: 'future_activity', name: '待生效活动', status: 'published', is_effective: false },
      { id: 43, code: 'withdrawn_leader', name: '已撤回领队', status: 'withdrawn', is_effective: false },
      { id: 44, code: 'effective_member', name: '生效成员', status: 'published', is_effective: true }
    ].map(item => ({ ...item, revision_id: item.id * 10 + 1, revision_no: 1, lifecycle_state: 'active' }))
    await installMockBackend(page, { language, elements: mappings.map(item => ({ ...item, data_type: 'string' })), glossaryElementIDs: mappings.map(item => item.id) })
    await page.route('**/glossaries/21/elements', route => route.request().method() === 'GET' ? fulfillJSON(route, mappings) : route.fallback())
    await page.goto('/glossaries/21')
    const en = language === 'en'
    const card = page.locator('.section-card').filter({ has: page.getByRole('heading', { name: en ? 'Related Data Elements' : '关联的数据元', exact: true }) })
    await expect(card.getByText(en ? 'No Effective Revision' : '无当前生效修订', { exact: true })).toHaveCount(3)
    const futureRow = card.getByRole('row').filter({ hasText: 'future_activity' })
    await expect(futureRow).toContainText(en ? 'Published' : '已发布')
    await expect(futureRow).toContainText(en ? 'No Effective Revision' : '无当前生效修订')
    await expect(card.getByRole('row').filter({ hasText: 'effective_member' })).toContainText(en ? 'Currently Effective' : '当前生效')
    await page.getByRole('button', { name: en ? 'Manage Data Elements' : '管理关联数据元', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: en ? 'Manage Data Elements' : '管理关联数据元', exact: true })
    await expect(dialog.locator('.el-tag')).toHaveCount(4)
    const saved = page.waitForRequest(request => request.method() === 'PUT' && request.url().endsWith('/glossaries/21/elements'))
    await dialog.getByRole('button', { name: en ? 'Confirm' : '确定', exact: true }).click()
    expect((await saved).postDataJSON()).toEqual({ version: 1, element_ids: [41, 42, 43, 44] })
    await expect(dialog).not.toBeVisible()
    await page.reload()
    await expect(card.getByRole('row').filter({ hasText: 'draft_person' })).toBeVisible()
    await expect(card.getByRole('row').filter({ hasText: 'withdrawn_leader' })).toBeVisible()
  })
}

test('glossary mapping labels survive an absent first candidate page without detail reads', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements, glossaryElementIDs: [41] })
  let releaseSearch
  const pending = new Promise(resolve => { releaseSearch = resolve })
  const elementReads = []
  page.on('request', request => {
    if (request.method() === 'GET' && request.url().includes('/standard/elements')) elementReads.push(new URL(request.url()).pathname)
  })
  await page.route('**/standard/elements?**', async route => {
    await pending
    return fulfillJSON(route, { data: [], total: 0 })
  })
  await page.goto('/glossaries/21')
  await page.getByRole('button', { name: '管理关联数据元', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '管理关联数据元', exact: true })
  await expect(dialog.locator('.el-tag')).toHaveText(['人员标识 (person_id)'])
  const response = page.waitForResponse(response => response.url().includes('/standard/elements?'))
  releaseSearch()
  await response
  await dialog.locator('.el-select').click()
  await expect(page.getByRole('option', { name: '人员标识 (person_id)', exact: true })).toBeVisible()
  await expect(dialog.locator('.el-tag')).toHaveText(['人员标识 (person_id)'])
  await dialog.getByRole('heading').click()
  const save = page.waitForRequest(request => request.method() === 'PUT' && request.url().endsWith('/glossaries/21/elements'))
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  expect((await save).postDataJSON()).toEqual({ version: 1, element_ids: [41] })
  await expect(dialog).not.toBeVisible()
  expect(elementReads.length).toBeGreaterThan(0)
  expect([...new Set(elementReads)]).toEqual(['/api/v1/standard/elements'])
})

test('glossary selector retains selected labels but not unselected historical candidates', async ({ page }) => {
  const extraElements = [
    { id: 42, code: 'activity_id', name: '活动标识', data_type: 'string', status: 'draft' },
    { id: 43, code: 'unused', name: '未选候选', data_type: 'string', status: 'draft' }
  ]
  await installMockBackend(page, { elements: [...mappingFixtureElements, ...extraElements], glossaryElementIDs: [41] })
  await page.route('**/standard/elements?**', route => {
    const keyword = new URL(route.request().url()).searchParams.get('keyword')
    if (!keyword) return route.fallback()
    if (keyword === '失败') return fulfillJSON(route, { error: '候选查询失败' }, 503)
    return fulfillJSON(route, { data: [], total: 0 })
  })
  await page.goto('/glossaries/21')
  await page.getByRole('button', { name: '管理关联数据元', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '管理关联数据元', exact: true })
  await dialog.locator('.el-select').click()
  await expect(page.getByRole('option', { name: '人员标识 (person_id)', exact: true })).toHaveCount(1)
  await page.getByRole('option', { name: '活动标识 (activity_id)', exact: true }).click()
  for (const keyword of ['无结果', '失败']) {
    const response = page.waitForResponse(response => response.url().includes('/standard/elements?') && new URL(response.url()).searchParams.get('keyword') === keyword)
    await dialog.getByRole('combobox').fill(keyword)
    await response
    await expect(page.getByRole('option', { name: '未选候选 (unused)', exact: true })).toHaveCount(0)
    await expect(dialog.locator('.el-tag')).toHaveText(['人员标识 (person_id)', '活动标识 (activity_id)'])
    await expect(page.getByRole('option', { name: '活动标识 (activity_id)', exact: true })).toHaveCount(1)
  }
  await dialog.getByRole('heading').click()
  const save = page.waitForRequest(request => request.method() === 'PUT' && request.url().endsWith('/glossaries/21/elements'))
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  expect((await save).postDataJSON()).toEqual({ version: 1, element_ids: [41, 42] })
  await expect(dialog).not.toBeVisible()
})

test('glossary selector removes deselected pinned options and restores saved mappings after cancel', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements, glossaryElementIDs: [41] })
  await page.route('**/standard/elements?**', route => fulfillJSON(route, { data: [], total: 0 }))
  const writes = []
  page.on('request', request => {
    if (request.method() === 'PUT') writes.push(request.postDataJSON())
  })
  await page.goto('/glossaries/21')
  const manage = page.getByRole('button', { name: '管理关联数据元', exact: true })
  await manage.click()
  const dialog = page.getByRole('dialog', { name: '管理关联数据元', exact: true })
  await dialog.locator('.el-select').click()
  await page.getByRole('option', { name: '人员标识 (person_id)', exact: true }).click()
  await expect(dialog.locator('.el-tag')).toHaveCount(0)
  await expect(page.getByRole('option', { name: '人员标识 (person_id)', exact: true })).toHaveCount(0)
  await dialog.getByRole('heading').click()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes).toEqual([])
  await manage.click()
  await expect(dialog.locator('.el-tag')).toHaveText(['人员标识 (person_id)'])
  await dialog.locator('.el-tag__close').click()
  await expect(dialog.locator('.el-tag')).toHaveCount(0)
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  expect(writes).toEqual([{ version: 1, element_ids: [] }])
  await expect(page.getByRole('row').filter({ hasText: 'person_id' })).toHaveCount(0)
})

test('glossary element search keeps the newest candidate response', async ({ page }) => {
  await installMockBackend(page, { elements: mappingFixtureElements })
  let releaseSearch
  const pending = new Promise(resolve => { releaseSearch = resolve })
  await page.route('**/standard/elements?**', async route => {
    if (new URL(route.request().url()).searchParams.get('keyword')) return route.fallback()
    await pending
    return fulfillJSON(route, { data: [{ id: 42, code: 'old', name: '过期候选' }], total: 1 })
  })
  await page.goto('/glossaries/21')
  await page.getByRole('button', { name: '管理关联数据元', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '管理关联数据元', exact: true })
  await dialog.getByRole('combobox').fill('人员')
  await expect(page.getByRole('option', { name: '人员标识 (person_id)', exact: true })).toBeVisible()
  const oldResponse = page.waitForResponse(response => response.url().includes('/standard/elements?') && !new URL(response.url()).searchParams.get('keyword'))
  releaseSearch()
  await oldResponse
  await expect(page.getByRole('option', { name: '过期候选 (old)', exact: true })).toHaveCount(0)
  await page.getByRole('option', { name: '人员标识 (person_id)', exact: true }).click()
  await dialog.getByRole('heading').click()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
})

test('keeps the latest glossary filter result when an older request returns later', async ({ page }) => {
  await installMockBackend(page, { glossaryListRace: true })
  const oldRequest = page.waitForRequest(request => (
    request.url().includes('/api/v1/standard/glossaries?') &&
    new URL(request.url()).searchParams.get('keyword') === 'old'
  ))
  await page.goto('/glossaries?keyword=old')
  await oldRequest

  const keywordInput = page.getByPlaceholder('搜索术语名称或定义')
  await keywordInput.fill('new')
  await keywordInput.press('Tab')

  await expect(page.getByText('新筛选结果', { exact: true })).toBeVisible()
  await expect(page.getByText('旧筛选结果', { exact: true })).toHaveCount(0)
})

test('expands, collapses and searches measurement units and keeps the effective interval together', async ({ page }) => {
  await installMockBackend(page, { elements: [{ id: 41, code: 'phone', name: '电话', data_type: 'string', status: 'draft' }] })
  await page.route('**/api/v1/standard/units*', route => fulfillJSON(route, [
    { id: 1, name: '米', symbol: 'm', category_id: 10, category: { name: '长度', sort_order: 1 } },
    { id: 2, name: '克', symbol: 'g', category_id: 20, category: { name: '质量', sort_order: 2 } }
  ]))
  await page.goto('/elements/41')
  await expect(page.getByText('责任人用户 ID', { exact: true })).toHaveCount(0)
  await expect(page.getByText('可选；留空表示不限制最大字符数', { exact: true })).toBeVisible()
  const units = page.getByRole('combobox', { name: '计量单位', exact: true })
  await page.locator('.unit-select .el-select__wrapper').click()
  await expect(page.getByText('长度', { exact: true }).last()).toBeVisible()
  await expect(page.getByText('质量', { exact: true })).toBeVisible()
  const lengthCategory = page.locator('.el-tree-node').filter({ has: page.getByRole('option', { name: '长度', exact: true }) }).first()
  await expect(lengthCategory).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByRole('option', { name: '米 (m)', exact: true })).not.toBeVisible()
  await page.getByRole('option', { name: '长度', exact: true }).click()
  await expect(lengthCategory).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('option', { name: '米 (m)', exact: true })).toBeVisible()
  await expect(page.locator('.unit-select .el-select__selected-item').filter({ hasText: '长度' })).toHaveCount(0)
  await page.getByRole('option', { name: '长度', exact: true }).click()
  await expect(lengthCategory).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByRole('option', { name: '米 (m)', exact: true })).not.toBeVisible()
  await units.fill('质量')
  await expect(page.getByRole('option', { name: '克 (g)', exact: true })).toBeVisible()
  await expect(page.getByRole('option', { name: '米 (m)', exact: true })).not.toBeVisible()
  await units.fill('不存在的单位')
  await expect(page.getByRole('option', { name: '克 (g)', exact: true })).not.toBeVisible()
  await units.fill('G')
  await page.getByRole('option', { name: '克 (g)', exact: true }).click()
  await expect(units).toHaveValue('')
  await expect(page.locator('.el-select.unit-select')).toContainText('克 (g)')
  await page.locator('.unit-select .el-select__wrapper').click()
  await expect(page.getByRole('option', { name: '克 (g)', exact: true })).toBeVisible()
  await units.press('Escape')
  await page.locator('.el-select.unit-select').hover()
  await page.locator('.unit-select .el-select__clear').click()
  await expect(page.locator('.el-select.unit-select')).not.toContainText('克 (g)')
  const interval = page.locator('.effective-interval')
  const tops = await interval.locator('.el-form-item').evaluateAll(items => items.map(item => Math.round(item.getBoundingClientRect().top)))
  expect(tops).toHaveLength(2)
  expect(tops[0]).toBe(tops[1])
  await expect(page.locator('#app').getByText('输入示例值，按回车添加', { exact: true })).toBeVisible()
  const examples = page.getByRole('combobox', { name: '示例值', exact: true })
  await examples.fill('010-12345678')
  await examples.press('Enter')
  await expect(page.locator('.el-tag').filter({ hasText: '010-12345678' })).toBeVisible()
})

test('opens the saved measurement unit category and clears its reference', async ({ page }) => {
  await installMockBackend(page, { theme: 'dark', elements: [{ id: 41, code: 'weight', name: '重量', data_type: 'float', status: 'draft', unit_id: 2 }] })
  await page.route('**/api/v1/standard/units*', route => fulfillJSON(route, [
    { id: 1, name: '米', symbol: 'm', category_id: 10, category: { name: '长度', sort_order: 1 } },
    { id: 2, name: '克', symbol: 'g', category_id: 20, category: { name: '质量', sort_order: 2 } }
  ]))
  await page.goto('/elements/41')
  const selector = page.locator('.el-select.unit-select')
  await expect(selector).toContainText('克 (g)')
  await selector.locator('.el-select__wrapper').click()
  await expect(page.getByRole('option', { name: '克 (g)', exact: true })).toBeVisible()
  await expect(page.getByRole('option', { name: '米 (m)', exact: true })).not.toBeVisible()
  await selector.getByRole('combobox').press('Escape')
  await selector.hover()
  await selector.locator('.el-select__clear').click()
  const savedRequest = page.waitForRequest(request => request.method() === 'PUT' && request.url().endsWith('/elements/41/revisions/411'))
  await page.locator('.page-header').getByRole('button', { name: '保存', exact: true }).click()
  expect((await savedRequest).postDataJSON().unit_id).toBeNull()
})

for (const language of ['zh-cn', 'en']) {
  test(`explains type choices with two-line options and contextual guidance in ${language}`, async ({ page }) => {
    await installMockBackend(page, { language, elements: [{ id: 41, code: 'phone', name: 'Phone', data_type: 'string', status: 'draft' }] })
    await page.goto('/elements/41')
    const selector = page.locator('.data-type-field')
    const hint = selector.getByRole('status')
    await expect(hint).toContainText(language === 'zh-cn' ? '保留前导零' : 'preserve leading zeros')
    const choices = language === 'zh-cn'
      ? [['整数', '-2,147,483,648'], ['大整数', '9.22×10¹⁸'], ['近似小数', '微小计算误差'], ['精确小数', '除法等运算仍可能需要舍入']]
      : [['Integer', '-2,147,483,648'], ['Large integer', '9.22 × 10¹⁸'], ['Approximate decimal', 'small calculation errors'], ['Exact decimal', 'division may still require rounding']]
    for (const [label, guidance] of choices) {
      await selector.locator('.el-select__wrapper').click()
      const option = page.getByRole('option').filter({ has: page.locator('.data-type-name').getByText(label, { exact: true }) })
      await expect(option.locator('.data-type-example')).toContainText(language === 'zh-cn' ? '例如：' : 'Examples:')
      const layout = await option.evaluate(node => ({
        nameBottom: node.querySelector('.data-type-name').getBoundingClientRect().bottom,
        exampleTop: node.querySelector('.data-type-example').getBoundingClientRect().top,
        fits: node.scrollWidth <= node.clientWidth
      }))
      expect(layout.exampleTop).toBeGreaterThanOrEqual(layout.nameBottom)
      expect(layout.fits).toBe(true)
      await option.click()
      await expect(hint).toContainText(guidance)
      expect(await hint.evaluate(node => node.scrollWidth <= node.clientWidth)).toBe(true)
    }
  })
}

for (const kind of ['code-set', 'metric']) {
  test(`creates a ${kind} without a change summary`, async ({ page }) => {
    await installMockBackend(page, { metrics: [{ id: 51, code: 'customer_count', name: '客户数', type: 'atomic', status: 'draft' }] })
    const path = kind === 'code-set' ? '/code-sets' : '/metrics'
    let payload
    await page.route(`**/api/v1/standard${path}`, async route => {
      if (route.request().method() !== 'POST') return route.fallback()
      payload = route.request().postDataJSON()
      return fulfillJSON(route, { id: kind === 'code-set' ? 31 : 51, version: 1 }, 201)
    })
    await page.goto(path)
    await page.getByRole('button', { name: kind === 'code-set' ? '新建码值集' : '新增指标', exact: true }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('textbox', { name: '变更说明' })).toHaveCount(0)
    await dialog.getByRole('textbox', { name: /编码/ }).fill(kind === 'code-set' ? 'customer_status' : 'customer_count')
    await dialog.getByRole('textbox', { name: /名称/ }).fill('客户标准')
    if (kind === 'code-set') {
      await dialog.getByRole('textbox', { name: /描述/ }).fill('客户状态')
    } else {
      await dialog.getByRole('textbox', { name: /业务口径/ }).fill('客户数量')
      await dialog.getByRole('textbox', { name: /统计口径/ }).fill('按客户计数')
    }
    await dialog.getByRole('button', { name: '确定', exact: true }).click()
    await expect(dialog).not.toBeVisible()
    expect(payload).toBeDefined()
    expect(payload).not.toHaveProperty('change_summary')
    if (kind === 'metric') {
      expect(payload).not.toHaveProperty('dependency_ids')
      expect(payload.dependencies).toEqual([])
    }
  })
}

test('creates a data element without asking for a change summary', async ({ page }) => {
  await installMockBackend(page)
  let payload
  await page.route('**/api/v1/standard/elements', async route => {
    if (route.request().method() !== 'POST') return route.fallback()
    payload = route.request().postDataJSON()
    return fulfillJSON(route, { id: 41, code: payload.code, version: 1 }, 201)
  })
  await page.goto('/elements')
  await page.getByRole('button', { name: '新建数据元' }).click()
  const dialog = page.getByRole('dialog', { name: '新建数据元' })
  await dialog.locator('.el-form-item').filter({ hasText: '数据类型' }).locator('.el-select__wrapper').click()
  await expect(page.getByRole('option', { name: '文本 例如：名称、电话号码、说明', exact: true })).toBeVisible()
  await expect(page.getByRole('option', { name: 'text', exact: true })).toHaveCount(0)
  await page.getByRole('option', { name: '文本 例如：名称、电话号码、说明', exact: true }).click()
  await dialog.getByRole('spinbutton', { name: '最大长度', exact: true }).fill('32')
  await expect(dialog.getByRole('textbox', { name: '变更说明' })).toHaveCount(0)
  await dialog.getByRole('textbox', { name: '英文编码' }).fill('customer_id')
  await dialog.getByRole('textbox', { name: '中文名称' }).fill('客户标识')
  await dialog.getByRole('textbox', { name: '业务含义' }).fill('客户的唯一标识')
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page).toHaveURL(/\/elements\/41$/)
  expect(payload).toMatchObject({ code: 'customer_id', name: '客户标识', definition: '客户的唯一标识', data_type: 'string', length: 32 })
  expect(payload).not.toHaveProperty('change_summary')
})

test('submits a new glossary only once when the confirm action fires twice', async ({ page }) => {
  const backend = await installMockBackend(page, { delayedGlossaryCreate: true })
  await page.goto('/glossaries')
  await page.getByRole('button', { name: '新建术语' }).click()

  const dialog = page.getByRole('dialog', { name: '新建业务术语' })
  await dialog.getByRole('textbox', { name: '编码' }).fill('duplicate_submit')
  await dialog.getByRole('textbox', { name: '术语名称' }).fill('重复提交测试')
  await dialog.getByRole('textbox', { name: '定义' }).fill('验证写请求只能发送一次')
  await expect(dialog.getByRole('textbox', { name: '变更说明' })).toHaveCount(0)
  const createRequest = page.waitForRequest(request => request.method() === 'POST' && request.url().endsWith('/glossaries'))
  const confirmButton = dialog.getByRole('button', { name: '确定' })
  await confirmButton.evaluate(button => {
    button.click()
    button.click()
  })

  await expect(dialog).not.toBeVisible()
  expect(backend.getGlossaryCreateRequests()).toBe(1)
  expect((await createRequest).postDataJSON()).not.toHaveProperty('change_summary')
})

test('hides glossary delete after any publication history exists', async ({ page }) => {
  const backend = await installMockBackend(page, { glossaryPublicationHistory: true })
  await page.goto('/glossaries')

  const row = page.getByRole('row').filter({ hasText: 'leader' })
  await expect(row.getByRole('button', { name: '详情' })).toBeVisible()
  await expect(row.getByRole('button', { name: '删除' })).toHaveCount(0)
  expect(backend.getDeleteRequests()).toEqual([])
})

test('keeps tree actions on one line and canceling delete sends no request', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/domains')

  const rows = page.locator('.tree-node')
  await expect(rows).toHaveCount(2)
  const layout = await rows.evaluateAll(elements => elements.map(element => {
    const buttons = Array.from(element.querySelectorAll('.node-actions button'))
    return {
      height: element.getBoundingClientRect().height,
      buttonTops: buttons.map(button => Math.round(button.getBoundingClientRect().top)),
      whiteSpace: getComputedStyle(element.querySelector('.node-actions')).whiteSpace
    }
  }))

  for (const row of layout) {
    expect(row.height).toBeLessThanOrEqual(30)
    expect(new Set(row.buttonTops).size).toBe(1)
    expect(row.whiteSpace).toBe('nowrap')
  }

  await rows.filter({ hasText: '户外域' }).getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除业务域「户外域」？')
  await confirm.getByRole('button', { name: '取消' }).click()
  await expect(confirm).not.toBeVisible()
  expect(backend.getDeleteRequests()).toEqual([])
  await expect(page.locator('.el-message--error')).toHaveCount(0)
})

test('keeps local domain edits when a stale version is rejected', async ({ page }) => {
  const backend = await installMockBackend(page, { domainVersionConflict: true })
  await page.goto('/domains')

  await page.locator('.tree-node').filter({ hasText: '户外域' }).getByRole('button', { name: '编辑' }).click()
  const dialog = page.getByRole('dialog', { name: '编辑业务域' })
  const nameInput = dialog.getByRole('textbox', { name: '名称' })
  await nameInput.fill('本地尚未保存的户外域')
  await dialog.getByRole('button', { name: '确定' }).click()

  await expect(page.getByText('资源已被其他用户修改，请刷新后重试')).toBeVisible()
  await expect(dialog).toBeVisible()
  await expect(nameInput).toHaveValue('本地尚未保存的户外域')
  expect(backend.getDomainUpdateRequests()).toEqual([{ id: 2, version: 1, name: '本地尚未保存的户外域' }])
})

test('supports keyboard focus and escape dismissal in domain dialogs', async ({ page }) => {
  const backend = await installMockBackend(page)
  await page.goto('/domains')

  const createButton = page.getByRole('button', { name: '新建业务域' })
  await createButton.click()
  const createDialog = page.getByRole('dialog', { name: '新建业务域' })
  const nameInput = createDialog.getByRole('textbox', { name: '名称' })
  await expect(nameInput).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(createDialog).not.toBeVisible()
  await expect(createButton).toBeFocused()

  const deleteButton = page.locator('.tree-node').filter({ hasText: '户外域' }).getByRole('button', { name: '删除' })
  await deleteButton.click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm.getByRole('button', { name: '确定' })).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(confirm).not.toBeVisible()
  await expect(deleteButton).toBeFocused()
  expect(backend.getDeleteRequests()).toEqual([])
})

test('avoids page overflow and keeps table actions on one line at narrow width', async ({ page }) => {
  await page.setViewportSize({ width: 720, height: 760 })
  await installMockBackend(page)

  for (const path of ['/domains', '/code-sets', '/documents']) {
    await page.goto(path)
    await expect.poll(() => page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth
    }))).toEqual({ clientWidth: 720, scrollWidth: 720 })
  }

  await page.goto('/code-sets')
  const actions = page.locator('.table-actions').first()
  await expect(actions).toBeVisible()
  const actionLayout = await actions.locator('button').evaluateAll(buttons => ({
    tops: buttons.map(button => Math.round(button.getBoundingClientRect().top)),
    whiteSpace: getComputedStyle(buttons[0].parentElement).whiteSpace
  }))
  expect(new Set(actionLayout.tops).size).toBe(1)
  expect(actionLayout.whiteSpace).toBe('nowrap')
})

test('preserves code-set filters through detail and back navigation', async ({ page }) => {
  await installMockBackend(page)
  await page.goto('/code-sets?keyword=gender&scope_type=domain')
  await page.getByText('性别', { exact: true }).click()
  await expect(page).toHaveURL(/\/code-sets\/31\?keyword=gender&scope_type=domain$/)
  await expect(page.getByText('性别', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/code-sets\?keyword=gender&scope_type=domain$/)
})

test('shows compiled standard constraints without a uniqueness editor', async ({ page }) => {
  await installMockBackend(page, {
    elements: [{
      id: 41, name: '活动编号', code: 'activity_id', data_type: 'string', domain_id: 2, status: 'approved',
      compiled_quality_rules: { schema_version: 'addp.quality.rules/v1', rules: [{
        rule_key: '00000000-0000-4000-8000-000000000001', type: 'length', enabled: true, severity: 'error', message: '', params: { max: 32 }
      }] }
    }]
  })
  await page.goto('/elements/41')
  const rules = page.locator('.el-card').filter({ has: page.getByText('标准约束规则', { exact: true }) })
  await expect(rules.getByText('长度范围', { exact: true })).toBeVisible()
  await expect(rules.getByText('{"max":32}', { exact: true })).toBeVisible()
  await expect(rules.getByRole('checkbox')).toHaveCount(0)
})

test('exact historical element revision survives reload and history selection updates the URL', async ({ page }) => {
  await installMockBackend(page, {
    elements: [{ id: 41, code: 'person_id', name: '当前人员标识', data_type: 'string', status: 'approved' }],
    elementHistory: [{ id: 410, element_id: 41, revision_no: 1, name: '冻结的人员标识', data_type: 'string', status: 'withdrawn', definition: '审批时定义' }]
  })
  await page.goto('/elements/41?revision_id=410')
  await expect(page.getByRole('heading', { name: '冻结的人员标识', exact: true })).toBeVisible()
  await expect(page.getByRole('textbox', { name: '中文名称', exact: true })).toBeDisabled()
  await page.reload()
  await expect(page.getByRole('heading', { name: '冻结的人员标识', exact: true })).toBeVisible()
  await page.getByText('R1 · 当前人员标识', { exact: true }).click()
  await expect(page).toHaveURL(/revision_id=411$/)
  await expect(page.getByRole('heading', { name: '当前人员标识', exact: true })).toBeVisible()
  await page.getByText('R1 · 冻结的人员标识', { exact: true }).click()
  await expect(page).toHaveURL(/revision_id=410$/)
  await expect(page.getByRole('heading', { name: '冻结的人员标识', exact: true })).toBeVisible()
})

test('creating a draft while viewing a frozen revision opens the new draft identity', async ({ page }) => {
  await installMockBackend(page, {
    elements: [{ id: 41, code: 'person_id', name: '当前人员标识', data_type: 'string', status: 'approved' }]
  })
  await page.goto('/elements/41?revision_id=411')
  await page.getByRole('button', { name: '创建新草稿', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('textbox').fill('修订数据元说明')
  await dialog.getByRole('button', { name: '确定', exact: true }).click()
  await expect(page).toHaveURL(/\/elements\/41\?revision_id=412$/)
  await expect(page.getByRole('textbox', { name: '中文名称', exact: true })).toBeEditable()
})

for (const destination of ['list', 'history']) {
  test(`unsaved element edits protect ${destination} navigation and keep the URL on cancel`, async ({ page }) => {
    await installMockBackend(page, {
      elements: [{ id: 41, code: 'person_id', name: '人员标识', data_type: 'string', status: 'draft' }],
      elementHistory: [{ id: 410, element_id: 41, revision_no: 1, name: '历史人员标识', data_type: 'string', status: 'published' }]
    })
    await page.goto('/elements/41?revision_id=411')
    const name = page.getByRole('textbox', { name: '中文名称', exact: true })
    await name.fill('尚未保存的名称')
    const navigate = () => destination === 'list'
      ? page.getByRole('button', { name: '返回', exact: true }).click()
      : page.getByText('R1 · 历史人员标识', { exact: true }).click()
    await navigate()
    const dialog = page.getByRole('dialog', { name: '有未保存的修改' })
    await dialog.getByRole('button', { name: '继续编辑' }).click()
    await expect(page).toHaveURL(/\/elements\/41\?revision_id=411$/)
    await expect(name).toHaveValue('尚未保存的名称')
    await navigate()
    await dialog.getByRole('button', { name: '放弃修改并离开' }).click()
    await expect(page).toHaveURL(destination === 'list' ? /\/elements$/ : /revision_id=410$/)
    await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
  })
}

for (const firstSection of ['identity', 'revision']) {
  test(`element saving ${firstSection} first preserves the other unsaved section`, async ({ page }) => {
    await installMockBackend(page, { elements: [{ id: 41, code: 'person_id', name: '人员标识', data_type: 'string', status: 'draft' }] })
    await page.goto('/elements/41')
    const name = page.getByRole('textbox', { name: '中文名称', exact: true })
    await name.fill('更新后的名称')
    const tags = page.getByRole('combobox', { name: '标签', exact: true })
    await tags.fill('新标签')
    await tags.press('Enter')
    const buttons = {
      revision: page.locator('.page-header').getByRole('button', { name: '保存', exact: true }),
      identity: page.locator('.el-card').filter({ has: page.getByText('治理信息', { exact: true }) }).getByRole('button', { name: '保存', exact: true })
    }
    await buttons[firstSection].click()
    await expect(name).toBeEnabled()
    await expect(name).toHaveValue('更新后的名称')
    await expect(page.getByText('新标签', { exact: true })).toBeVisible()
    await expect(page.getByText('未保存', { exact: true })).toBeVisible()
    await buttons[firstSection === 'identity' ? 'revision' : 'identity'].click()
    await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
    expect(await page.evaluate(() => window.dispatchEvent(new Event('beforeunload', { cancelable: true })))).toBe(true)
    await page.getByRole('button', { name: '返回', exact: true }).click()
    await expect(page).toHaveURL(/\/elements$/)
  })
}

test('failed element save retains unload protection and blocks revision actions until saved', async ({ page }) => {
  const backend = await installMockBackend(page, {
    elementVersionConflict: true,
    elements: [{ id: 41, code: 'person_id', name: '人员标识', data_type: 'string', status: 'draft' }]
  })
  await page.goto('/elements/41')
  const name = page.getByRole('textbox', { name: '中文名称', exact: true })
  await name.fill('本地尚未保存')
  await page.locator('.page-header').getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.getByText('资源已被其他用户修改，请刷新后重试')).toBeVisible()
  await expect(name).toHaveValue('本地尚未保存')
  expect(await page.evaluate(() => window.dispatchEvent(new Event('beforeunload', { cancelable: true })))).toBe(false)
  await page.getByRole('button', { name: '提交审核', exact: true }).click()
  await expect(page.getByText('请先保存当前修改，再执行状态操作')).toBeVisible()
  expect(backend.getActionRequests()).toEqual([])
  await page.getByRole('button', { name: '返回', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '有未保存的修改' })).toBeVisible()
})

test('a late element save response cannot overwrite a different selected revision', async ({ page }) => {
  let releaseSave
  const pendingSave = new Promise(resolve => { releaseSave = resolve })
  await installMockBackend(page, {
    pendingElementSave: pendingSave,
    elements: [{ id: 41, code: 'person_id', name: '人员标识', data_type: 'string', status: 'draft' }],
    elementHistory: [{ id: 410, element_id: 41, revision_no: 1, name: '历史人员标识', data_type: 'string', status: 'published' }]
  })
  await page.goto('/elements/41?revision_id=411')
  await page.getByRole('textbox', { name: '中文名称', exact: true }).fill('待保存的修订名称')
  const request = page.waitForRequest(req => req.method() === 'PUT')
  await page.locator('.page-header').getByRole('button', { name: '保存', exact: true }).click()
  await request
  await page.getByText('R1 · 历史人员标识', { exact: true }).click()
  await page.getByRole('dialog', { name: '有未保存的修改' }).getByRole('button', { name: '放弃修改并离开' }).click()
  await expect(page.getByRole('heading', { name: '历史人员标识', exact: true })).toBeVisible()
  const response = page.waitForResponse(res => res.request().method() === 'PUT')
  releaseSave()
  await response
  await expect(page.getByRole('textbox', { name: '中文名称', exact: true })).toHaveValue('历史人员标识')
  await expect(page).toHaveURL(/revision_id=410$/)
  await expect(page.getByText('未保存', { exact: true })).toHaveCount(0)
})

test('element iframe publishes only dirty state and clears it after saving', async ({ page }) => {
  await installMockBackend(page, { elements: [{ id: 41, code: 'person_id', name: '人员标识', data_type: 'string', status: 'draft' }] })
  await page.route('**/unsaved-host', route => route.fulfill({
    contentType: 'text/html',
    body: `<output id="state"></output><script type="module">
      import { createIframeAuthCoordinator } from ${JSON.stringify(`/@fs${new URL('../../../common-frontend/basic/src/auth/authSession.js', import.meta.url).pathname}`)}
      createIframeAuthCoordinator({
        allowedOrigins: [location.origin],
        getToken: () => 'standard-e2e-token',
        getExpiresAt: () => Date.now() + 3600000,
        refreshToken: async () => {}, logout: async () => {}
      })
      window.addEventListener('message', event => {
        if (event.data?.type === 'addp:unsaved-changes') document.querySelector('#state').textContent = JSON.stringify(event.data)
      })
    </script><iframe src="/elements/41" title="Standard editor" style="width:850px;height:1000px"></iframe>`
  }))
  await page.goto('/unsaved-host')
  const frame = page.frameLocator('iframe')
  await frame.getByRole('textbox', { name: '中文名称', exact: true }).fill('不得向父窗口发送的草稿')
  await expect(page.locator('#state')).toContainText('"dirty":true')
  const message = JSON.parse(await page.locator('#state').textContent())
  expect(Object.keys(message).sort()).toEqual(['active', 'dirty', 'id', 'type'])
  await frame.locator('.page-header').getByRole('button', { name: '保存', exact: true }).click()
  await expect(page.locator('#state')).toContainText('"dirty":false')
})

for (const suffix of ['', '?revision_id=411']) {
  test(`enum binding displays its withdrawn snapshot without code set permission: ${suffix || 'default'}`, async ({ page }) => {
    const codeSetRequests = []
    page.on('request', request => {
      if (request.url().includes('/standard/code-sets')) codeSetRequests.push(request.url())
    })
    await installMockBackend(page, {
      permissions: ['standard.element.read'],
      elements: [{ id: 41, code: 'gender', name: '历史性别', data_type: 'string', status: 'approved', value_domain_kind: 'enumeration', code_set_revision_id: 310 }],
      elementCodeSetSnapshots: { 310: { revision_id: '310', code_set_id: '31', name: '历史性别码表', code: 'gender', revision_no: 1, status: 'withdrawn' } }
    })
    await page.goto(`/elements/41${suffix}`)
    const binding = page.locator('.el-form-item').filter({ has: page.getByText('关联码值集', { exact: true }) })
    await expect(binding).toContainText('历史性别码表 (gender) · R1')
    await expect(binding).toContainText('已撤回')
    await expect(binding.getByRole('combobox')).toHaveCount(0)
    await page.reload()
    await expect(binding).toContainText('历史性别码表 (gender) · R1')
    expect(codeSetRequests).toEqual([])
  })
}

test('draft preserves an unavailable binding label but only offers published candidates for a new selection', async ({ page }) => {
  await installMockBackend(page, {
    elements: [{ id: 41, code: 'gender', name: '性别', data_type: 'string', status: 'draft', value_domain_kind: 'enumeration', code_set_revision_id: 310 }],
    elementCodeSetSnapshots: { 310: { revision_id: '310', code_set_id: '31', name: '历史性别码表', code: 'gender', revision_no: 1, status: 'withdrawn' } }
  })
  await page.goto('/elements/41')
  const binding = page.locator('.el-form-item').filter({ has: page.getByText('关联码值集', { exact: true }) })
  await expect(binding).toContainText('历史性别码表 (gender) · R1')
  await binding.locator('.el-select__wrapper').click()
  await expect(page.getByRole('option', { name: '历史性别码表 (gender) · R1', exact: true })).toBeDisabled()
  await page.getByRole('option', { name: '性别 (gender) · R1', exact: true }).click()
  await expect(binding).not.toContainText('历史性别码表')
  await expect(binding).not.toContainText('已撤回')
})

test('switching enum history renders each bound version, including English status labels', async ({ page }) => {
  await installMockBackend(page, {
    language: 'en',
    elements: [{ id: 41, code: 'gender', name: 'Current gender', data_type: 'string', status: 'approved', value_domain_kind: 'enumeration', code_set_revision_id: 311 }],
    elementHistory: [{ id: 410, element_id: 41, revision_no: 1, name: 'Historical gender', data_type: 'string', status: 'withdrawn', value_domain_kind: 'enumeration', code_set_revision_id: 310 }],
    elementCodeSetSnapshots: {
      310: { revision_id: '310', code_set_id: '31', name: 'Historical codes', code: 'gender', revision_no: 1, status: 'withdrawn' },
      311: { revision_id: '311', code_set_id: '31', name: 'Current codes', code: 'gender', revision_no: 2, status: 'published' }
    }
  })
  await page.goto('/elements/41?revision_id=410')
  const binding = page.locator('.el-form-item').filter({ has: page.getByText('Code Set', { exact: true }) })
  await expect(binding).toContainText('Historical codes (gender) · R1')
  await expect(binding).toContainText('Withdrawn')
  await page.getByText('R1 · Current gender', { exact: true }).click()
  await expect(binding).toContainText('Current codes (gender) · R2')
  await expect(binding).not.toContainText('Historical codes')
  await page.getByText('R1 · Historical gender', { exact: true }).click()
  await expect(binding).toContainText('Historical codes (gender) · R1')
  await expect(binding).not.toContainText('Current codes')
})

for (const snapshot of [null, { revision_id: '999', name: '错误码值集' }]) {
  test(`enum revision rejects a missing or mismatched bound snapshot: ${snapshot?.revision_id || 'missing'}`, async ({ page }) => {
    await installMockBackend(page, {
      elements: [{ id: 41, code: 'gender', name: '性别', data_type: 'string', status: 'approved', value_domain_kind: 'enumeration', code_set_revision_id: 310 }],
      elementCodeSetSnapshots: { 310: snapshot }
    })
    await page.goto('/elements/41')
    await expect(page.getByRole('alert')).toContainText('无法读取该修订绑定的码值集版本')
    await expect(page.getByText('错误码值集', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '创建新草稿', exact: true })).toHaveCount(0)
  })
}

for (const [revision, forbidden, error] of [['999', false, '数据元修订不存在'], ['410', true, '无权读取此数据元修订'], ['invalid', false, '加载失败']]) {
  test(`exact element revision fails closed: ${revision}, forbidden=${forbidden}`, async ({ page }) => {
    await installMockBackend(page, {
      elements: [{ id: 41, code: 'person_id', name: '不能替代历史的当前名称', data_type: 'string', status: 'approved' }],
      forbidElementRevision: forbidden
    })
    await page.goto(`/elements/41?revision_id=${revision}`)
    await expect(page.getByRole('alert')).toContainText(error)
    await expect(page.getByRole('heading', { name: '不能替代历史的当前名称' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '新建修订', exact: true })).toHaveCount(0)
  })
}

test('submits and publishes a draft data element revision', async ({ page }) => {
  const backend = await installMockBackend(page, {
    elements: [{
      id: 41,
      name: '活动编号',
      code: 'activity_id',
      data_type: 'string',
      domain_id: 2,
      status: 'draft',
      quality_rules: null
    }]
  })
  await page.goto('/elements/41')

  await expect(page.getByText('R1 · 草稿', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '提交审核' }).click()
  await page.getByRole('dialog', { name: '提示' }).getByRole('button', { name: '确定' }).click()
  await expect(page.getByText('R1 · 审核中', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: '发布' }).click()
  await page.getByRole('dialog', { name: '提示' }).getByRole('button', { name: '确定' }).click()
  await expect(page.getByText('R1 · 已发布', { exact: true })).toBeVisible()
  expect(backend.getActionRequests()).toEqual([
    '/api/v1/standard/elements/41/revisions/411/submit',
    '/api/v1/standard/elements/41/revisions/411/publish'
  ])
})

test('submits a data element only once when confirmation fires twice', async ({ page }) => {
  const backend = await installMockBackend(page, {
    delayedElementApprove: true,
    elements: [{
      id: 41,
      name: '审批防重数据元',
      code: 'approve_once',
      data_type: 'string',
      domain_id: 2,
      status: 'draft',
      quality_rules: null
    }]
  })
  await page.goto('/elements/41')

  await page.getByRole('button', { name: '提交审核' }).click()
  const confirmButton = page.getByRole('dialog', { name: '提示' }).getByRole('button', { name: '确定' })
  await confirmButton.evaluate(button => {
    button.click()
    button.click()
  })

  await expect(page.getByText('R1 · 审核中', { exact: true })).toBeVisible()
  expect(backend.getActionRequests().filter(path => path === '/api/v1/standard/elements/41/revisions/411/submit')).toHaveLength(1)
})

test('moves a metric revision through submit, publish and withdraw states', async ({ page }) => {
  const backend = await installMockBackend(page, {
    elements: [{
      id: 41,
      name: '活动参与人数数据元',
      code: 'participant_count_element',
      data_type: 'int',
      domain_id: 2,
      status: 'approved'
    }],
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'draft',
      tags: [],
      element_ids: [41],
      created_at: '2026-08-12T08:00:00Z'
    }]
  })
  await page.goto('/metrics/51')

  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 草稿' })).toBeVisible()
  await page.getByRole('button', { name: '提交审核' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 审核中' })).toBeVisible()

  await page.getByRole('button', { name: '发布' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 已发布' })).toBeVisible()

  await page.getByRole('button', { name: '撤回' }).click()
  await expect(page.locator('.page-header .el-tag').filter({ hasText: 'R1 · 已撤回' })).toBeVisible()

  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/metrics/51/revisions/511/submit',
    '/api/v1/standard/metrics/51/revisions/511/publish',
    '/api/v1/standard/metrics/51/revisions/511/withdraw'
  ]))
})

test('shows the backend domain conflict message after confirmed deletion', async ({ page }) => {
  const backend = await installMockBackend(page, { domainDeleteConflict: true })
  await page.goto('/domains')

  const outdoorRow = page.locator('.tree-node').filter({ hasText: '户外域' })
  await outdoorRow.getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除业务域「户外域」？')
  await confirm.getByRole('button', { name: '确定' }).click()

  await expect(page.locator('.el-message--error')).toContainText(
    '业务域仍被子业务域、业务术语、数据元或指标引用，无法删除'
  )
  await expect(outdoorRow).toBeVisible()
  expect(backend.getDeleteRequests()).toEqual(['/api/v1/standard/domains/2'])
  expect(backend.getDomainDeleteRequests()).toEqual([{ id: 2, version: 1 }])
})

test('cancels document deletion without sending a request', async ({ page }) => {
  const backend = await installMockBackend(page, {
    documents: [createDocumentFixture()]
  })
  await page.goto('/documents')

  const row = page.getByRole('row').filter({ hasText: '户外数据标准' })
  await row.getByRole('button', { name: '删除' }).click()
  const confirm = page.getByRole('dialog', { name: '提示' })
  await expect(confirm).toContainText('确认删除文档"户外数据标准"？')
  await confirm.getByRole('button', { name: '取消' }).click()
  await expect(confirm).not.toBeVisible()
  await expect(row).toBeVisible()
  expect(backend.getDeleteRequests()).toEqual([])
})

test('restores document detail from its canonical route and returns to the filtered list', async ({ page }) => {
  await installMockBackend(page, {
    documents: [createDocumentFixture()]
  })

  await page.goto('/documents?keyword=户外')
  await page.getByRole('row').filter({ hasText: '户外数据标准' }).getByRole('button', { name: '详情' }).click()
  await expect(page).toHaveURL(/\/documents\/71\?keyword=/)

  await page.reload()
  await expect(page.getByRole('heading', { name: '户外数据标准' })).toBeVisible()
  await expect(page.getByText(/outdoor-standard\.md · 2\.0 KB · SHA256/)).toBeVisible()
  await expect(page).toHaveURL(/\/documents\/71\?keyword=/)

  await page.getByRole('button', { name: /返回/ }).click()
  await expect(page).toHaveURL(/\/documents\?keyword=/)
})

test('shows deterministic candidate comparisons and opens the existing standard', async ({ page }) => {
  const documentCandidateFamilies = createCandidateFamilyResponse([
        { id: 811, candidate_type: 'glossary', code: 'leader', name: '领队', definition: '发起并组织户外活动的人', payload: {}, status: 'pending', version: 1, evidences: [], comparison: { result: 'exact', standard_id: 21, code: 'leader', name: '领队', scope_type: 'domain', owner_domain_id: 2, revision_id: 211, revision_no: 1, revision_status: 'draft', differences: [] } },
        { id: 812, candidate_type: 'element', code: 'activity_id', name: '活动编号', definition: '活动的唯一编号', payload: { data_type: 'string' }, status: 'pending', version: 1, evidences: [], comparison: { result: 'content_conflict', standard_id: 41, code: 'activity_id', name: '活动编号', scope_type: 'domain', owner_domain_id: 2, revision_id: 411, revision_no: 1, revision_status: 'draft', differences: [{ field: 'definition', candidate_value: { kind: 'text', text: '活动的唯一编号' }, standard_value: { kind: 'text', text: '户外活动主体的稳定标识' } }, { field: 'data_type', candidate_value: { kind: 'text', text: 'string' }, standard_value: { kind: 'text', text: 'bigint' } }] } },
        { id: 813, candidate_type: 'metric', code: 'participant_count', name: '活动参与人数', definition: '参加活动的总人数', payload: {}, status: 'pending', version: 1, evidences: [], comparison: { result: 'scope_conflict', standard_id: 51, code: 'participant_count', name: '活动参与人数', scope_type: 'domain', owner_domain_id: 1, revision_id: 511, revision_no: 1, revision_status: 'draft', differences: [{ field: 'owner_domain_id', candidate_value: { kind: 'integer', integer: 2 }, standard_value: { kind: 'integer', integer: 1 } }] } },
        { id: 814, candidate_type: 'code_set', code: 'outdoor_level', name: '户外等级', definition: '户外活动难度等级', payload: {}, status: 'pending', version: 1, evidences: [], comparison: { result: 'new', differences: [] } },
        { id: 815, candidate_type: 'code_set', code: 'member_status', name: '成员状态', definition: '成员参与状态', payload: {}, status: 'pending', version: 1, evidences: [], comparison: { result: 'content_conflict', standard_id: 61, code: 'member_status', name: '成员状态', scope_type: 'domain', owner_domain_id: 2, revision_id: 611, revision_no: 1, revision_status: 'draft', differences: [{ field: 'items', candidate_value: { kind: 'code_items', items: [{ code: 'signup', name: '报名中', definition: '已正式报名' }] }, standard_value: { kind: 'code_items', items: [{ code: 'registered', name: '已报名', definition: '报名已经确认' }] } }] } },
        { id: 816, candidate_type: 'code_set', code: 'member_status', name: '成员关系状态', definition: '成员与户外活动的关系状态', payload: {}, status: 'pending', version: 1, evidences: [], comparison: { result: 'content_conflict', standard_id: 61, code: 'member_status', name: '成员状态', scope_type: 'domain', owner_domain_id: 2, revision_id: 611, revision_no: 1, revision_status: 'draft', differences: [{ field: 'definition', candidate_value: { kind: 'text', text: '成员与户外活动的关系状态' }, standard_value: { kind: 'text', text: '成员参与状态' } }] } }
  ])
  const memberStatusSnapshotToken = documentCandidateFamilies.data.find(family => family.code === 'member_status').snapshot_token
  const backend = await installMockBackend(page, {
    documents: [createDocumentFixture()],
    documentCandidateFamilies
  })

  await page.goto('/documents/71')
  const comparisonFacets = page.getByRole('radiogroup', { name: '比对结果' })
  await expect(comparisonFacets.getByRole('radio', { name: '全部比对结果 · 5' })).toBeChecked()
  await expect(comparisonFacets.getByRole('radio', { name: '内容一致 · 1' })).toBeVisible()
  await expect(comparisonFacets.getByRole('radio', { name: '内容冲突 · 2' })).toBeVisible()
  await expect(comparisonFacets.getByRole('radio', { name: '范围冲突 · 1' })).toBeVisible()
  await expect(comparisonFacets.getByRole('radio', { name: '新候选 · 1' })).toBeVisible()
  const comparisonCountHelp = page.getByRole('button', { name: '比对结果计数说明' })
  await comparisonCountHelp.hover()
  await expect(page.getByRole('tooltip')).toContainText('各分类数量之和不一定等于候选族总数')
  await page.locator('.candidate-family').filter({ hasText: 'activity_id' }).locator('.el-collapse-item__header').first().click()
  const activityCandidate = page.locator('.candidate-card').filter({ hasText: 'activity_id' })
  await expect(activityCandidate.getByRole('columnheader', { name: '差异字段' })).toBeVisible()
  await expect(activityCandidate.getByRole('columnheader', { name: '候选值' })).toBeVisible()
  await expect(activityCandidate.getByRole('columnheader', { name: '当前标准值' })).toBeVisible()
  await expect(activityCandidate.locator('.comparison-differences').getByText('活动的唯一编号', { exact: true })).toBeVisible()
  await expect(activityCandidate.getByText('户外活动主体的稳定标识', { exact: true })).toBeVisible()
  await expect(activityCandidate.getByText('bigint', { exact: true })).toBeVisible()
  await page.locator('.candidate-family').filter({ hasText: 'member_status' }).locator('.el-collapse-item__header').first().click()
  const codeSetCandidate = page.locator('.candidate-card').filter({ hasText: 'member_status' })
  const variantDifferences = page.getByRole('region', { name: '语义变体差异摘要' })
  await expect(variantDifferences).toContainText('以下 2 个字段在本族变体之间存在差异')
  await expect(variantDifferences).toContainText('名称')
  await expect(variantDifferences).toContainText('成员状态')
  await expect(variantDifferences).toContainText('成员关系状态')
  await expect(variantDifferences.getByText('变体 1')).toHaveCount(2)
  await expect(variantDifferences.getByText('变体 2')).toHaveCount(2)
  await variantDifferences.getByRole('button', { name: '定位到变体 2' }).first().click()
  await expect(codeSetCandidate.nth(1)).toBeFocused()
  const codeItems = codeSetCandidate.locator('.comparison-item')
  await expect(codeItems.nth(0)).toContainText('signup · 报名中 — 已正式报名')
  await expect(codeItems.nth(1)).toContainText('registered · 已报名 — 报名已经确认')

  const candidateSearch = page.getByRole('textbox', { name: '搜索候选编码或名称' })
  await candidateSearch.fill('成员关系状态')
  await candidateSearch.press('Enter')
  await expect(page.getByText('显示 1/2 个语义变体')).toBeVisible()
  await expect(page.getByText('当前仅显示本族 1/2 个语义变体；请清除筛选后再执行胜出裁决。')).toBeVisible()
  await expect(page.getByRole('button', { name: '设为族内胜出变体' })).toHaveCount(0)
  await candidateSearch.fill('')
  await candidateSearch.press('Enter')
  await expect(page.getByText('共 5 个候选族，6 个语义变体')).toBeVisible()
  const memberStatusFamily = page.locator('.candidate-family').filter({ hasText: 'member_status' })
  const memberStatusCards = memberStatusFamily.locator('.candidate-card')
  await expect(memberStatusFamily.getByRole('button', { name: '保留候选' })).toHaveCount(0)
  await expect(memberStatusFamily.getByRole('button', { name: '驳回候选' })).toHaveCount(0)
  await expect(memberStatusFamily.getByRole('button', { name: '设为族内胜出变体' })).toHaveCount(2)
  await memberStatusCards.nth(1).getByRole('button', { name: '设为族内胜出变体' }).click()
  await page.getByPlaceholder('请说明选择该变体的业务依据、证据或取舍').fill('定义覆盖成员与户外活动的完整关系')
  await page.getByRole('button', { name: '确认裁决' }).click()
  await expect.poll(() => backend.getCandidateDecisionRequests()).toEqual([{
    winner_candidate_id: 816,
    snapshot_token: memberStatusSnapshotToken,
    reason: '定义覆盖成员与户外活动的完整关系'
  }])
  await expect(memberStatusCards.nth(0)).toContainText('已驳回')
  await expect(memberStatusCards.nth(1)).toContainText('已保留')
  await expect(page.getByText('候选族裁决完成')).toBeVisible()
  await memberStatusFamily.getByRole('button', { name: '裁决历史（1）', exact: true }).click()
  const decisionHistory = page.getByRole('dialog', { name: '候选族裁决历史 · member_status' })
  await expect(decisionHistory).toContainText('成员关系状态')
  await expect(decisionHistory).toContainText('定义覆盖成员与户外活动的完整关系')
  await expect(decisionHistory).toContainText('#816')
  await decisionHistory.getByRole('button', { name: '关闭', exact: true }).click()

  await candidateSearch.fill('MEMBER_STATUS')
  await candidateSearch.press('Enter')
  await expect(page.getByText('共 1 个候选族，2 个语义变体')).toBeVisible()
  await expect(comparisonFacets.getByRole('radio', { name: '全部比对结果 · 1' })).toBeChecked()
  await expect(comparisonFacets.getByRole('radio', { name: '内容冲突 · 1' })).toBeVisible()
  await expect(page.locator('.candidate-family')).toContainText('2 个语义变体')
  await expect(page.locator('.candidate-card')).toHaveCount(2)
  await expect(page.locator('.candidate-card').first()).toContainText('member_status')
  await candidateSearch.fill('')
  await candidateSearch.press('Enter')
  await expect(page.getByText('共 5 个候选族，6 个语义变体')).toBeVisible()

  await comparisonFacets.getByText('内容一致 · 1', { exact: true }).click()
  await expect(page.getByText('共 1 个候选族，1 个语义变体')).toBeVisible()
  await expect(page.locator('.candidate-card')).toHaveCount(1)
  await expect(page.locator('.candidate-card')).toContainText('leader')
  const leaderFamily = page.locator('.candidate-family').filter({ hasText: 'leader' })
  await expect(leaderFamily.getByRole('button', { name: '保留候选' })).toHaveCount(1)
  await expect(leaderFamily.getByRole('button', { name: '驳回候选' })).toHaveCount(1)
  await expect(leaderFamily.getByRole('button', { name: '设为族内胜出变体' })).toHaveCount(0)

  await page.getByRole('button', { name: '查看现有标准' }).first().click()
  await expect(page).toHaveURL(/\/glossaries\/21$/)
  await expect(page.getByRole('heading', { name: '领队' })).toBeVisible()
})

test('keeps the created document identity when an initial file upload returns file metadata', async ({ page }) => {
  const backend = await installMockBackend(page, { documents: [] })
  await page.goto('/documents')

  await page.getByRole('button', { name: '录入文档' }).click()
  const dialog = page.getByRole('dialog', { name: '录入标准文档' })
  await dialog.getByRole('textbox', { name: '编码' }).fill('outdoor_governance_plan')
  await dialog.getByRole('textbox', { name: '文档名称' }).fill('Outdoor 治理方案')
  await expect(dialog.getByRole('textbox', { name: '变更说明' })).toHaveCount(0)
  await dialog.locator('input[type="file"]').setInputFiles({
    name: 'outdoor-governance.md',
    mimeType: 'text/markdown',
    buffer: Buffer.from('# Outdoor 治理方案')
  })
  await dialog.getByRole('button', { name: '确定' }).click()

  await expect(page).toHaveURL(/\/documents\/72$/)
  await expect(page.getByRole('heading', { name: 'Outdoor 治理方案' })).toBeVisible()
  await expect(page.getByText(/outdoor-governance\.md/)).toBeVisible()
  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/documents',
    '/api/v1/standard/documents/72/revisions/721/file'
  ]))
})

test('shows the backend upload error when attaching a file to a standard item', async ({ page }) => {
  const backend = await installMockBackend(page, {
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'approved',
      tags: [],
      version: 1,
      created_at: '2026-08-12T08:00:00Z'
    }],
    documents: [],
    uploadError: '文档文件超过 100 MiB 限制'
  })
  await page.goto('/metrics/51')

  await page.getByRole('button', { name: '上传新文档' }).click()
  const uploadDialog = page.getByRole('dialog', { name: '上传并关联文档' })
  await uploadDialog.getByRole('textbox', { name: '编码' }).fill('participant_standard')
  await uploadDialog.getByRole('textbox', { name: '文档名称' }).fill('参与人数标准')
  await expect(uploadDialog.getByRole('textbox', { name: '变更说明' })).toHaveCount(0)
  await uploadDialog.locator('input[type="file"]').setInputFiles({
    name: 'participant-standard.pdf',
    mimeType: 'application/pdf',
    buffer: Buffer.from('fixture')
  })
  await uploadDialog.getByRole('button', { name: '上传并关联' }).click()

  await expect(page.locator('.el-message--error')).toContainText('文档文件超过 100 MiB 限制')
  expect(backend.getActionRequests()).toEqual(expect.arrayContaining([
    '/api/v1/standard/metrics/51/documents',
    '/api/v1/standard/documents/72/revisions/721/file'
  ]))
  await expect(uploadDialog).toBeVisible()
})

test('links an existing document to a metric and refreshes the panel', async ({ page }) => {
  const backend = await installMockBackend(page, {
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'approved',
      tags: [],
      version: 1,
      created_at: '2026-08-12T08:00:00Z'
    }],
    documents: [createDocumentFixture()]
  })
  await page.goto('/metrics/51')

  await page.getByRole('button', { name: '关联已有文档' }).click()
  const linkDialog = page.getByRole('dialog', { name: '关联已有文档' })
  await linkDialog.getByRole('combobox').click({ force: true })
  await page.getByRole('option', { name: /户外数据标准/ }).click()
  await page.keyboard.press('Escape')
  const confirmLink = linkDialog.getByRole('button', { name: '确认关联' })
  await expect(confirmLink).toBeEnabled()
  await confirmLink.click()

  await expect.poll(() => backend.getActionRequests()).toContain('/api/v1/standard/metrics/51/documents/link')
  await expect(page.getByRole('cell', { name: '户外数据标准', exact: true })).toBeVisible()
  await expect(linkDialog).not.toBeVisible()
})

test('presents Standard pages as read-only when the role only has read permissions', async ({ page }) => {
  await installMockBackend(page, {
    permissions: readOnlyStandardPermissions,
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'draft',
      tags: []
    }],
    elements: [{
      id: 41,
      name: '活动编号',
      code: 'activity_id',
      data_type: 'string',
      status: 'draft',
      quality_rules: null
    }]
  })

  for (const path of ['/domains', '/glossaries', '/elements', '/code-sets', '/units', '/metrics', '/documents']) {
    await page.goto(path)
    await expect(page.getByRole('button', { name: /新建|新增|添加分类|录入文档/ })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /审批|废弃|删除/ })).toHaveCount(0)
  }

  await page.goto('/glossaries/21')
  await expect(page.getByRole('textbox', { name: '术语名称' })).toBeDisabled()
  await expect(page.getByRole('button', { name: '保存', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /添加数据元|上传新文档|关联已有文档|解除关联/ })).toHaveCount(0)

  await page.goto('/metrics/51')
  await expect(page.getByRole('textbox', { name: '指标名称' })).toBeDisabled()
  await expect(page.getByRole('button', { name: /保存|审批|上传新文档|关联已有文档/ })).toHaveCount(0)

  await page.goto('/elements/41')
  await expect(page.getByRole('textbox', { name: '名称' }).first()).toBeDisabled()
  await expect(page.getByRole('button', { name: /保存|审批|添加规则|上传新文档|关联已有文档/ })).toHaveCount(0)
})

test('matches document panel controls to the backend permission combinations', async ({ page }) => {
  await installMockBackend(page, {
    permissions: [
      'standard.metric.read',
      'standard.metric.update',
      'standard.document.read',
      'standard.document.create'
    ],
    metrics: [{
      id: 51,
      name: '活动参与人数',
      code: 'participant_count',
      type: 'atomic',
      definition: '参加活动的总人数',
      status: 'approved',
      tags: []
    }]
  })
  await page.goto('/metrics/51')

  await expect(page.getByRole('button', { name: '上传新文档' })).toBeVisible()
  await expect(page.getByRole('button', { name: '关联已有文档' })).toHaveCount(0)
  await page.getByRole('button', { name: '上传新文档' }).click()
  const uploadDialog = page.getByRole('dialog', { name: '上传并关联文档' })
  await expect(uploadDialog.getByRole('button', { name: '选择文件' })).toHaveCount(0)
  await expect(uploadDialog.getByRole('button', { name: '仅录入元数据' })).toBeVisible()
})

test('updates Standard controls immediately when the authorization context changes', async ({ page }) => {
  await installMockBackend(page, {
    permissions: readOnlyStandardPermissions,
    authContextPermissionsByToken: {
      'standard-e2e-token': readOnlyStandardPermissions,
      'standard-e2e-full-token': allStandardPermissions
    }
  })
  await page.goto('/glossaries')
  await expect(page.getByRole('button', { name: '新建术语' })).toHaveCount(0)

  await page.evaluate(() => {
    const channel = new BroadcastChannel('addp-auth-session')
    channel.postMessage({
      type: 'token',
      token: 'standard-e2e-full-token',
      expiresAt: Date.now() + 900_000
    })
    channel.close()
  })
  await expect(page.getByRole('button', { name: '新建术语' })).toBeVisible()

  await page.evaluate(() => {
    const channel = new BroadcastChannel('addp-auth-session')
    channel.postMessage({
      type: 'token',
      token: 'standard-e2e-token',
      expiresAt: Date.now() + 900_000
    })
    channel.close()
  })
  await expect(page.getByRole('button', { name: '新建术语' })).toHaveCount(0)
})

async function installMockBackend(page, options = {}) {
  const deleteRequests = []
  const actionRequests = []
  let metricDocumentListRequests = 0
  let glossaryListRaceRequests = 0
  let glossaryCreateRequests = 0
  const domainUpdateRequests = []
  const domainDeleteRequests = []
  const candidateDecisionRequests = []
  const candidateFamilyDecisions = []
  let metricDocumentLinked = false
  const documents = (options.documents || []).map(item => ({ ...item }))
  const elements = (options.elements || []).map(item => ({ ...item }))
  const glossaryFixtures = structuredClone(glossaries)
  let glossaryElementIDs = [...(options.glossaryElementIDs || [])]
  const documentCandidateFamilyResponse = structuredClone(options.documentCandidateFamilies || createCandidateFamilyResponse([]))
  if (options.glossaryPublicationHistory) {
    const published = { ...glossaryFixtures[0].draft_revision, status: 'published' }
    Object.assign(glossaryFixtures[0], {
      draft_revision_id: null,
      draft_revision: null,
      current_revision: published,
      has_publication_history: true
    })
  }
  const glossaryHistory = options.glossaryHistory ? [{ ...glossaryFixtures[0].draft_revision, id: 210, name: '历史领队', status: 'published' }] : []
  if (options.glossaryHistory) glossaryFixtures[0].draft_revision.revision_no = 2
  const metrics = (options.metrics || []).map(item => {
    if (item.current_revision || item.draft_revision) return structuredClone(item)
    const revision = {
      id: item.id * 10 + 1,
      metric_definition_id: item.id,
      revision_no: 1,
      name: item.name,
      definition: item.definition || `${item.name}定义`,
      statistical_caliber: item.statistical_caliber || item.definition || `${item.name}口径`,
      semantic_formula: item.formula || '',
      metric_type: item.type || 'atomic',
      unit_id: null,
      dependencies: [],
      change_summary: '初始修订',
      effective_from: '2026-08-12T08:00:00Z',
      status: item.status === 'approved' ? 'published' : item.status === 'deprecated' ? 'withdrawn' : item.status,
      created_at: item.created_at || '2026-08-12T08:00:00Z'
    }
    const hasDraft = revision.status === 'draft' || revision.status === 'in_review'
    return {
      id: item.id,
      code: item.code,
      scope_type: item.domain_id ? 'domain' : 'tenant_common',
      owner_domain_id: item.domain_id || null,
      category_id: item.category_id || null,
      tags: item.tags || [],
      lifecycle_state: 'active',
      version: item.version || 1,
      draft_revision_id: hasDraft ? revision.id : null,
      draft_revision: hasDraft ? revision : null,
      current_revision: hasDraft ? null : revision
    }
  })
  const elementAggregates = elements.map(item => {
    const revision = {
      id: item.id * 10 + 1,
      element_id: item.id,
      revision_no: 1,
      name: item.name,
      definition: item.definition || `${item.name}定义`,
      data_type: item.data_type,
      unit_id: item.unit_id ?? null,
      nullable: true,
      value_domain_kind: item.value_domain_kind || 'unrestricted',
      code_set_revision_id: item.code_set_revision_id ?? null,
      example_values: [],
      compiled_quality_rules: item.compiled_quality_rules || null,
      change_summary: '初始修订',
      status: item.status === 'approved' ? 'published' : item.status,
      created_at: '2026-08-12T08:00:00Z'
    }
    const hasDraft = revision.status === 'draft' || revision.status === 'in_review'
    return {
      id: item.id,
      code: item.code,
      scope_type: item.domain_id ? 'domain' : 'tenant_common',
      owner_domain_id: item.domain_id || null,
      tags: [],
      lifecycle_state: 'active',
      version: 1,
      draft_revision_id: hasDraft ? revision.id : null,
      draft_revision: hasDraft ? revision : null,
      current_revision: hasDraft ? null : revision
    }
  })
  const codeSetRevision = {
    id: 311,
    code_set_id: 31,
    revision_no: 1,
    name: '性别',
    description: '性别码值',
    value_type: 'string',
    change_summary: '初始修订',
    status: 'published',
    items: [],
    created_at: '2026-08-12T08:00:00Z'
  }
  const codeSetAggregate = {
    id: 31,
    code: 'gender',
    scope_type: 'domain',
    owner_domain_id: 2,
    origin: 'tenant',
    tags: [],
    lifecycle_state: 'active',
    version: 1,
    current_revision: codeSetRevision,
    draft_revision: null
  }
  const permissions = options.permissions ?? allStandardPermissions
  const authContextPermissionsByToken = options.authContextPermissionsByToken || {}
  await page.addInitScript(({ theme, language }) => {
    localStorage.setItem('addp-lang', language || 'zh-cn')
    localStorage.setItem('theme-mode', theme || 'light')
  }, { theme: options.theme, language: options.language })
  await page.route('**/api/v1/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname

    if (request.method() === 'DELETE' && path === '/api/v1/standard/domains/2' && options.domainDeleteConflict) {
      deleteRequests.push(path)
      domainDeleteRequests.push({ id: 2, version: request.postDataJSON().version })
      return fulfillJSON(route, {
        error: '业务域仍被子业务域、业务术语、数据元或指标引用，无法删除'
      }, 409)
    }
    if (request.method() === 'PUT' && path === '/api/v1/standard/domains/2') {
      const body = request.postDataJSON()
      domainUpdateRequests.push({ id: 2, version: body.version, name: body.name })
      if (options.domainVersionConflict) {
        return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
      }
      return fulfillJSON(route, { ...domains[0], ...body, version: domains[0].version + 1 })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/glossaries' && options.delayedGlossaryCreate) {
      glossaryCreateRequests += 1
      await new Promise(resolve => setTimeout(resolve, 150))
      return fulfillJSON(route, { id: 24, code: request.postDataJSON().code, version: 1, draft_revision: { ...request.postDataJSON(), id: 241, status: 'draft', revision_no: 1 } }, 201)
    }
    if (request.method() === 'PUT' && path === '/api/v1/standard/glossaries/21' && options.glossaryVersionConflict) {
      return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
    }
    if (request.method() === 'DELETE') {
      deleteRequests.push(path)
      return fulfillJSON(route, {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/elements/41/revisions/411/submit') {
      actionRequests.push(path)
      if (options.delayedElementApprove) await new Promise(resolve => setTimeout(resolve, 150))
      const element = elementAggregates.find(item => item.id === 41)
      if (element?.draft_revision) {
        element.draft_revision.status = 'in_review'
        element.version += 1
      }
      return fulfillJSON(route, element || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/elements/41/revisions/411/publish') {
      actionRequests.push(path)
      const element = elementAggregates.find(item => item.id === 41)
      if (element?.draft_revision) {
        element.draft_revision.status = 'published'
        element.current_revision = element.draft_revision
        element.draft_revision = null
        element.draft_revision_id = null
        element.version += 1
      }
      return fulfillJSON(route, element || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/revisions/511/submit') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric?.draft_revision) {
        metric.draft_revision.status = 'in_review'
        metric.version += 1
      }
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/revisions/511/publish') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric?.draft_revision) {
        metric.draft_revision.status = 'published'
        metric.current_revision = metric.draft_revision
        metric.draft_revision = null
        metric.draft_revision_id = null
        metric.version += 1
      }
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/revisions/511/withdraw') {
      actionRequests.push(path)
      const metric = metrics.find(item => item.id === 51)
      if (metric?.current_revision) {
        metric.current_revision.status = 'withdrawn'
        metric.version += 1
      }
      return fulfillJSON(route, metric || {})
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/documents') {
      actionRequests.push(path)
      const body = request.postDataJSON()
      expect(body).not.toHaveProperty('change_summary')
      const document = createDocumentFixture({
        id: 72,
        code: body.code,
        scope_type: body.scope_type,
        owner_domain_id: body.owner_domain_id,
        doc_type: body.doc_type,
        source_org: body.source_org,
        revision: {
          name: body.name,
          version_label: body.version_label,
          description: body.description,
          file_name: null,
          file_size: 0,
          media_type: '',
          content_sha256: '',
          change_summary: '初始创建'
        }
      })
      documents.push(document)
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.version = (metric.version || 1) + 1
      return fulfillJSON(route, { document, version: metric?.version || 2 })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/documents') {
      actionRequests.push(path)
      const body = request.postDataJSON()
      expect(body).not.toHaveProperty('change_summary')
      const document = createDocumentFixture({
        id: 72,
        code: body.code,
        scope_type: body.scope_type,
        owner_domain_id: body.owner_domain_id,
        doc_type: body.doc_type,
        source_org: body.source_org,
        revision: {
          name: body.name,
          version_label: body.version_label,
          description: body.description,
          file_name: null,
          file_size: 0,
          media_type: '',
          content_sha256: '',
          change_summary: '初始创建'
        }
      })
      documents.push(document)
      return fulfillJSON(route, document, 201)
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/documents/71/extraction-candidates/batch_decide') {
      const body = request.postDataJSON()
      candidateDecisionRequests.push(structuredClone(body))
      const family = documentCandidateFamilyResponse.data.find(item => item.variants.some(group => group.candidate.id === body.winner_candidate_id))
      if (!family || body.snapshot_token !== family.snapshot_token) return fulfillJSON(route, { error: 'candidate family snapshot stale', error_code: 'candidate_family_snapshot_stale' }, 409)
      const statuses = new Map(family.variants.map(group => [group.candidate.id, group.candidate.id === body.winner_candidate_id ? 'retained' : 'rejected']))
      const decidedCandidates = []
      documentCandidateFamilyResponse.data.forEach(family => {
        family.variants.forEach(group => {
          const status = statuses.get(group.candidate.id)
          if (!status) return
          group.state = status
          group.candidate.status = status
          group.candidate.version += 1
          group.occurrences.forEach(occurrence => {
            if (occurrence.candidate_id !== group.candidate.id) return
            occurrence.status = status
            occurrence.version += 1
          })
          decidedCandidates.push(structuredClone(group.candidate))
        })
      })
      const allVariants = documentCandidateFamilyResponse.data.flatMap(family => family.variants)
      documentCandidateFamilyResponse.variant_status_counts = allVariants.reduce((counts, group) => {
        counts[group.state] += 1
        return counts
      }, { pending: 0, retained: 0, rejected: 0, formalized: 0 })
      const winner = decidedCandidates.find(candidate => candidate.id === body.winner_candidate_id)
      family.snapshot_token = createCandidateFamilySnapshotToken(family)
      const decision = {
        id: candidateFamilyDecisions.length + 1,
        document_id: 71,
        candidate_type: winner?.candidate_type,
        code: winner?.code,
        winner_candidate_id: body.winner_candidate_id,
        reason: body.reason,
        members: decidedCandidates.map(candidate => ({ candidate_id: candidate.id, semantic_fingerprint: `fingerprint-${candidate.id}`, name: candidate.name, version: candidate.version, status: candidate.status })),
        created_by: 1,
        created_at: '2026-09-09T08:00:00Z'
      }
      candidateFamilyDecisions.unshift(decision)
      if (family) family.decision_count = candidateFamilyDecisions.filter(item => item.candidate_type === family.candidate_type && item.code === family.code).length
      return fulfillJSON(route, { decision, candidates: decidedCandidates })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/documents/72/revisions/721/file' && options.uploadError) {
      actionRequests.push(path)
      return fulfillJSON(route, { error: options.uploadError }, 413)
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/documents/72/revisions/721/file') {
      actionRequests.push(path)
      const document = documents.find(item => item.id === 72)
      if (document?.draft_revision) {
        document.version += 1
        Object.assign(document.draft_revision, {
          file_name: 'outdoor-governance.md',
          file_size: 21,
          media_type: 'text/markdown',
          content_sha256: 'a'.repeat(64)
        })
      }
      return fulfillJSON(route, {
        message: '上传成功',
        file_name: document?.draft_revision?.file_name,
        file_size: document?.draft_revision?.file_size,
        version: document?.version
      })
    }
    if (request.method() === 'POST' && path === '/api/v1/standard/metrics/51/documents/link') {
      actionRequests.push(path)
      documents.forEach(document => { document.linkedToMetric = true })
      metricDocumentLinked = true
      const document = documents[0]
      const metric = metrics.find(item => item.id === 51)
      if (metric) metric.version = (metric.version || 1) + 1
      return fulfillJSON(route, { version: metric?.version || 2 })
    }
    if (path === '/api/v1/system/refresh') {
      return fulfillJSON(route, { access_token: 'standard-e2e-token', expires_in: 3600 })
    }
    if (path === '/api/v1/system/users/me') {
      return fulfillJSON(route, { id: 1, username: 'standard-e2e' })
    }
    if (path === '/api/v1/system/auth/context') {
      const token = request.headers().authorization?.replace(/^Bearer\s+/i, '')
      return fulfillJSON(route, {
        context: { type: 'tenant' },
        authorization: { role_assignments: [{ permissions: authContextPermissionsByToken[token] ?? permissions }] }
      })
    }
    if (path === '/api/v1/standard/domains') return fulfillJSON(route, domains)
    if (path === '/api/v1/standard/glossaries') {
      if (options.glossaryListRace) {
        glossaryListRaceRequests += 1
        const keyword = url.searchParams.get('keyword') || ''
        if (keyword === 'old') await new Promise(resolve => setTimeout(resolve, 250))
        const data = keyword === 'new'
          ? [{ ...glossaryFixtures[0], id: 22, draft_revision: { ...glossaryFixtures[0].draft_revision, name: '新筛选结果' } }]
          : keyword === 'old'
            ? [{ ...glossaryFixtures[0], id: 23, draft_revision: { ...glossaryFixtures[0].draft_revision, name: '旧筛选结果' } }]
            : glossaryFixtures
        return fulfillJSON(route, { data, total: data.length })
      }
      const domainID = Number(url.searchParams.get('owner_domain_id'))
      const data = domainID ? glossaryFixtures.filter(item => item.owner_domain_id === domainID) : glossaryFixtures
      return fulfillJSON(route, { data, total: data.length })
    }
    if (path === '/api/v1/standard/glossaries/21') {
      if (request.method() === 'PUT') Object.assign(glossaryFixtures[0], request.postDataJSON(), { version: glossaryFixtures[0].version + 1 })
      return fulfillJSON(route, glossaryFixtures[0])
    }
    if (path === '/api/v1/standard/glossaries/21/revisions') {
      if (request.method() === 'POST') {
        const aggregate = glossaryFixtures[0]
        aggregate.draft_revision = { ...aggregate.current_revision, ...request.postDataJSON(), id: 212, revision_no: 2, status: 'draft' }
        aggregate.draft_revision_id = 212
        aggregate.version += 1
        return fulfillJSON(route, aggregate, 201)
      }
      return fulfillJSON(route, [glossaryFixtures[0].draft_revision, glossaryFixtures[0].current_revision, ...glossaryHistory].filter(Boolean))
    }
    const glossaryRevisionMatch = path.match(/^\/api\/v1\/standard\/glossaries\/21\/revisions\/(\d+)$/)
    if (glossaryRevisionMatch) {
      const aggregate = glossaryFixtures[0]
      const revision = [aggregate.draft_revision, aggregate.current_revision, ...glossaryHistory].find(item => item?.id === Number(glossaryRevisionMatch[1]))
      if (!revision) return fulfillJSON(route, { error: '修订不存在' }, 404)
      if (revision.id === 210 && options.glossaryRevisionDenied) return fulfillJSON(route, { error: '无权读取修订' }, 403)
      if (request.method() === 'PUT') {
        Object.assign(revision, request.postDataJSON())
        aggregate.version += 1
        return fulfillJSON(route, aggregate)
      }
      return fulfillJSON(route, revision)
    }
    if (path === '/api/v1/standard/glossaries/21/elements') {
      if (request.method() === 'PUT') {
        const body = request.postDataJSON()
        if (body.version !== glossaryFixtures[0].version) return fulfillJSON(route, { error: '资源版本冲突' }, 409)
        glossaryElementIDs = body.element_ids
        glossaryFixtures[0].version += 1
        return fulfillJSON(route, glossaryFixtures[0])
      }
      return fulfillJSON(route, elementAggregates.filter(item => glossaryElementIDs.includes(item.id)).map(item => {
        const revision = item.current_revision || item.draft_revision
        return { id: item.id, code: item.code, lifecycle_state: item.lifecycle_state, name: revision.name, revision_id: revision.id, revision_no: revision.revision_no, status: revision.status, is_effective: revision.status === 'published' }
      }))
    }
    if (path === '/api/v1/standard/glossaries/21/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/elements') return fulfillJSON(route, { data: elementAggregates, total: elementAggregates.length })
    if (path === '/api/v1/standard/elements/41') {
      const element = elementAggregates.find(item => item.id === 41)
      if (request.method() === 'PUT') {
        if (options.elementVersionConflict) return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
        Object.assign(element, request.postDataJSON(), { version: element.version + 1 })
      }
      return fulfillJSON(route, element || {})
    }
    if (request.method() === 'PUT' && path === '/api/v1/standard/elements/41/revisions/411') {
      if (options.pendingElementSave) await options.pendingElementSave
      if (options.elementVersionConflict) return fulfillJSON(route, { error: '资源已被其他用户修改，请刷新后重试' }, 409)
      const element = elementAggregates.find(item => item.id === 41)
      const { version, ...payload } = request.postDataJSON()
      Object.assign(element.draft_revision, payload)
      element.version += 1
      return fulfillJSON(route, element)
    }
    if (path === '/api/v1/standard/elements/41/revisions') {
      const element = elementAggregates.find(item => item.id === 41)
      if (request.method() === 'POST') {
        element.draft_revision = { ...element.current_revision, id: 412, revision_no: 2, status: 'draft', change_summary: request.postDataJSON().change_summary }
        element.draft_revision_id = 412
        element.version += 1
        return fulfillJSON(route, element)
      }
      const revision = element?.draft_revision || element?.current_revision
      return fulfillJSON(route, [...(options.elementHistory || []), ...(revision ? [revision] : [])])
    }
    if (request.method() === 'GET' && /^\/api\/v1\/standard\/elements\/41\/revisions\/\d+$/.test(path)) {
      const element = elementAggregates.find(item => item.id === 41)
      const revision = [...(options.elementHistory || []), element?.draft_revision, element?.current_revision].find(item => item?.id === Number(path.split('/').at(-1)))
      const detail = revision ? { ...revision, code_set_revision: options.elementCodeSetSnapshots?.[revision.code_set_revision_id] } : null
      return route.fulfill({ status: options.forbidElementRevision ? 403 : revision ? 200 : 404, contentType: 'application/json', body: JSON.stringify(options.forbidElementRevision ? { error: '无权读取此数据元修订' } : detail || { error: '数据元修订不存在' }) })
    }
    if (path === '/api/v1/standard/elements/41/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/code-sets') {
      return fulfillJSON(route, { data: [codeSetAggregate], total: 1 })
    }
    if (path === '/api/v1/standard/code-sets/31') return fulfillJSON(route, codeSetAggregate)
    if (path === '/api/v1/standard/code-sets/31/revisions') return fulfillJSON(route, [codeSetRevision])
    if (path === '/api/v1/standard/code-sets/31/documents') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/measurement-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/units') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/metric-categories') return fulfillJSON(route, [])
    if (path === '/api/v1/standard/metrics') return fulfillJSON(route, { data: metrics, total: metrics.length })
    if (path === '/api/v1/standard/metrics/51') return fulfillJSON(route, metrics.find(item => item.id === 51) || {})
    if (path === '/api/v1/standard/metrics/51/revisions') {
      const metric = metrics.find(item => item.id === 51)
      const revision = metric?.draft_revision || metric?.current_revision
      return fulfillJSON(route, revision ? [revision] : [])
    }
    if (path === '/api/v1/standard/metrics/51/documents') {
      metricDocumentListRequests += 1
      return fulfillJSON(route, metricDocumentLinked ? documents : [])
    }
    if (path === '/api/v1/standard/documents') return fulfillJSON(route, { data: documents, total: documents.length })
    if (path === '/api/v1/standard/documents/71') return fulfillJSON(route, documents.find(item => item.id === 71) || {})
    if (path === '/api/v1/standard/documents/71/revisions') {
      const document = documents.find(item => item.id === 71)
      const revision = document?.draft_revision || document?.current_revision
      return fulfillJSON(route, revision ? [revision] : [])
    }
    if (path === '/api/v1/standard/documents/71/extraction-candidate-families') return fulfillJSON(route, filterCandidateFamilyResponse(documentCandidateFamilyResponse, url))
    if (path === '/api/v1/standard/documents/71/extraction-candidate-family-decisions') {
      const candidateType = url.searchParams.get('candidate_type')
      const code = url.searchParams.get('code')
      const page = Number(url.searchParams.get('page')) || 1
      const pageSize = Number(url.searchParams.get('page_size')) || 20
      const matched = candidateFamilyDecisions.filter(item => item.candidate_type === candidateType && item.code === code)
      return fulfillJSON(route, { data: matched.slice((page - 1) * pageSize, page * pageSize), total: matched.length, page, page_size: pageSize, total_pages: Math.max(1, Math.ceil(matched.length / pageSize)) })
    }
    if (path === '/api/v1/standard/documents/71/mappings') {
      return fulfillJSON(route, { elements: [], glossaries: [], metrics: [] })
    }
    if (path === '/api/v1/standard/documents/72') return fulfillJSON(route, documents.find(item => item.id === 72) || {})
    if (path === '/api/v1/standard/documents/72/revisions') {
      const document = documents.find(item => item.id === 72)
      const revision = document?.draft_revision || document?.current_revision
      return fulfillJSON(route, revision ? [revision] : [])
    }
    if (path === '/api/v1/standard/documents/72/extraction-candidate-families') return fulfillJSON(route, filterCandidateFamilyResponse(documentCandidateFamilyResponse, url))
    if (path === '/api/v1/standard/documents/72/mappings') return fulfillJSON(route, { elements: [], glossaries: [], metrics: [] })
    return fulfillJSON(route, {})
  })

  return {
    getActionRequests: () => [...actionRequests],
    getDeleteRequests: () => [...deleteRequests],
    getDomainUpdateRequests: () => [...domainUpdateRequests],
    getDomainDeleteRequests: () => [...domainDeleteRequests],
    getCandidateDecisionRequests: () => structuredClone(candidateDecisionRequests),
    getGlossaryCreateRequests: () => glossaryCreateRequests,
    isMetricDocumentLinked: () => metricDocumentLinked
  }
}

async function fulfillJSON(route, body, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(body)
  })
}
