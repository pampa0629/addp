import { expect, test } from '@playwright/test'
test('anonymous administrator recovery opens the actual System page through Console', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('addp-lang', 'zh-cn'))
  await page.route('**/api/v1/system/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path.endsWith('/refresh')) return route.fulfill({ status: 401, json: { error: 'No session' } })
    if (path.endsWith('/administrator-recovery/enrollments')) {
      expect(route.request().headers().authorization).toBeUndefined()
      expect(route.request().postDataJSON()).toEqual({ recovery_token: 'addp_pcr_console_fixture' })
      return route.fulfill({ status: 201, json: { enrollment_token: 'addp_pce_console_fixture', secret: 'JBSWY3DPEHPK3PXP', otpauth_uri: 'otpauth://totp/ADDP:fixture?secret=JBSWY3DPEHPK3PXP&issuer=ADDP', expires_at: new Date(Date.now() + 900000).toISOString() } })
    }
    throw new Error(`Unexpected public Console recovery request ${path}`)
  })
  await page.goto('/administrator-recovery')
  const frame = page.frameLocator('iframe')
  await expect(frame.getByRole('heading', { name: '管理员凭据恢复' })).toBeVisible()
  await frame.locator('textarea').fill('addp_pcr_console_fixture')
  await frame.getByRole('button', { name: '继续', exact: true }).click()
  await expect(frame.getByText('JBSWY3DPEHPK3PXP', { exact: true })).toBeVisible()
  expect(page.url()).not.toContain('addp_pcr_')
  await expect(page.locator('iframe')).toHaveAttribute('src', '/module-ui/system/administrator-recovery')
})
