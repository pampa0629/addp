import { afterEach, describe, expect, it, vi } from 'vitest'
import { formatStandardDate, formatStandardDateTime } from '../src/utils/dateTime'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('standard date formatting', () => {
  it('中文界面使用 zh-CN 格式化日期时间', () => {
    const formatter = vi.spyOn(Date.prototype, 'toLocaleString').mockReturnValue('中文日期时间')
    const date = new Date('2026-08-12T08:00:00Z')

    expect(formatStandardDateTime(date, 'zh')).toBe('中文日期时间')
    expect(formatter).toHaveBeenCalledWith('zh-CN')
  })

  it('英文界面使用 en 格式化日期', () => {
    const formatter = vi.spyOn(Date.prototype, 'toLocaleDateString').mockReturnValue('English date')
    const date = new Date('2026-08-12T08:00:00Z')

    expect(formatStandardDate(date, 'en')).toBe('English date')
    expect(formatter).toHaveBeenCalledWith('en')
  })

  it.each([null, '', 'invalid-date'])('无效值 %s 显示占位符', value => {
    expect(formatStandardDateTime(value, 'zh')).toBe('-')
    expect(formatStandardDate(value, 'en')).toBe('-')
  })
})
