import { describe, expect, it } from 'vitest'
import { presentUnitEntry, copyUnitTranslations, validUnitTranslations } from '../src/utils/unitPresentation'

describe('runtime measurement directory translations', () => {
  it('changes language without changing resource identity or writing translation data', () => {
    const unit = { id: 8, scope_type: 'platform', translations: {
      'zh-cn': { name: '千克', symbol: 'kg' }, en: { name: 'Kilogram', symbol: 'kg' }
    }, category: { id: 3, scope_type: 'platform', translations: { 'zh-cn': { name: '质量' }, en: { name: 'Mass' } } } }
    expect(presentUnitEntry(unit, 'en')).toMatchObject({ id: 8, name: 'Kilogram', category: { id: 3, name: 'Mass' } })
    expect(presentUnitEntry(unit, 'zh-cn')).toMatchObject({ id: 8, name: '千克' })
    expect(presentUnitEntry(unit, 'fr')).toMatchObject({ id: 8, name: '千克' })
    expect(unit).not.toHaveProperty('name')
    expect(presentUnitEntry({ id: 9, scope_type: 'tenant_common', name: '我的箱' }, 'en').name).toBe('我的箱')
  })
  it('edits independent dictionaries and requires every declared translation', () => {
    const original = { 'zh-cn': { name: '米' }, en: { name: 'Metre' } }
    const copy = copyUnitTranslations(original)
    copy.en.name = 'Meter'
    expect(original.en.name).toBe('Metre')
    expect(validUnitTranslations(copy)).toBe(true)
    expect(validUnitTranslations({ ...copy, fr: { name: '' } })).toBe(false)
    expect(validUnitTranslations({ ...copy, fr: { name: 'Mètre' } })).toBe(true)
  })
})
