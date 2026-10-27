import { createHmac } from 'node:crypto'

// RFC 6238, SHA-1/six digits, matching System's configured authenticator. The
// caller waits for a fresh counter so an earlier API login cannot replay a code.
export function localIdentityTOTP(secret, now = Date.now()) {
  if (typeof secret !== 'string' || !/^[A-Z2-7]{16,128}$/.test(secret) || !Number.isFinite(now) || now < 0) throw new Error('execution_trace_local_browser_mfa_invalid')
  let bits = 0, count = 0
  const bytes = []
  for (const letter of secret) {
    bits = (bits << 5) | 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'.indexOf(letter); count += 5
    if (count >= 8) { count -= 8; bytes.push((bits >>> count) & 255) }
  }
  const counter = Buffer.alloc(8); counter.writeBigUInt64BE(BigInt(Math.floor(now / 30000)))
  const digest = createHmac('sha1', Buffer.from(bytes)).update(counter).digest(), offset = digest[19] & 15
  return String((digest.readUInt32BE(offset) & 0x7fffffff) % 1000000).padStart(6, '0')
}

// One real browser password/MFA protocol for the local product acceptance profiles.
export async function localConsoleLogin(page, expect, { prefix, path, beforeMFA = () => {} }) {
  const identityResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/auth/context' && response.ok())
  identityResponse.catch(() => {})
  await page.goto('/login?redirect=' + encodeURIComponent(path))
  await page.locator('input[autocomplete="username"]').fill(process.env[prefix + 'ADMIN_USERNAME'])
  await page.locator('input[autocomplete="current-password"]').fill(process.env[prefix + 'ADMIN_PASSWORD'])
  const passwordResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/login')
  await page.locator('.auth-login-primary').click()
  expect((await (await passwordResponse).json()).next_action).toBe('verify_mfa')
  await expect(page.locator('input[autocomplete="one-time-code"]')).toBeVisible()
  await new Promise(done => setTimeout(done, 30000 - Date.now() % 30000 + 150))
  const code = localIdentityTOTP(process.env[prefix + 'ADMIN_TOTP'])
  const mfaResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1/system/auth/mfa-verifications')
  beforeMFA()
  await page.locator('input[autocomplete="one-time-code"]').fill(code)
  await page.locator('.auth-login-primary').click()
  expect((await (await mfaResponse).json()).next_action).toBe('session_issued')
  const identity = await (await identityResponse).json()
  expect(identity.principal.type).toBe('user')
  expect(identity.context.type).toBe('platform')
  expect(identity.delegation ?? null).toBeNull()
  expect(identity.authentication.assurance_level).toMatch(/^aal[23]$/)
  expect(identity.authorization.role_assignments.map(role => role.role_key)).toEqual(['platform.system_administrator'])
  return identity
}
