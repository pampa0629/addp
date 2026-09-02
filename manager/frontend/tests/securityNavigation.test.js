import { describe, expect, it } from 'vitest'

import { buildSecurityProtectionRoute } from '@/utils/securityNavigation'

describe('Security protection navigation', () => {
  it('passes only the selected ResourceLocator to the canonical Security route', () => {
    const locator = 'addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657'
    expect(buildSecurityProtectionRoute(locator)).toBe(
      '/security/protection-enrollments?action=enroll&locator=addp%3A%2F%2Fengine%2F11%2Fpath%2FOutdoor%2FPersons%3Ftype%3Dcollection%26item_id%3D51657'
    )
  })

  it('does not create a route without a selected resource', () => {
    expect(buildSecurityProtectionRoute('  ')).toBe('')
  })
})
