import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { json, login, isAnonymousRefreshConsoleError, isAnonymousRefreshResponse } from './transfer-browser-support.js'
import { expectSourceFactsLayout, expectFingerprintHover, expectFingerprintFocus } from '../helpers/catalog-source-facts.js'

const requiredNames = [
  'ADDP_ONLINE_ARTIFACT_DIR',
  'ADDP_ONLINE_TEST_RUN_ID',
  'ADDP_ONLINE_TEST_TENANT_ID',
  'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
  'ADDP_ONLINE_TEST_USER_USERNAME',
  'ADDP_ONLINE_TEST_USER_PASSWORD',
  'ADDP_ONLINE_CATALOG_ENTRY_ID',
  'ADDP_ONLINE_CATALOG_SOURCE_IDENTITY',
  'ADDP_ONLINE_CATALOG_BUSINESS_NAME',
  'ADDP_ONLINE_CATALOG_COVERAGE_TOTAL',
  'ADDP_ONLINE_CATALOG_SHARING_DECISION_ID',
  'ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID',
  'ADDP_ONLINE_ASSET_CATEGORY_ID',
  'ADDP_ONLINE_ASSET_ID',
  'GATEWAY_URL'
]

function environment() {
  const missing = requiredNames.filter(name => !process.env[name])
  if (missing.length > 0) throw new Error(`missing Online environment: ${missing.join(', ')}`)
  return Object.fromEntries(requiredNames.map(name => [name, process.env[name]]))
}

function findDomain(nodes, id) {
  for (const domain of nodes) {
    if (String(domain.id) === id) return domain
    const child = findDomain(domain.children || [], id)
    if (child) return child
  }
  return null
}

function grantedPermissions(identity) {
  const tenantID = identity?.context?.tenant_id
  return new Set((identity?.authorization?.role_assignments || [])
    .filter(assignment => assignment.scope?.type === 'tenant' && assignment.scope.tenant_id === tenantID)
    .flatMap(assignment => assignment.permissions || []))
}

