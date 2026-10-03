import { afterEach, describe, expect, it, vi } from 'vitest'
import { createAddpI18n } from '../../../common-frontend/basic/src/composables/useAddpI18n.js'
import zhCnMessages from '../src/i18n/zh-cn.json'
import enMessages from '../src/i18n/en.json'

afterEach(() => vi.unstubAllGlobals())

describe('Manager shared translations', () => {
  it('keeps the shared index term alongside the module engine terms in both languages', () => {
    vi.stubGlobal('localStorage', { getItem: () => 'zh-cn' })
    const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zhCnMessages, en: enMessages } })
    expect(i18n.global.t('engine.term.index')).toBe('索引')
    expect(i18n.global.t('engine.term.database')).toBe(zhCnMessages.engine.term.database)
    i18n.global.locale.value = 'en'
    expect(i18n.global.t('engine.term.index')).toBe('Index')
    expect(i18n.global.t('engine.term.database')).toBe(enMessages.engine.term.database)
  })

  it('does not leak module overrides into another i18n instance', () => {
    vi.stubGlobal('localStorage', { getItem: () => 'zh-cn' })
    const moduleInstance = createAddpI18n({ moduleMessages: { 'zh-cn': { engine: { term: { index: '模块索引' } } } } })
    const sharedInstance = createAddpI18n()
    expect(moduleInstance.i18n.global.t('engine.term.index')).toBe('模块索引')
    expect(sharedInstance.i18n.global.t('engine.term.index')).toBe('索引')
  })
})
