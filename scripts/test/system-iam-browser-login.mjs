import { createRequire } from 'node:module'
import { localIdentityTOTP } from '../../console/frontend/e2e/online/local-identity-support.mjs'

const require = createRequire(new URL('../../workbench/frontend/package.json', import.meta.url))
const { chromium, expect: playwrightExpect } = require('@playwright/test')
const expect = playwrightExpect.configure({ timeout: 12000 })

class CheckFailed extends Error {}
function check(condition, message) {
  if (!condition) throw new CheckFailed(message)
}

const system = suffix => '/api/v1/system/' + suffix
const requestPath = request => new URL(request.url()).pathname
function responseFor(page, path, method, status = 200) {
  return page.waitForResponse(response => requestPath(response.request()) === path &&
    response.request().method() === method && response.status() === status)
}

function verifyIdentity(payload, fixture) {
  check(payload.principal.type === 'user' && payload.principal.id === fixture.principal, 'unexpected principal')
  check(payload.context.type === 'tenant' && payload.context.tenant_id === fixture.tenant &&
    payload.context.tenant_membership_id === fixture.membership, 'unexpected tenant or membership')
  check(payload.authentication.assurance_level === 'aal2' &&
    payload.authentication.methods.join(',') === 'password,totp', 'expected password and TOTP at AAL2')
  const roles = payload.authorization.role_assignments
  check(roles.length === 1 && roles.every(role => role.scope.type === 'tenant' &&
    role.scope.tenant_id === fixture.tenant), 'unexpected tenant role assignment')
}

async function verifyRendered(page, entry, metrics) {
  if (entry === 'Workbench') {
    await expect(page.getByTestId('runtime-component')).toHaveCount(2)
    await expect.poll(() => metrics.requests.length).toBeGreaterThan(0)
    check(metrics.requests.every(request => request.body.parameters.grain === 'month'), 'original preset was not applied')
  } else {
    await expect(page.getByRole('heading', { name: '资产门户', exact: true })).toBeVisible()
    await expect(page.getByText('暂无已上架资产', { exact: true })).toBeVisible()
  }
}

async function verifyNoPersistedSecrets(page, secrets) {
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
  for (const secret of secrets) {
    check(!storage.includes(secret) && !page.url().includes(secret), 'secret persisted in storage or URL')
  }
}