test('enterprise Catalog renders governance coverage, human-readable navigation, and the canonical entry', async ({ page }) => {
  const env = environment()
  const coveragePath = '/catalog/governance/coverage'
  const totalEntries = Number(env.ADDP_ONLINE_CATALOG_COVERAGE_TOTAL)
  if (!Number.isInteger(totalEntries) || totalEntries <= 0) {
    throw new Error('ADDP_ONLINE_CATALOG_COVERAGE_TOTAL must be positive')
  }
  const api = await request.newContext({
    baseURL: env.GATEWAY_URL,
    extraHTTPHeaders: { Authorization: `Bearer ${env.ADDP_ONLINE_TEST_USER_ACCESS_TOKEN}` }
  })
  const browserMessages = [], failedBusinessResponses = []
  let businessStarted = false, anonymousRefresh401 = 0
  page.on('request', observed => {
    if (new URL(observed.url()).pathname === '/api/v1/system/auth/context' && observed.headers().authorization) businessStarted = true
  })
  page.on('response', response => {
    const pathname = new URL(response.url()).pathname
    if (isAnonymousRefreshResponse(response, businessStarted)) { anonymousRefresh401++; return }
    if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
      failedBusinessResponses.push({ pathname, status: response.status() })
    }
  })
  page.on('console', message => {
    if (['warning', 'error'].includes(message.type()) && !isAnonymousRefreshConsoleError(message, businessStarted)) {
      browserMessages.push({ type: message.type(), text: message.text() })
    }
  })
  page.on('pageerror', error => browserMessages.push({ type: 'pageerror', text: error.message }))

  let browserAPI
  try {
    const apiIdentity = await json(await api.get('/api/v1/system/auth/context'), 'read API AuthContext')
    expect(apiIdentity?.principal?.type).toBe('user')
    expect(String(apiIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)

    const browserAccessToken = await login(
      page,
      env.ADDP_ONLINE_TEST_USER_USERNAME,
      env.ADDP_ONLINE_TEST_USER_PASSWORD,
      coveragePath
    )
    browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = await json(
      await browserAPI.get('/api/v1/system/auth/context'),
      'read browser AuthContext'
    )
    expect(String(browserIdentity?.principal?.id)).toBe(String(apiIdentity?.principal?.id))
    expect(browserIdentity?.principal?.type).toBe('user')
    expect(String(browserIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    const currentUser = await json(await browserAPI.get('/api/v1/system/users/me'), 'read current browser user')
    expect(String(currentUser.id)).toBe(String(browserIdentity.principal.id))
    expect(currentUser.username).toBe(env.ADDP_ONLINE_TEST_USER_USERNAME)
    expect(typeof currentUser.display_name).toBe('string')
    expect(currentUser.display_name.trim()).not.toBe('')
    const currentEntry = await json(await browserAPI.get(`/api/v1/catalog/entries/${env.ADDP_ONLINE_CATALOG_ENTRY_ID}`), 'read technical source names')
    expect(currentEntry.source_engine?.status).toBe('current')
    expect(currentEntry.source.source_identity).toBe(env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)
    const snapshot = currentEntry.source.observed_snapshot
    const expectedSource = { engineName: currentEntry.source_engine.name, engineType: currentEntry.source_engine.engine_type,
      itemName: snapshot.name, itemType: snapshot.item_type, fullName: snapshot.full_name }
    const decisionID = env.ADDP_ONLINE_CATALOG_SHARING_DECISION_ID
    const decision = await json(await browserAPI.get(`/api/v1/catalog/entries/${env.ADDP_ONLINE_CATALOG_ENTRY_ID}/sharing_decisions/${decisionID}`), 'read exact sharing confirmation')
    expect(decision.id).toBe(decisionID)
    expect(decision.recipient_type).toBe('user')
    expect(decision.recipient_id).toBe(browserIdentity.principal.id)
    expect(decision.recipient_label_status).toBe('resolved')
    expect(decision.recipient_name).toBe(currentUser.display_name)
    if (decision.recipient_code) expect(decision.recipient_code).toBe(currentUser.username)
    expect(decision.reason).toContain(env.ADDP_ONLINE_TEST_RUN_ID)
    const historyName = [currentUser.display_name, decision.recipient_code].filter(Boolean).join(' · ')
    const recipientLabel = `账号 · ${historyName} (当前账号)`
    const candidateLabel = `账号 · ${currentUser.display_name} · ${currentUser.username} (当前账号)`
    const sharingCommands = []
    page.on('request', observed => {
      const path = new URL(observed.url()).pathname
      if (observed.method() !== 'GET' && path.startsWith(`/api/v1/catalog/entries/${env.ADDP_ONLINE_CATALOG_ENTRY_ID}/sharing_`)) {
        sharingCommands.push(`${observed.method()} ${path}`)
      }
    })

    let frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const coverage = frame.getByTestId('catalog-governance-coverage')
    await expect(coverage).toHaveAttribute('data-load-state', 'loaded')
    await expect(coverage).toHaveAttribute('data-total-entries', String(totalEntries))
    await expect(frame.getByTestId('catalog-coverage-dimension')).toHaveCount(7)
    await expect(coverage).not.toContainText('undefined')

    await frame.locator('[data-testid="catalog-coverage-status-link"][data-status="curated"]').click()
    await expect.poll(() => new URL(page.url()).searchParams.get('governance_status')).toBe('curated')
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByTestId('catalog-entry-list')).toHaveAttribute('data-load-state', 'loaded')
    await expect(frame.getByTestId('catalog-entry-results')).toContainText(env.ADDP_ONLINE_CATALOG_BUSINESS_NAME)

    await page.goto('/catalog/entries?view=inventory&governance_status=discovered')
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByTestId('catalog-entry-list')).toHaveAttribute('data-load-state', 'loaded')
    await expect(frame.getByTestId('catalog-discovered-guidance')).toContainText('批量分配主业务域或责任部门不会改变编目状态')

    await page.goto('/catalog/entries?view=inventory&coverage_dimension=accountable_department&coverage_state=missing')
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByTestId('catalog-entry-list')).toHaveAttribute('data-load-state', 'loaded')
    await expect(frame.getByTestId('catalog-coverage-gap-alert')).toContainText('可在缺口列表勾选条目并批量分配责任部门')

    const detailPath = `/catalog/entries/${env.ADDP_ONLINE_CATALOG_ENTRY_ID}?view=inventory`
    await page.goto(detailPath)
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const detail = frame.getByTestId('catalog-entry-detail')
    await expect(detail).toHaveAttribute('data-load-state', 'loaded')
    await expect(detail).toHaveAttribute('data-entry-id', env.ADDP_ONLINE_CATALOG_ENTRY_ID)
    await expect(detail).toHaveAttribute('data-governance-status', 'curated')
    await expect(detail.locator('h1')).toHaveText(env.ADDP_ONLINE_CATALOG_BUSINESS_NAME)
    await expect(frame.getByTestId('catalog-curation-action')).toBeVisible()
    await frame.getByTestId('catalog-more-actions').click()
    await expect(frame.getByTestId('catalog-certify-action')).toBeVisible()
    await expect(frame.getByTestId('catalog-deprecate-action')).toBeVisible()
    await expect(frame.getByTestId('catalog-withdraw-curation-action')).toBeVisible()
    await page.keyboard.press('Escape')
    await detail.getByRole('tab', { name: '专业事实', exact: true }).click()
    const facts = detail.locator('.source-facts-descriptions')
    for (const width of [1440, 900]) {
      await page.setViewportSize({ width, height: 1000 })
      await expectSourceFactsLayout(facts, expectedSource)
      await expectFingerprintHover(frame, facts, env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)
      await expectFingerprintFocus(frame, facts, env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)
    }
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.reload()
    await expect(detail.getByRole('tab', { name: '专业事实', exact: true })).toHaveAttribute('aria-selected', 'true')
    await expectSourceFactsLayout(facts, expectedSource)
    await expectFingerprintFocus(frame, facts, env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)
    await expect(detail).not.toContainText('undefined')

    await page.goto(`/catalog/entries/${env.ADDP_ONLINE_CATALOG_ENTRY_ID}?tab=sharing&sharing_decision_id=${decisionID}`)
    const confirmation = frame.getByTestId('sharing-confirmation-detail')
    const recipient = confirmation.locator('td.el-descriptions__label').filter({ hasText: /^接收方$/ })
      .locator('xpath=following-sibling::td[1]')
    await expect(recipient).toHaveText(recipientLabel)
    await page.reload()
    await expect(recipient).toHaveText(recipientLabel)
    await frame.getByTestId('sharing-back-records').click()
    const record = frame.getByTestId('sharing-confirmations').locator('.el-table__body tbody tr')
      .filter({ hasText: decision.reason })
    await expect(record).toHaveCount(1)
    await expect(record.locator('td').first()).toHaveText(recipientLabel)
    await record.getByTestId('sharing-open-confirmation').click()
    await expect.poll(() => new URL(page.url()).searchParams.get('sharing_decision_id')).toBe(decisionID)
    await expect(recipient).toHaveText(recipientLabel)
    await frame.getByTestId('sharing-workspace').getByText('批准数据使用', { exact: true }).click()
    const recipientSelect = frame.getByTestId('sharing-create-panel').locator('.el-select').nth(1)
    await recipientSelect.click()
    await recipientSelect.getByRole('combobox').fill(currentUser.username)
    const currentOption = frame.getByRole('option').filter({ hasText: candidateLabel })
    await expect(currentOption).toHaveCount(1)
    await currentOption.click()
    await expect(recipientSelect).toContainText(candidateLabel)
    expect(sharingCommands).toEqual([])

    await page.goto(`/catalog/entries?view=governance&source_identity=${encodeURIComponent(env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)}`)
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const discoveryList = frame.getByTestId('catalog-entry-list')
    await expect(discoveryList).toHaveAttribute('data-load-state', 'loaded')
    await expect(discoveryList.getByTestId('catalog-entry-results')).toContainText(env.ADDP_ONLINE_CATALOG_BUSINESS_NAME)
    await expect(discoveryList.getByRole('columnheader', { name: '业务说明' })).toBeVisible()
    await expect(discoveryList.getByRole('columnheader', { name: '来源状态' })).toHaveCount(0)
    await expect(discoveryList.getByRole('columnheader', { name: '来源引擎' })).toHaveCount(0)

    await page.goto(`/catalog/entries?view=inventory&source_identity=${encodeURIComponent(env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)}`)
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const entryList = frame.getByTestId('catalog-entry-list')
    await expect(entryList).toHaveAttribute('data-load-state', 'loaded')
    await expect(entryList.getByRole('columnheader', { name: '业务说明' })).toHaveCount(0)
    await expect(entryList.getByRole('columnheader', { name: '来源状态' })).toBeVisible()
    await expect(entryList.getByRole('columnheader', { name: '来源引擎' })).toBeVisible()
    const viewScope = frame.getByTestId('catalog-view-scope')
    await expect(viewScope).toHaveAttribute('data-active-view', 'inventory')
    await expect(viewScope).toHaveAttribute('data-result-total', /\d+/)
    await expect(frame.getByTestId('catalog-entry-navigation')).toHaveCount(1)
    for (const testID of ['catalog-domain-navigation', 'catalog-department-navigation', 'catalog-entry-type-navigation']) {
      const navigation = frame.getByTestId(testID)
      await expect(navigation).toHaveCount(1)
      await expect(navigation.getByRole('combobox')).toBeVisible()
    }
    await frame.getByTestId('catalog-advanced-filters-toggle').click()
    const engineSelector = frame.getByTestId('catalog-engine-filter')
    await expect(engineSelector).toHaveCount(1)
    await expect(engineSelector.getByRole('combobox')).toHaveCount(1)
    await expect(entryList).toContainText(env.ADDP_ONLINE_CATALOG_BUSINESS_NAME)
    await expect(entryList).not.toContainText('undefined')
    const batchToolbar = frame.getByTestId('catalog-batch-governance-toolbar')
    await expect(batchToolbar).toBeVisible()
    await entryList.locator('tbody .el-checkbox').first().click()
    const openBatchGovernance = frame.getByTestId('catalog-batch-governance-open')
    await expect(openBatchGovernance).toBeEnabled()
    await openBatchGovernance.click()
    const batchDialog = frame.getByTestId('catalog-batch-governance-dialog')
    await expect(batchDialog).toBeVisible()
    await expect(frame.getByTestId('catalog-batch-governance-operation')).toBeVisible()
    await expect(frame.getByTestId('catalog-batch-governance-target')).toBeVisible()
    await batchDialog.locator('.el-dialog__headerbtn').click()

    const domains = await json(await api.get('/api/v1/catalog/domains'), 'read Catalog domain overviews')
    const domain = findDomain(domains, env.ADDP_ONLINE_TEST_CATALOG_DOMAIN_ID)
    expect(domain, 'configured online business domain exists in Standard').not.toBeNull()
    await page.goto(`/catalog/entries?view=inventory&primary_domain_id=${encodeURIComponent(domain.id)}`)
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const domainList = frame.getByTestId('catalog-entry-list')
    await expect(domainList).toHaveAttribute('data-load-state', 'loaded')
    const domainContext = frame.getByTestId('catalog-domain-context')
    await expect(domainContext).toContainText(domain.name)
    await expect(domainContext).toContainText(domain.code)
    await expect(domainList.getByTestId('catalog-entry-results')).toContainText(env.ADDP_ONLINE_CATALOG_BUSINESS_NAME)
    const professionalToggle = frame.getByTestId('catalog-domain-professional-toggle')
    await expect(professionalToggle).toHaveAttribute('aria-expanded', 'false')
    await expect(frame.getByTestId('catalog-domain-professional-summary')).toHaveCount(0)
    await professionalToggle.click()
    await expect(professionalToggle).toHaveAttribute('aria-expanded', 'true')
    const professionalSummary = frame.getByTestId('catalog-domain-professional-summary')
    await expect(professionalSummary).toBeVisible()
    await expect(frame.getByTestId('catalog-domain-model-links')).toBeVisible()
    const permissions = grantedPermissions(browserIdentity)
    for (const [key, permission] of [
      ['glossaries', 'standard.glossary.read'],
      ['elements', 'standard.element.read'],
      ['metrics', 'standard.metric.read']
    ]) {
      await expect(frame.getByTestId(`catalog-domain-standard-${key}`))
        .toHaveAttribute('data-state', permissions.has(permission) ? 'ready' : 'forbidden')
    }
    for (const [key, permission] of [
      ['rules', 'quality.rule.read'],
      ['plans', 'quality.plan.read'],
      ['issues', 'quality.issue.read']
    ]) {
      await expect(frame.getByTestId(`catalog-domain-quality-${key}`))
        .toHaveAttribute('data-state', permissions.has(permission) ? 'ready' : 'forbidden')
    }
    await professionalToggle.click()
    await expect(professionalToggle).toHaveAttribute('aria-expanded', 'false')
    await expect(professionalSummary).toHaveCount(0)
    await domainContext.getByRole('button', { name: /数据项 · \d+/ }).click()
    await expect.poll(() => new URL(page.url()).searchParams.get('entry_type')).toBe('data_item')
    await page.reload()
    frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    await expect(frame.getByTestId('catalog-domain-context')).toContainText(domain.name)
    await expect(frame.getByTestId('catalog-entry-results')).toContainText(env.ADDP_ONLINE_CATALOG_BUSINESS_NAME)

    const portalCategoryPath = `/portal/categories/${env.ADDP_ONLINE_ASSET_CATEGORY_ID}`
    await page.goto(portalCategoryPath)
    const portalCategory = page.getByTestId('portal-category-page')
    await expect(portalCategory).toHaveAttribute('data-load-state', 'loaded')
    await expect(page.getByTestId('portal-category-tree')).toContainText(`Online Catalog ${env.ADDP_ONLINE_TEST_RUN_ID}`)
    const portalAsset = page
      .getByTestId('portal-asset-card')
      .filter({ hasText: `Online Asset ${env.ADDP_ONLINE_TEST_RUN_ID}` })
    await expect(portalAsset).toHaveCount(1)
    await expect(portalAsset).toHaveAttribute('data-asset-id', env.ADDP_ONLINE_ASSET_ID)
    await expect(portalCategory).not.toContainText('undefined')

    expect(anonymousRefresh401).toBeLessThanOrEqual(1)
    expect(failedBusinessResponses).toEqual([])
    expect(browserMessages).toEqual([])
    expect(sharingCommands).toEqual([])
    const report = {
      schema_version: 'addp.enterprise-catalog-publishing-browser/v3',
      suite: 'enterprise-catalog-publishing',
      run_id: env.ADDP_ONLINE_TEST_RUN_ID,
      result: 'passed',
      tenant_id: env.ADDP_ONLINE_TEST_TENANT_ID,
      catalog_entry_id: env.ADDP_ONLINE_CATALOG_ENTRY_ID,
      source_identity: env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY,
      coverage_total_entries: totalEntries,
      coverage_dimensions: 7,
      human_readable_filter_selectors: 3,
      explicit_batch_governance_ui: true,
      technical_source_names: true,
      source_fingerprint_interactions: true,
      source_layout_viewports: [1440, 900],
      source_facts_reload: true,
      sharing_current_account: true,
      sharing_decision_id: decisionID,
      sharing_browser_commands: sharingCommands.length,
      catalog_lifecycle_actions: ['edit', 'certify', 'deprecate', 'withdraw-curation'],
      portal_category_id: env.ADDP_ONLINE_ASSET_CATEGORY_ID,
      portal_asset_id: env.ADDP_ONLINE_ASSET_ID,
      portal_category_assets: 1,
      browser_warning_errors: 0,
      failed_business_responses: 0,
      anonymous_refresh_401: anonymousRefresh401
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'enterprise-catalog-publishing-browser.json'),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    await browserAPI?.dispose()
    await api.dispose()
  }
})
