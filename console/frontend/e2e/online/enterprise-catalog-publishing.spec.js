import { expect, request, test } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

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

async function json(response, operation) {
  const payload = await response.json()
  if (!response.ok()) {
    throw new Error(`${operation} returned HTTP ${response.status()} (${payload?.error_code || 'unknown'})`)
  }
  return payload
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

async function login(page, username, password, redirect) {
  let browserAccessToken = ''
  page.on('request', requestEvent => {
    if (!requestEvent.url().endsWith('/api/v1/system/auth/context')) return
    browserAccessToken = (requestEvent.headers().authorization || '').replace(/^Bearer\s+/i, '')
  })
  await page.goto(`/login?redirect=${encodeURIComponent(redirect)}`)
  await page.locator('input[autocomplete="username"]').fill(username)
  await page.locator('input[autocomplete="current-password"]').fill(password)
  await page.locator('button.auth-login-primary').click()
  const contextStep = page.locator('.auth-login-contexts')
  const needsContext = await contextStep.waitFor({ state: 'visible', timeout: 5000 })
    .then(() => true)
    .catch(() => false)
  if (needsContext) await contextStep.locator('button.auth-login-primary').click()
  if (await page.locator('input[autocomplete="one-time-code"]').isVisible().catch(() => false)) {
    throw new Error('the dedicated Online browser user must not require MFA')
  }
  await page.waitForURL(url => url.pathname === redirect)
  await expect.poll(() => browserAccessToken, { timeout: 20_000 }).not.toBe('')
  return browserAccessToken
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
  const browserMessages = []
  page.on('console', message => {
    if (['warning', 'error'].includes(message.type())) {
      browserMessages.push({ type: message.type(), text: message.text() })
    }
  })
  page.on('pageerror', error => browserMessages.push({ type: 'pageerror', text: error.message }))

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
    const browserAPI = await request.newContext({
      baseURL: env.GATEWAY_URL,
      extraHTTPHeaders: { Authorization: `Bearer ${browserAccessToken}` }
    })
    const browserIdentity = await json(
      await browserAPI.get('/api/v1/system/auth/context'),
      'read browser AuthContext'
    )
    expect(String(browserIdentity?.principal?.id)).toBe(String(apiIdentity?.principal?.id))
    expect(String(browserIdentity?.context?.tenant_id)).toBe(env.ADDP_ONLINE_TEST_TENANT_ID)
    await browserAPI.dispose()

    const failedBusinessResponses = []
    page.on('response', response => {
      const pathname = new URL(response.url()).pathname
      if (pathname.startsWith('/api/v1/') && response.status() >= 400) {
        failedBusinessResponses.push({ pathname, status: response.status() })
      }
    })

    let frame = page.frameLocator('iframe[data-testid="module-iframe"]')
    const coverage = frame.getByTestId('catalog-governance-coverage')
    await expect(coverage).toHaveAttribute('data-load-state', 'loaded')
    await expect(coverage).toHaveAttribute('data-total-entries', String(totalEntries))
    await expect(frame.getByTestId('catalog-coverage-dimension')).toHaveCount(7)
    await expect(coverage).not.toContainText('undefined')

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
    await detail.getByRole('tab').nth(2).click()
    await expect(detail).toContainText(env.ADDP_ONLINE_CATALOG_SOURCE_IDENTITY)
    await expect(detail).not.toContainText('undefined')

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

    expect(failedBusinessResponses).toEqual([])
    expect(browserMessages).toEqual([])
    const report = {
      schema_version: 'addp.enterprise-catalog-publishing-browser/v2',
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
      catalog_lifecycle_actions: ['edit', 'certify', 'deprecate', 'withdraw-curation'],
      portal_category_id: env.ADDP_ONLINE_ASSET_CATEGORY_ID,
      portal_asset_id: env.ADDP_ONLINE_ASSET_ID,
      portal_category_assets: 1,
      browser_warning_errors: 0
    }
    writeFileSync(
      resolve(env.ADDP_ONLINE_ARTIFACT_DIR, 'enterprise-catalog-publishing-browser.json'),
      `${JSON.stringify(report)}\n`,
      'utf8'
    )
  } finally {
    await api.dispose()
  }
})
