import { describe, expect, it } from 'vitest'
import { standardMappingElementLabel, standardMappingRevisionLabel } from '../src/utils/standardMappingPresentation'
import zhCn from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

function translate(messages) {
  return key => key.split('.').reduce((value, part) => value?.[part], messages) || key
}

describe('StandardMapping human-readable presentation', () => {
  it('uses the exact resolved Standard revision number, never its database ID', () => {
    const row = {
      element_id: '50', element_revision_id: '501',
      element_reference: { status: 'resolved', name: '人员标识', code: 'outdoor_person_id', revision_no: 3 }
    }
    expect(standardMappingElementLabel(row, translate(zhCn))).toBe('人员标识 · outdoor_person_id')
    expect(standardMappingRevisionLabel(row, translate(zhCn))).toBe('R3')
  })

  it('distinguishes an unfixed legacy candidate from an unavailable exact revision', () => {
    const t = translate(zhCn)
    expect(standardMappingElementLabel({
      element_id: '50', evidence: { legacy_observed_snapshot: { name: '历史人员标识' } },
      element_reference: { status: 'unfixed' }
    }, t)).toBe('历史人员标识')
    expect(standardMappingRevisionLabel({ element_revision_id: null }, t)).toBe('待补选修订')
    expect(standardMappingElementLabel({ element_id: '50', element_reference: { status: 'unavailable' } }, t)).toBe('数据元信息不可用')
    expect(standardMappingRevisionLabel({ element_revision_id: '501', element_reference: { status: 'missing' } }, t)).toBe('修订信息不可用')
    expect(standardMappingRevisionLabel({ element_revision_id: '501', element_reference: { status: 'unavailable' } }, translate(en))).toBe('Revision information unavailable')
  })
})