let stage = 'read fixture'
let browser
try {
  const fixture = JSON.parse(Buffer.concat(await Array.fromAsync(process.stdin)).toString())
  const origin = new URL(fixture.origin)
  check(origin.protocol === 'http:' && origin.hostname === '127.0.0.1' && origin.port, 'expected a private loopback server')
  check(['Workbench', 'Portal'].includes(fixture.entry), 'unknown login entry')
  // The shared Workbench fixture resolves its public runtime against the run origin.
  process.env.ADDP_BROWSER_RUN = JSON.stringify({ ports: { console: Number(origin.port) } })
  const { applicationID, installMetricApplicationBackend } = await import('../../workbench/frontend/e2e/fixtures/metricApplication.js')

  stage = 'launch Chromium (install Workbench Playwright Chromium before running locally)'
  browser = await chromium.launch({ headless: true })
  const context = await browser.newContext({ baseURL: origin.origin, locale: 'zh-CN', serviceWorkers: 'block' })
  try {
    const metrics = await installMetricApplicationBackend(context, {
      mockSystem: false,
      permissions: ['workbench.data_application.execute'],
      configure(draft) {
        draft.snapshot.parameter_presets = [{
          key: 'monthly', name: 'Monthly', parameter_values: { shared_grain: 'month', direction: 'both' },
        }]
      },
    })
    await context.route(url => url.pathname === '/api/v1/portal/home', route =>
      route.fulfill({ json: { total_published: 0, type_stats: [], latest_assets: [] } }))
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.fallback() : route.abort())

    const workbench = '/data-apps/' + applicationID + '?preset=monthly#summary'
    const portal = '/portal/home?source=auth-acceptance#latest'
    const opensWorkbench = fixture.entry === 'Workbench'
    const target = opensWorkbench ? workbench : portal
    const loginPath = opensWorkbench ? '/module-ui/workbench/login' : '/portal/login'
    const page = await context.newPage()
    const businessRequests = []
    page.on('request', request => {
      const path = requestPath(request)
      if (path.startsWith('/api/') && !path.startsWith('/api/v1/system/')) businessRequests.push(path)
    })
    const checkAnonymous = async () => {
      await expect(page).toHaveURL(url => url.pathname === loginPath && url.searchParams.get('redirect') === target)
      check(!(await context.cookies()).some(cookie => cookie.name === 'addp_refresh_token'), 'refresh cookie issued before tenant selection')
      check(businessRequests.length === 0, 'business API called before login completed')
    }

    stage = 'anonymous redirect'
    await page.goto(target)
    await checkAnonymous()

    stage = 'password requires MFA'
    await page.locator('input[autocomplete=username]').fill(fixture.username)
    await page.locator('input[autocomplete=current-password]').fill(fixture.password)
    const submit = page.getByRole('button', { name: '登录', exact: true })
    const [passwordResponse] = await Promise.all([responseFor(page, system('login'), 'POST'), submit.click()])
    const challenge = await passwordResponse.json()
    check(challenge.next_action === 'verify_mfa' && challenge.mfa.method === 'totp' && !challenge.session, 'unexpected password response')
    const code = page.locator('input[autocomplete=one-time-code]')
    await expect(code).toBeVisible()
    await checkAnonymous()

    stage = 'invalid TOTP stays on MFA'
    const now = Date.now()
    const nearbyCodes = [-2, -1, 0, 1, 2].map(step => localIdentityTOTP(fixture.totpSecret, now + step * 30000))
    await code.fill(['000000', '000001', '000002', '000003', '000004', '000005'].find(value => !nearbyCodes.includes(value)))
    const verify = page.getByRole('button', { name: '验证', exact: true })
    await Promise.all([responseFor(page, system('auth/mfa-verifications'), 'POST', 401), verify.click()])
    await expect(page.getByText('动态验证码错误或已失效', { exact: true })).toBeVisible()
    await checkAnonymous()

    stage = 'valid TOTP requires a tenant choice'
    await code.fill(localIdentityTOTP(fixture.totpSecret))
    const [mfaResponse] = await Promise.all([responseFor(page, system('auth/mfa-verifications'), 'POST'), verify.click()])
    const selection = await mfaResponse.json()
    check(selection.next_action === 'select_context' && !selection.session, 'unexpected MFA response')
    const options = selection.selection.contexts
    check(options.length === 2 && options.every(option => option.type === 'tenant') &&
      options.some(option => option.tenant_id === fixture.tenant &&
        option.tenant_membership_id === fixture.membership), 'expected two tenant memberships including the selected one')
    await expect(page.getByRole('radio')).toHaveCount(2)
    const proceed = page.getByRole('button', { name: '继续', exact: true })
    await expect(proceed).toBeDisabled()
    await checkAnonymous()
    const secrets = [fixture.password, challenge.mfa.challenge_token, selection.selection.selection_ticket]
    await verifyNoPersistedSecrets(page, secrets)

    stage = 'explicit tenant and complete return URL'
    await page.getByText(fixture.tenantName, { exact: true }).click()
    await expect(page.getByRole('radio', { name: fixture.tenantName, exact: true })).toBeChecked()
    const [selected] = await Promise.all([
      responseFor(page, system('auth/context-selections'), 'POST'),
      responseFor(page, system('auth/context'), 'GET'),
      proceed.click(),
    ])
    const choice = selected.request().postDataJSON()
    check(choice.context_type === 'tenant' && choice.tenant_membership_id === fixture.membership &&
      choice.selection_ticket === selection.selection.selection_ticket, 'submitted the wrong tenant choice')
    await expect(page).toHaveURL(origin.origin + target)
    await verifyRendered(page, fixture.entry, metrics)
    const refresh = (await context.cookies()).find(cookie => cookie.name === 'addp_refresh_token')
    check(refresh?.httpOnly && refresh.path === '/api/v1/system', 'missing HttpOnly refresh cookie')
    await verifyNoPersistedSecrets(page, [...secrets, refresh.value])

    stage = 'refresh preserves the tenant and AAL2'
    const [renewal, identity] = await Promise.all([
      responseFor(page, system('refresh'), 'POST'),
      responseFor(page, system('auth/context'), 'GET'),
      page.reload(),
    ])
    const renewed = await renewal.json()
    verifyIdentity(await identity.json(), fixture)
    await expect(page).toHaveURL(origin.origin + target)
    await verifyRendered(page, fixture.entry, metrics)
    await verifyNoPersistedSecrets(page, [...secrets, renewed.access_token])

    stage = 'other module reuses the authenticated session'
    const otherPage = await context.newPage()
    const otherTarget = opensWorkbench ? portal : workbench
    const [sharedIdentity] = await Promise.all([
      responseFor(otherPage, system('auth/context'), 'GET'),
      otherPage.goto(otherTarget),
    ])
    verifyIdentity(await sharedIdentity.json(), fixture)
    await expect(otherPage).toHaveURL(origin.origin + otherTarget)
    await verifyRendered(otherPage, opensWorkbench ? 'Portal' : 'Workbench', metrics)
    check(metrics.unexpected.length === 0, 'unexpected business API request')
    console.log(fixture.entry + ': real password, TOTP, tenant selection, return URL, refresh and cross-module session passed')
  } finally {
    await context.close()
  }
} catch (error) {
  // Do not print Playwright assertion details or network data with credentials.
  console.error('Browser login failed at ' + stage + (error instanceof CheckFailed ? ': ' + error.message : ''))
  process.exitCode = 1
} finally {
  await browser?.close()
}
