import { expect } from '@playwright/test'
import { resolve } from 'node:path'
import { identity } from './transfer-browser-support.js'

export function matchesTiffPermissionResponse(response, target, phase) {
  const url = new URL(response.url())
  const hidden = ['all_withdrawn', 'expired'].includes(phase)
  return response.request().method() === 'GET'
    && url.pathname === (hidden ? `/api/v1/meta/resource-tree/${target.engine_id}/ancestors` : '/api/v1/manager/preview')
    && url.searchParams.get('locator') === target.locator
    && url.searchParams.getAll('locator').length === 1
    && response.status() === (hidden ? 404 : 200)
}

// This observer issues no grants and performs no business writes. Python owns
// the transition; each call observes a newly opened page after that transition.
export async function observeTiffPermissionPage(page, evidence, artifactDir) {
  const { permission_phase: phase, nfs_tiff_authorization: target } = evidence
  expect(['metadata_only', 'read_withdrawn', 'all_withdrawn', 'expired']).toContain(phase)
  const hidden = ['all_withdrawn', 'expired'].includes(phase)
  const identities = new Map(), renders = [], errors = [], failures = [], content = [], consoleCandidates = [], consoleChecks = []
  const onResponse = async response => {
    const url = new URL(response.url())
    if (url.pathname === '/api/v1/system/auth/context' && response.ok()) {
      try {
        identities.set(response.request().headers().authorization, identity(await response.json()))
      } catch {
        errors.push({ type: 'identity', text: 'AuthContext response could not be verified' })
      }
    }
    if (url.pathname.startsWith('/api/v1/') && response.status() >= 400
      && !(hidden && matchesTiffPermissionResponse(response, target, phase))) {
      failures.push({ path: url.pathname, status: response.status() })
    }
  }
  const onRequest = request => {
    const path = new URL(request.url()).pathname
    if (/^\/api\/v1\/manager\/(storage-stream|downloads\/|raster_cog\/|data-profile|quick-view\/(actions|flatgeobuf|geojson|tiles))/.test(path)) {
      content.push({ path, method: request.method() })
    }
  }
  const onConsole = message => {
    if (!['warning', 'error'].includes(message.type())) return
    consoleChecks.push((async () => {
      let networkDenial = false
      if (hidden && message.type() === 'error'
        && message.text() === 'Failed to load resource: the server responded with a status of 404 (Not Found)') {
        try {
          networkDenial = matchesTiffPermissionResponse({ url: () => message.location().url,
            request: () => ({ method: () => 'GET' }), status: () => 404 }, target, phase)
        } catch { /* A missing network location is not an expected denial. */ }
      }
      // Inspect the original AxiosError, not just its localized console prefix.
      let navigationDenial = false
      if (hidden && message.type() === 'error' && message.text().startsWith('[DataExplorer] 定位失败:')) {
        try {
          const original = await message.args()[1].evaluate(error => ({ url: error.request?.responseURL,
            status: error.response?.status, method: error.config?.method?.toUpperCase() }))
          navigationDenial = matchesTiffPermissionResponse({ url: () => original.url,
            request: () => ({ method: () => original.method }), status: () => original.status }, target, phase)
        } catch { /* An unprovable diagnostic remains an error. */ }
      }
      if (networkDenial || navigationDenial) consoleCandidates.push(networkDenial ? 'network' : 'navigation')
      else errors.push({ type: message.type(), text: message.text() })
    })())
  }
  const onPageError = error => errors.push({ type: 'pageerror', text: error.message })
  page.on('response', onResponse); page.on('request', onRequest)
  page.on('console', onConsole); page.on('pageerror', onPageError)
  try {
    for (const locale of ['zh-cn', 'en']) {
      await page.addInitScript(language => localStorage.setItem('addp-lang', language), locale)
      for (const opening of ['initial', 'reopened']) {
        const observed = page.waitForResponse(response => matchesTiffPermissionResponse(response, target, phase))
        observed.catch(() => {})
        if (opening === 'initial') await page.goto(`/manager/data-explorer?locator=${encodeURIComponent(target.locator)}`)
        else await page.reload()
        const response = await observed
        const authorization = response.request().headers().authorization
        expect(authorization).toMatch(/^Bearer\s+\S+$/)
        await expect.poll(() => identities.has(authorization)).toBe(true)
        const actual = identities.get(authorization)
        expect(actual.principalType).toBe('user'); expect(actual.contextType).toBe('tenant')
        expect(actual.principalID).toBe(String(evidence.identity.principal_id))
        expect(actual.tenantID).toBe(String(evidence.identity.tenant_id))
        expect([...actual.permissions].sort()).toEqual([...evidence.identity.permissions_verified].sort())
        const frame = page.frameLocator('iframe[data-testid="module-iframe"]')
        const panel = frame.getByTestId(hidden ? 'preview-failure' : 'metadata-only-preview')
        await expect(panel).toBeVisible()
        await expect(panel).toContainText(hidden
          ? locale === 'zh-cn' ? '无法访问该资源' : 'Resource unavailable'
          : locale === 'zh-cn' ? '仅可查看元数据' : 'Metadata access only')
        if (hidden) await expect(panel).toContainText(locale === 'zh-cn'
          ? '资源可能已移除，或你暂无查看权限' : 'may have been removed, or you may not have permission')
        await expect(frame.locator('.el-message--error')).toHaveCount(0)
        await expect(frame.getByText(locale === 'zh-cn' ? '请选择数据进行预览' : 'Select data to preview', { exact: true })).toHaveCount(0)
        await expect(frame.locator('.preview-content, .panel-actions, .profile-tab-pane, .tiff-canvas, .raster-tiff-quick-view')).toHaveCount(0)
        if (hidden) {
          await expect(frame.locator('.attributes-panel')).toHaveCount(0)
          await expect(frame.getByRole('tab', { name: /^(?:属性|Attributes)$/, exact: true })).toHaveCount(0)
        } else {
          await frame.getByRole('tab', { name: /^(?:属性|Attributes)$/, exact: true }).click()
          await expect(frame.locator('.attributes-panel')).toContainText('single')
          await expect(frame.locator('.attributes-panel')).not.toContainText(/content_versions|sha256|physical_path/)
          await frame.getByRole('tab', { name: /^(?:预览|Preview)$/, exact: true }).click()
          await expect(panel).toBeVisible()
        }
        const screenshot = `tiff-permission-${phase}-${locale}-${opening}.png`
        await frame.locator('body').screenshot({ path: resolve(artifactDir, screenshot) })
        await Promise.all(consoleChecks)
        const url = new URL(response.url())
        renders.push({ locale, opening, screenshot, identity_verified: true,
          response: { method: 'GET', path: url.pathname, locator: url.searchParams.get('locator'), status: response.status() },
          message: hidden ? 'inaccessible' : 'metadata_only', content_absent: true, actions_absent: true,
          attributes_safe: true, raw_error_absent: true, empty_absent: true })
      }
    }
    await Promise.all(consoleChecks)
    expect(failures).toEqual([]); expect(errors).toEqual([]); expect(content).toEqual([])
    // At most one Chromium network diagnostic and one Owner diagnostic per observed denial.
    for (const kind of ['network', 'navigation']) expect(consoleCandidates.filter(value => value === kind).length).toBeLessThanOrEqual(hidden ? renders.length : 0)
    return { schema_version: 'addp.manager-tiff-permission-browser/v1', result: 'passed',
      run_id: evidence.run_id, identity: evidence.identity, phase, target, renders,
      unexpected_business_responses: failures.length, warnings_errors: errors.length, content_requests: content.length }
  } finally {
    page.off('response', onResponse); page.off('request', onRequest)
    page.off('console', onConsole); page.off('pageerror', onPageError)
  }
}
