import { describe, expect, it } from 'vitest'
import { profileFailureMessage } from '../../src/utils/dataProfileExecution.js'
import zh from '../../src/i18n/zh-cn.json'
import en from '../../src/i18n/en.json'

describe('data profile execution feedback', () => {
  const translate = messages => key => key.split('.').reduce((value, part) => value[part], messages)

  it('explains protection changes in both supported languages and asks for explicit rerun', () => {
    const execution = { status: 'failed', error_code: 'protection_version_changed', error: 'unsafe stored details' }
    expect(profileFailureMessage(execution, translate(zh))).toBe('保护规则已变化，本次剖析结果未保存。请重新执行剖析。')
    expect(profileFailureMessage(execution, translate(en))).toBe('Protection rules changed and this profile was not saved. Please run profiling again.')
  })

  it('shows no failure for active or successful work and does not expose worker details', () => {
    for (const status of ['pending', 'running', 'success']) expect(profileFailureMessage({ status }, translate(zh))).toBe('')
    // A previous successful profile may still exist after a failed refresh.
    expect(profileFailureMessage({ status: 'failed', error: 'unsafe stored details' }, translate(zh))).toBe('最近一次剖析失败，请重新执行。')
    expect(profileFailureMessage({ status: 'timeout' }, translate(en))).toBe('The latest profile failed. Please run profiling again.')
  })

  it('explains missing execution source authorization without suggesting unsafe retries', () => {
    const execution = { status: 'failed', error_code: 'source_authorization_required', error: 'unsafe source details' }
    expect(profileFailureMessage(execution, translate(zh))).toBe('剖析执行的源数据授权尚未接通，本次未读取源数据。已有成功结果不受影响。')
    expect(profileFailureMessage(execution, translate(en))).toBe('Source authorization for profiling execution is not yet available. No source data was read; existing successful results are unchanged.')
  })
})
