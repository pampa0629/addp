import { describe, expect, it, vi } from 'vitest'
import { catalogStatusLabel } from '../src/utils/catalogStatusLabel'

describe('catalog status label', () => {
  it('does not construct an i18n key for an empty Element Plus table placeholder row', () => {
    const translate = vi.fn()

    expect(catalogStatusLabel(translate, 'catalog.status.visibility', undefined)).toBe('-')
    expect(translate).not.toHaveBeenCalled()
  })

  it('translates a present canonical status value', () => {
    const translate = vi.fn(key => `translated:${key}`)

    expect(catalogStatusLabel(translate, 'catalog.status.visibility', 'inventory')).toBe(
      'translated:catalog.status.visibility.inventory'
    )
    expect(translate).toHaveBeenCalledWith('catalog.status.visibility.inventory')
  })
})
