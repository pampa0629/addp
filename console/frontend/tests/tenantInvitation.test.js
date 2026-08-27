import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import {
  tenantInvitationRegistrationRequest,
  tenantInvitationSecret,
  tenantInvitationSession
} from '../src/invitations/tenantInvitation'

const readSource = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8')

describe('tenant invitation acceptance', () => {
  it('keeps the System invitation URL on a public Console route before the authenticated catch-all', () => {
    const router = readSource('router/index.js')
    const invitationRoute = router.indexOf("path: '/invitations/accept'")
    const catchAllRoute = router.indexOf("path: '/:pathMatch(.*)*'")

    expect(invitationRoute).toBeGreaterThan(-1)
    expect(catchAllRoute).toBeGreaterThan(invitationRoute)
    expect(router.slice(invitationRoute, catchAllRoute)).toContain('requiresAuth: false')

    const api = readSource('api/auth.js')
    expect(api).toContain("systemClient.post('/tenant/invitations/registrations'")
    expect(api).toContain("systemClient.post('/tenant/invitations/acceptances'")
  })

  it('accepts only the canonical opaque invitation secret', () => {
    expect(tenantInvitationSecret('addp_ti_example')).toBe('addp_ti_example')
    expect(() => tenantInvitationSecret(undefined)).toThrow('tenant_invitation_missing')
    expect(() => tenantInvitationSecret('invalid')).toThrow('tenant_invitation_invalid')
    expect(() => tenantInvitationSecret('addp_ti_')).toThrow('tenant_invitation_invalid')
  })

  it('builds the single public registration request without confirmation fields', () => {
    expect(tenantInvitationRegistrationRequest({
      invitationSecret: 'addp_ti_example',
      username: ' invited-user ',
      password: 'secret',
      displayName: ' Invited User ',
      locale: 'zh-cn'
    })).toEqual({
      invitation_secret: 'addp_ti_example',
      username: 'invited-user',
      password: 'secret',
      display_name: 'Invited User',
      locale: 'zh-cn'
    })
  })

  it('rejects incomplete registration and invalid session responses', () => {
    expect(() => tenantInvitationRegistrationRequest({ invitationSecret: 'addp_ti_example' }))
      .toThrow('tenant_invitation_registration_incomplete')
    expect(() => tenantInvitationSession({ session: {} })).toThrow('tenant_invitation_session_invalid')
    expect(tenantInvitationSession({ data: { session: { access_token: 'addp_at_example', expires_in: 900 } } }))
      .toEqual({ access_token: 'addp_at_example', expires_in: 900 })
  })
})
