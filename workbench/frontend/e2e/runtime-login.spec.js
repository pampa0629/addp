import { test, expect } from '@playwright/test'
import { applicationID, runtimePath, installMetricApplicationBackend } from './fixtures/metricApplication.js'

async function installLoginBackend(context, { locale = 'zh-cn', mfa = false, direct = false } = {}) {
  const backend = await installMetricApplicationBackend(context, {
    locale,
    permissions: ['workbench.data_application.execute'],
    configure(draft) {
      draft.snapshot.parameter_presets = [{ key: 'monthly', name: 'Monthly', parameter_values: { shared_grain: 'month', direction: 'both' } }]
    },
  })
  let issued = false
  const calls = []
  const tenant = { type: 'tenant', tenant_id: '1', tenant_membership_id: 'membership-1', tenant_name: 'Fixture Tenant' }
  const session = { access_token: 'isolated-login-token', expires_in: 3600 }
  const selection = { next_action: 'select_context', selection: { selection_ticket: 'selection-ticket', expires_at: '2099-01-01T00:00:00Z', contexts: [
    tenant,
    { type: 'tenant', tenant_id: '2', tenant_membership_id: 'membership-2', tenant_name: 'Another Tenant' },
  ] } }
  await context.route(url => url.pathname.startsWith('/api/v1/system/'), async route => {
    const path = new URL(route.request().url()).pathname
    const send = (data, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(data) })
    if (path.endsWith('/refresh')) return send(issued ? session : { error: 'Unauthorized' }, issued ? 200 : 401)
    if (path.endsWith('/login')) {
      calls.push({ action: 'login', body: route.request().postDataJSON() })
      if (direct) { issued = true; return send({ next_action: 'session_issued', session }) }
      return send(mfa ? { next_action: 'verify_mfa', mfa: { challenge_token: 'mfa-challenge', method: 'totp', expires_at: '2099-01-01T00:00:00Z' } } : selection)
    }
    if (path.endsWith('/auth/mfa-verifications')) {
      calls.push({ action: 'mfa', body: route.request().postDataJSON() })
      return send(selection)
    }
    if (path.endsWith('/auth/context-selections')) {
      calls.push({ action: 'context', body: route.request().postDataJSON() })
      issued = true
      return send(session)
    }
    return route.fallback()
  })
  return { backend, calls }
}

for (const locale of ['zh-cn', 'en']) {
  test(`fresh browser completes context selection and returns to the original runtime (${locale})`, async ({ page, context }) => {
    const { backend, calls } = await installLoginBackend(context, { locale })
    const target = `${runtimePath}?preset=monthly#summary`
    await page.goto(target)
    await page.locator('input').first().fill('fixture-user')
    await page.locator('input[type=password]').fill('fixture-password')
    await page.locator('button[type=submit]').click()
    await expect(page.getByText('Fixture Tenant', { exact: true })).toBeVisible()
    expect(calls.map(call => call.action)).toEqual(['login'])
    expect(backend.requests).toEqual([])
    const continueButton = page.getByRole('button', { name: locale === 'en' ? 'Continue' : '继续', exact: true })
    await expect(continueButton).toBeDisabled()
    await page.getByText('Fixture Tenant', { exact: true }).click()
    await expect(page.getByRole('radio', { name: 'Fixture Tenant', exact: true })).toBeChecked()
    await continueButton.click()
    await expect(page).toHaveURL(target)
    await expect(page.getByTestId('runtime-component')).toHaveCount(2)
    await expect.poll(() => backend.requests.length).toBeGreaterThan(0)
    expect(backend.requests.every(request => request.body.parameters.grain === 'month')).toBe(true)
    expect(calls.map(call => call.action)).toEqual(['login', 'context'])
    expect(backend.unexpected).toEqual([])
  })
}

test('runtime login has a reloadable Workbench URL and completes MFA before issuing a session', async ({ page, context }) => {
  const { backend, calls } = await installLoginBackend(context, { mfa: true })
  const target = `${runtimePath}?preset=monthly#summary`
  await page.goto(target)
  await expect(page).toHaveURL(url => url.pathname === '/module-ui/workbench/login' && url.searchParams.get('redirect') === `/data-apps/${applicationID}?preset=monthly#summary`)
  await page.reload()
  await expect(page.getByRole('heading', { name: '登录工作台' })).toBeVisible()
  await page.locator('input').first().fill('fixture-user')
  await page.locator('input[type=password]').fill('fixture-password')
  await page.locator('button[type=submit]').click()
  const code = page.locator('input[autocomplete=one-time-code]')
  await expect(code).toBeVisible()
  expect(backend.requests).toEqual([])
  await code.fill('123456')
  await page.locator('button[type=submit]').click()
  await expect(page.getByText('Fixture Tenant', { exact: true })).toBeVisible()
  await page.getByText('Fixture Tenant', { exact: true }).click()
  await expect(page.getByRole('radio', { name: 'Fixture Tenant', exact: true })).toBeChecked()
  await page.getByRole('button', { name: '继续', exact: true }).click()
  await expect(page).toHaveURL(target)
  await expect(page.getByTestId('runtime-component')).toHaveCount(2)
  expect(calls).toEqual([
    { action: 'login', body: { username: 'fixture-user', password: 'fixture-password' } },
    { action: 'mfa', body: { challenge_token: 'mfa-challenge', code: '123456' } },
    { action: 'context', body: { selection_ticket: 'selection-ticket', context_type: 'tenant', tenant_membership_id: 'membership-1' } },
  ])
  expect(backend.unexpected).toEqual([])
})

test('direct session login returns to the runtime and survives a reload', async ({ page, context }) => {
  const { backend } = await installLoginBackend(context, { direct: true })
  await page.goto(runtimePath)
  await page.locator('input').first().fill('fixture-user')
  await page.locator('input[type=password]').fill('fixture-password')
  await page.locator('button[type=submit]').click()
  await expect(page).toHaveURL(runtimePath)
  await expect(page.getByTestId('runtime-component')).toHaveCount(2)
  await page.reload()
  await expect(page).toHaveURL(runtimePath)
  await expect(page.getByTestId('runtime-component')).toHaveCount(2)
  expect(backend.unexpected).toEqual([])
})
