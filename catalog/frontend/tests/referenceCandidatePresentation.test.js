import { describe, expect, it } from 'vitest'
import { referenceCandidateLabel } from '../src/utils/referenceCandidatePresentation'

describe('reference candidate labels', () => {
  it('keeps full names and codes for search labels and hover details', () => {
    const option = { id: '30', name: '户外部', code: 'department_ef6e47ab0e3348fdb07ab5647b8e6241' }
    expect(referenceCandidateLabel(option, '名称不可用')).toBe(`${option.name} · ${option.code}`)
  })

  it('shortens only a long code while retaining the name and both code ends', () => {
    const option = Object.freeze({ id: '30', name: '户外部', code: 'department_ef6e47ab0e3348fdb07ab5647b8e6241' })
    expect(referenceCandidateLabel(option, '名称不可用', { compact: true })).toBe('户外部 · department_e…7b8e6241')
    expect(option.code).toBe('department_ef6e47ab0e3348fdb07ab5647b8e6241')
  })

  it('keeps short usernames and business codes intact', () => {
    for (const code of ['outdoor-develop', 'outdoor_person_id', '123456789012345678901234']) {
      expect(referenceCandidateLabel({ name: '业务名称', code }, '', { compact: true })).toBe(`业务名称 · ${code}`)
    }
  })

  it('does not abbreviate a long business name or substitute a database ID', () => {
    const name = '用于户外活动数据统计的业务责任部门完整名称'
    expect(referenceCandidateLabel({ id: '9007199254740993', name }, '', { compact: true })).toBe(name)
    expect(referenceCandidateLabel({ id: '9007199254740993' }, '名称不可用', { compact: true })).toBe('名称不可用')
  })

  it('does not split Unicode characters in a long code', () => {
    const code = '🌲'.repeat(25)
    expect(referenceCandidateLabel({ name: '户外', code }, '', { compact: true })).toBe(`户外 · ${'🌲'.repeat(12)}…${'🌲'.repeat(8)}`)
  })

  it('keeps an unavailable-name explanation when the candidate is absent', () => {
    expect(referenceCandidateLabel(undefined, '名称不可用', { compact: true })).toBe('名称不可用')
  })
})
