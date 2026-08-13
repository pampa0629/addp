import { describe, expect, it, vi } from 'vitest'
import { getStandardErrorMessage, isCanceledInteraction, normalizeStandardBlobError } from '../src/utils/apiError'

describe('getStandardErrorMessage', () => {
  it('优先返回后端结构化错误消息', () => {
    const t = vi.fn()
    const error = { response: { data: { error: '业务域仍被引用，无法删除' } } }

    expect(getStandardErrorMessage(error, t)).toBe('业务域仍被引用，无法删除')
    expect(t).not.toHaveBeenCalled()
  })

  it('后端没有有效消息时返回指定的国际化兜底文案', () => {
    const t = vi.fn(key => `translated:${key}`)

    expect(getStandardErrorMessage(
      { response: { data: { error: '  ' } } },
      t,
      'standard.common.deleteFailed'
    )).toBe('translated:standard.common.deleteFailed')
  })
})

describe('isCanceledInteraction', () => {
  it.each(['cancel', 'close'])('将 %s 识别为用户取消操作', reason => {
    expect(isCanceledInteraction(reason)).toBe(true)
  })

  it('不会吞掉真实异常', () => {
    expect(isCanceledInteraction(new Error('request failed'))).toBe(false)
  })
})

describe('normalizeStandardBlobError', () => {
  it('将下载接口的 JSON Blob 错误还原为结构化响应', async () => {
    const error = {
      response: {
        data: new Blob([JSON.stringify({ error: '无权下载该文档', error_code: 'permission_denied' })]),
        headers: { 'content-type': 'application/json; charset=utf-8' }
      }
    }

    await normalizeStandardBlobError(error)

    expect(getStandardErrorMessage(error, vi.fn())).toBe('无权下载该文档')
    expect(error.response.data.error_code).toBe('permission_denied')
  })

  it('不改动非 JSON 或不可读取的响应', async () => {
    const data = { text: vi.fn() }
    const error = { response: { data, headers: { 'content-type': 'application/octet-stream' } } }

    await normalizeStandardBlobError(error)

    expect(error.response.data).toBe(data)
    expect(data.text).not.toHaveBeenCalled()
  })
})
