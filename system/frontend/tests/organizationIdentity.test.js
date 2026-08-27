import { describe, expect, it } from 'vitest'
import { isValidOrganizationCode } from '../src/utils/organizationIdentity'

describe('organization identity validation', () => {
  it.each([
    'a',
    'outdoor_data_governance',
    'department2'
  ])('accepts canonical organization code %s', (code) => {
    expect(isValidOrganizationCode(code)).toBe(true)
  })

  it.each([
    '',
    'outdoor-data-governance',
    '_department',
    'department_',
    '2department',
    'Department'
  ])('rejects non-canonical organization code %s', (code) => {
    expect(isValidOrganizationCode(code)).toBe(false)
  })
})
