import { describe, expect, it } from 'vitest'
import { extractRtfText } from '../../../../common-frontend/basic/src/lib/office/rtf.ts'

function rtfBuffer(value) {
  return new TextEncoder().encode(value).buffer
}

describe('ADDP RTF parser', () => {
  it('decodes consecutive CP936 hex escapes as multibyte text', () => {
    const text = extractRtfText(rtfBuffer(
      String.raw`{\rtf1\ansi\ansicpg936 \'d6\'d0\'d0\'c5\'b3\'f6\'b0\'e6\'c9\'e7\'ba\'cf\'bc\'af.txt}`
    ))

    expect(text).toBe('中信出版社合集.txt')
  })

  it('keeps Unicode controls and skips their ANSI fallback and metadata destinations', () => {
    const text = extractRtfText(rtfBuffer(
      String.raw`{\rtf1\ansi\uc1{\fonttbl\f0 Arial;}{\info{\title Hidden}}\u20013?\u20449?\par visible}`
    ))

    expect(text).toBe('中信\nvisible')
  })

  it('preserves escaped line breaks and Windows punctuation used by Cocoa RTF', () => {
    const text = extractRtfText(rtfBuffer(
      "{\\rtf1\\ansi\\ansicpg936{\\fonttbl\\f0\\fnil\\fcharset134 PingFangSC-Regular;\\f1\\fswiss\\fcharset0 Helvetica;}\\f1 \\'93\\f0 \\'b4\\'b4\\'d2\\'b5\\f1 \\'94\\\nnext}"
    ))

    expect(text).toBe('“创业”\nnext')
  })
})
