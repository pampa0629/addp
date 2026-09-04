import { describe, expect, it } from 'vitest'

import en from '../src/i18n/en.json'
import zhCn from '../src/i18n/zh-cn.json'

describe('Metric translations', () => {
  it('defines the unit label used by metric forms in every supported locale', () => {
    expect(zhCn.standard.metric.unitLabel).toBe('计量单位')
    expect(en.standard.metric.unitLabel).toBe('Unit')
  })
})
