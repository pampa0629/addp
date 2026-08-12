import { describe, expect, it, vi } from 'vitest'
import { getStandardErrorMessage, isCanceledInteraction } from '../src/utils/apiError'

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
