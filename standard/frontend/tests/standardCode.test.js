import { describe, expect, it, vi } from 'vitest'
import { buildStandardCodeRules, isValidStandardStableCode } from '../src/utils/standardCode'

describe('standard stable code', () => {
  it.each([
    ['outdoor_activity', true],
    ['  outdoor_activity  ', true],
    ['Outdoor_activity', false],
    ['outdoor-activity', false],
    ['1st_activity', false],
    ['', false]
  ])('validates %s as %s', (value, expected) => {
    expect(isValidStandardStableCode(value)).toBe(expected)
  })

  it('enforces the configured database column length', () => {
    expect(isValidStandardStableCode('a'.repeat(50), 50)).toBe(true)
    expect(isValidStandardStableCode('a'.repeat(51), 50)).toBe(false)
  })

  it('builds a form validator with the shared localized error', () => {
    const t = vi.fn(key => `translated:${key}`)
    const callback = vi.fn()
    const [, formatRule] = buildStandardCodeRules(t, 'standard.domain.codeRequired', 50)

    formatRule.validator({}, 'Invalid-Code', callback)

    expect(callback).toHaveBeenCalledOnce()
    expect(callback.mock.calls[0][0]).toEqual(new Error('translated:standard.common.codeFormatInvalid'))
  })
})
