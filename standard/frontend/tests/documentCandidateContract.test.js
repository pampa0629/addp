import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

import en from '../src/i18n/en.json'
import zhCn from '../src/i18n/zh-cn.json'

const documentDetailSource = readFileSync(new URL('../src/views/DocumentDetail.vue', import.meta.url), 'utf8')

describe('document candidate contract', () => {
  it('shows an enumeration candidate code set reference with bilingual labels', () => {
    expect(documentDetailSource).toContain('candidate.payload?.code_set_code')
    expect(documentDetailSource).toContain('candidate.payload.code_set_code')
    expect(zhCn.standard.document.codeSetReference).toBe('引用码值集候选')
    expect(en.standard.document.codeSetReference).toBe('Referenced Code Set Candidate')
    expect(zhCn.standard.document.comparisonField.code_set_code).toBe('码值集编码')
    expect(en.standard.document.comparisonField.code_set_code).toBe('Code Set Code')
  })
})
