import { describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { catalogStatusLabel } from '../src/utils/catalogStatusLabel'
import zhCn from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

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

  it.each([
    ['zh-cn', ['业务域', '业务术语', '主业务域', '辅助业务域', '适用术语', '有效', '待移交']],
    ['en', ['Business domain', 'Business term', 'Primary domain', 'Secondary domain', 'Applicable term', 'Active', 'Transfer required']]
  ])('shows readable curation relations in %s without missing translations', (locale, expected) => {
    const missing = vi.fn()
    const { global: { t } } = createI18n({ legacy: false, locale, fallbackLocale: false, messages: { 'zh-cn': zhCn, en }, missing })
    const values = [
      ['catalog.edit.semanticTypeLabel', 'domain'], ['catalog.edit.semanticTypeLabel', 'glossary'],
      ['catalog.edit', 'primary'], ['catalog.edit', 'secondary'], ['catalog.edit', 'applies'],
      ['catalog.status.responsibility', 'active'], ['catalog.status.responsibility', 'needs_transfer']
    ]
    expect(values.map(([prefix, value]) => catalogStatusLabel(t, prefix, value))).toEqual(expected)
    expect(missing).not.toHaveBeenCalled()
  })
})
