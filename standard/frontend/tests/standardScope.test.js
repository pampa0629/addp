import { describe, expect, it } from 'vitest'
import {
  EDITABLE_STANDARD_SCOPES,
  buildStandardOwnership,
  requiresOwnerDomain,
  standardScopeLabelKey
} from '../src/utils/standardScope'

describe('standard scope', () => {
  it('only exposes tenant-owned scopes for editing', () => {
    expect(EDITABLE_STANDARD_SCOPES).toEqual(['tenant_common', 'domain'])
  })

  it('keeps an owner only for domain scope', () => {
    expect(buildStandardOwnership('domain', 12)).toEqual({ scope_type: 'domain', owner_domain_id: 12 })
    expect(buildStandardOwnership('tenant_common', 12)).toEqual({ scope_type: 'tenant_common', owner_domain_id: null })
  })

  it('provides scope behavior and label keys', () => {
    expect(requiresOwnerDomain('domain')).toBe(true)
    expect(requiresOwnerDomain('platform')).toBe(false)
    expect(standardScopeLabelKey('tenant_common')).toBe('standard.common.scopeValue.tenant_common')
  })
})
