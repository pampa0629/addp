import { describe, expect, it, vi } from 'vitest'
import { coverageDimensionLabel } from '../src/utils/governanceCoverageView'

describe('catalog governance coverage view', () => {
  it('does not construct an i18n key for an empty Element Plus table placeholder row', () => {
    const translate = vi.fn()

    expect(coverageDimensionLabel(translate, undefined, 'name')).toBe('-')
    expect(coverageDimensionLabel(translate, undefined, 'description')).toBe('-')
    expect(translate).not.toHaveBeenCalled()
  })

  it('translates a canonical coverage dimension field', () => {
    const translate = vi.fn(key => `translated:${key}`)

    expect(coverageDimensionLabel(translate, 'primary_domain', 'name')).toBe(
      'translated:catalog.coverage.dimensions.primary_domain.name'
    )
  })
})
